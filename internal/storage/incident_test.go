package storage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gauravrautela/kubewatch/internal/event"
)

func TestStatsWindow(t *testing.T) {
	start := time.Date(2026, 7, 8, 13, 45, 0, 0, time.UTC)
	windowDay, floor := statsWindow(start)
	if want := time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC); !windowDay.Equal(want) {
		t.Fatalf("windowDay = %v, want %v", windowDay, want)
	}
	if want := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC); !floor.Equal(want) {
		t.Fatalf("floor = %v, want %v", floor, want)
	}
}

func TestIncidentQueriesShape(t *testing.T) {
	for _, want := range []string{
		"FROM change_events",
		"cluster = ?",
		"event_time >= ?",
		"event_time <= ?",
		"dry_run = 0",
		"ORDER BY event_time DESC",
	} {
		if !strings.Contains(incidentEventsQuery, want) {
			t.Errorf("incidentEventsQuery missing %q", want)
		}
	}
	for _, want := range []string{
		"FROM resource_change_stats",
		"GROUP BY namespace, kind, name",
		"sum(if(day < ?, change_count, 0))",
		"uniqExactIf(day, day < ?)",
		"max(if(day < ?",
	} {
		if !strings.Contains(resourceStatsQuery, want) {
			t.Errorf("resourceStatsQuery missing %q", want)
		}
	}
}

// Integration coverage; skipped without CLICKHOUSE_DSN (see testStore).
func TestIncidentEventsAndStatsIntegration(t *testing.T) {
	s := testStore(t)
	defer s.Close()
	ctx := context.Background()

	cluster := "incident-test-" + uuid.NewString()[:8]
	now := time.Now().UTC().Truncate(time.Millisecond)
	ev := event.ChangeEvent{
		EventID: uuid.NewString(), EventTime: now, Cluster: cluster,
		Source: "webhook", Operation: event.OpUpdate,
		Kind: "Deployment", Namespace: "payments", Name: "checkout",
		UserName: "alice", ActorType: "human", ChangeClass: []string{"image"},
	}
	if err := s.InsertBatch(ctx, []event.ChangeEvent{ev}); err != nil {
		t.Fatal(err)
	}

	rows, err := s.IncidentEvents(ctx, cluster, now.Add(-time.Hour), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 incident row, got %d", len(rows))
	}
	r := rows[0]
	if r.Name != "checkout" || r.ActorType != "human" || len(r.Classes) != 1 || r.Classes[0] != "image" {
		t.Fatalf("unexpected row: %+v", r)
	}

	stats, err := s.ResourceStats(ctx, cluster, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	st, ok := stats[ResourceKey{Namespace: "payments", Kind: "Deployment", Name: "checkout"}]
	if !ok {
		t.Fatal("expected stats row for checkout (materialized view populated on insert)")
	}
	// The only activity is on the incident-window day itself, so the
	// prior-window baseline must be empty: a brand-new resource.
	if st.PerDay != 0 || st.PriorTotal != 0 || !st.LastPriorDay.IsZero() {
		t.Fatalf("want empty prior baseline, got %+v", st)
	}
}
