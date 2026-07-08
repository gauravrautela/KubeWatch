package storage

import (
	"strings"
	"testing"
)

func TestSchemaHasIncidentColumnsAndTTL(t *testing.T) {
	for _, want := range []string{
		"change_class  Array(LowCardinality(String)) DEFAULT []",
		"actor_type    Enum8('unknown' = 0, 'human' = 1, 'serviceaccount' = 2, 'system' = 3) DEFAULT 'unknown'",
		"TTL toDateTime(event_time) + INTERVAL 7 DAY",
	} {
		if !strings.Contains(Schema, want) {
			t.Errorf("Schema missing %q", want)
		}
	}
}

func TestMigrationStatements(t *testing.T) {
	joined := strings.Join(migrationStatements(), "\n---\n")
	for _, want := range []string{
		"ADD COLUMN IF NOT EXISTS change_class",
		"ADD COLUMN IF NOT EXISTS actor_type",
		"MODIFY TTL toDateTime(event_time) + INTERVAL 7 DAY",
		"CREATE TABLE IF NOT EXISTS resource_change_stats",
		"SummingMergeTree",
		"TTL day + INTERVAL 30 DAY",
		"CREATE MATERIALIZED VIEW IF NOT EXISTS resource_change_stats_mv TO resource_change_stats",
		"WHERE dry_run = 0",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("migration statements missing %q", want)
		}
	}
}

func TestInsertStatementIncludesIncidentColumns(t *testing.T) {
	for _, want := range []string{"change_class", "actor_type"} {
		if !strings.Contains(insertStmt, want) {
			t.Errorf("insertStmt missing %q", want)
		}
	}
}
