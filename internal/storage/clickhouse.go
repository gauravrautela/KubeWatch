// Package storage persists change events to ClickHouse.
package storage

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"

	"github.com/gauravrautela/kubewatch/internal/event"
)

// Schema is the DDL for the change_events table. Append-only MergeTree,
// partitioned by month, ordered for fast per-resource and per-cluster reads,
// with a 7-day TTL and a bloom-filter index for user-based search.
const Schema = `
CREATE TABLE IF NOT EXISTS change_events (
    event_id      UUID,
    event_time    DateTime64(3),
    ingested_at   DateTime64(3),
    cluster       LowCardinality(String),
    source        Enum8('webhook' = 1, 'reconcile' = 2),
    operation     Enum8('CREATE' = 1, 'UPDATE' = 2, 'DELETE' = 3),
    api_group     LowCardinality(String),
    api_version   LowCardinality(String),
    kind          LowCardinality(String),
    namespace     String,
    name          String,
    resource_uid  String,
    sub_resource  LowCardinality(String),
    user_name     String,
    user_groups   Array(String),
    user_uid      String,
    user_agent    String,
    dry_run       UInt8,
    old_object    String,
    new_object    String,
    diff          String,
    change_class  Array(LowCardinality(String)) DEFAULT [],
    actor_type    Enum8('unknown' = 0, 'human' = 1, 'serviceaccount' = 2, 'system' = 3) DEFAULT 'unknown',
    INDEX idx_user user_name TYPE bloom_filter GRANULARITY 4
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(event_time)
ORDER BY (cluster, namespace, kind, name, event_time)
TTL toDateTime(event_time) + INTERVAL 7 DAY
`

// statsSchema keeps per-resource daily change counts alive for 30 days so
// rarity/churn scoring can look past the raw event TTL. Rows are tiny (no
// object bodies).
const statsSchema = `
CREATE TABLE IF NOT EXISTS resource_change_stats (
    cluster      LowCardinality(String),
    namespace    String,
    kind         LowCardinality(String),
    name         String,
    day          Date,
    change_count UInt64
)
ENGINE = SummingMergeTree
ORDER BY (cluster, namespace, kind, name, day)
TTL day + INTERVAL 30 DAY
`

const statsMV = `
CREATE MATERIALIZED VIEW IF NOT EXISTS resource_change_stats_mv TO resource_change_stats AS
SELECT cluster, namespace, kind, name, toDate(event_time) AS day, count() AS change_count
FROM change_events
WHERE dry_run = 0
GROUP BY cluster, namespace, kind, name, day
`

// migrationStatements is every DDL statement Migrate runs, in order. The
// ALTERs bring pre-existing installs up to the current Schema; each is a
// no-op once applied.
func migrationStatements() []string {
	return []string{
		Schema,
		`ALTER TABLE change_events ADD COLUMN IF NOT EXISTS change_class Array(LowCardinality(String)) DEFAULT []`,
		`ALTER TABLE change_events ADD COLUMN IF NOT EXISTS actor_type Enum8('unknown' = 0, 'human' = 1, 'serviceaccount' = 2, 'system' = 3) DEFAULT 'unknown'`,
		`ALTER TABLE change_events MODIFY TTL toDateTime(event_time) + INTERVAL 7 DAY`,
		statsSchema,
		statsMV,
	}
}

const insertStmt = `INSERT INTO change_events (
    event_id, event_time, ingested_at, cluster, source, operation,
    api_group, api_version, kind, namespace, name, resource_uid, sub_resource,
    user_name, user_groups, user_uid, user_agent, dry_run,
    old_object, new_object, diff, change_class, actor_type
)`

// Store is a ClickHouse-backed change-event store.
type Store struct {
	conn driver.Conn
}

// New opens a ClickHouse connection from a DSN like
// clickhouse://user:pass@host:9000/db. It does not eagerly connect; call Ping.
func New(dsn string) (*Store, error) {
	opts, err := clickhouse.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	return &Store{conn: conn}, nil
}

// Ping forces a connection and surfaces config errors.
func (s *Store) Ping(ctx context.Context) error { return s.conn.Ping(ctx) }

// Migrate creates and upgrades the KubeWatch tables.
func (s *Store) Migrate(ctx context.Context) error {
	for _, q := range migrationStatements() {
		if err := s.conn.Exec(ctx, q); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

// Close closes the underlying connection.
func (s *Store) Close() error { return s.conn.Close() }

// InsertBatch writes events in a single ClickHouse batch, stamping ingested_at
// as now for every row.
func (s *Store) InsertBatch(ctx context.Context, events []event.ChangeEvent) error {
	if len(events) == 0 {
		return nil
	}
	batch, err := s.conn.PrepareBatch(ctx, insertStmt)
	if err != nil {
		return fmt.Errorf("prepare batch: %w", err)
	}
	now := time.Now().UTC()
	for _, e := range events {
		id, ok := validForInsert(e)
		if !ok {
			log.Printf("storage: skipping invalid event id=%q source=%q op=%q", e.EventID, e.Source, e.Operation)
			continue
		}
		groups := e.UserGroups
		if groups == nil {
			groups = []string{}
		}
		classes := e.ChangeClass
		if classes == nil {
			classes = []string{}
		}
		actor := e.ActorType
		if actor == "" {
			actor = "unknown"
		}
		if err := batch.Append(
			id, e.EventTime, now, e.Cluster, e.Source, string(e.Operation),
			e.APIGroup, e.APIVersion, e.Kind, e.Namespace, e.Name, e.ResourceUID, e.SubResource,
			e.UserName, groups, e.UserUID, e.UserAgent, e.DryRun,
			e.OldObject, e.NewObject, e.Diff, classes, actor,
		); err != nil {
			return fmt.Errorf("append: %w", err)
		}
	}
	return batch.Send()
}

// validForInsert reports whether e can be inserted and returns its parsed UUID.
// Rows failing this are skipped (logged) rather than failing the whole batch.
func validForInsert(e event.ChangeEvent) (uuid.UUID, bool) {
	id, err := uuid.Parse(e.EventID)
	if err != nil {
		return uuid.UUID{}, false
	}
	switch e.Source {
	case "webhook", "reconcile":
	default:
		return uuid.UUID{}, false
	}
	switch e.Operation {
	case event.OpCreate, event.OpUpdate, event.OpDelete:
	default:
		return uuid.UUID{}, false
	}
	return id, true
}
