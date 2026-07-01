// Package storage persists change events to ClickHouse.
package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"

	"github.com/gauravrautela/kubewatch/internal/event"
)

// Schema is the DDL for the change_events table. Append-only MergeTree,
// partitioned by month, ordered for fast per-resource and per-cluster reads,
// with a 90-day TTL and a bloom-filter index for user-based search.
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
    INDEX idx_user user_name TYPE bloom_filter GRANULARITY 4
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(event_time)
ORDER BY (cluster, namespace, kind, name, event_time)
TTL toDateTime(event_time) + INTERVAL 90 DAY
`

const insertStmt = `INSERT INTO change_events (
    event_id, event_time, ingested_at, cluster, source, operation,
    api_group, api_version, kind, namespace, name, resource_uid, sub_resource,
    user_name, user_groups, user_uid, user_agent, dry_run,
    old_object, new_object, diff
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

// Migrate creates the change_events table if it does not exist.
func (s *Store) Migrate(ctx context.Context) error { return s.conn.Exec(ctx, Schema) }

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
		id, err := uuid.Parse(e.EventID)
		if err != nil {
			return fmt.Errorf("event %q: bad uuid: %w", e.EventID, err)
		}
		groups := e.UserGroups
		if groups == nil {
			groups = []string{}
		}
		if err := batch.Append(
			id, e.EventTime, now, e.Cluster, e.Source, string(e.Operation),
			e.APIGroup, e.APIVersion, e.Kind, e.Namespace, e.Name, e.ResourceUID, e.SubResource,
			e.UserName, groups, e.UserUID, e.UserAgent, e.DryRun,
			e.OldObject, e.NewObject, e.Diff,
		); err != nil {
			return fmt.Errorf("append: %w", err)
		}
	}
	return batch.Send()
}
