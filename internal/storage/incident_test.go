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

func TestBuildIncidentEventsQuery(t *testing.T) {
	from := time.Unix(100, 0).UTC()
	to := time.Unix(200, 0).UTC()

	q, args := buildIncidentEventsQuery(IncidentFilter{Cluster: "c1", From: from, To: to})
	for _, want := range []string{
		"FROM change_events",
		"cluster = ?",
		"event_time >= ?",
		"event_time <= ?",
		"dry_run = 0",
		"ORDER BY event_time DESC",
		"LIMIT ?",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("query missing %q", want)
		}
	}
	if strings.Contains(q, "kind IN") || strings.Contains(q, "operation IN") {
		t.Errorf("unfiltered query must not constrain kind/operation: %s", q)
	}
	if len(args) != 4 { // cluster, from, to, limit
		t.Fatalf("want 4 args, got %d: %v", len(args), args)
	}

	q, args = buildIncidentEventsQuery(IncidentFilter{
		Cluster: "c1", From: from, To: to,
		Kinds:      []string{"Deployment", "ConfigMap"},
		Operations: []string{"UPDATE"},
	})
	if !strings.Contains(q, "kind IN (?)") || !strings.Contains(q, "operation IN (?)") {
		t.Errorf("filtered query missing kind/operation conditions: %s", q)
	}
	if len(args) != 6 { // cluster, from, to, kinds, operations, limit
		t.Fatalf("want 6 args, got %d: %v", len(args), args)
	}
}

func TestIncidentQueriesShape(t *testing.T) {
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

	f := IncidentFilter{Cluster: cluster, From: now.Add(-time.Hour), To: now.Add(time.Minute)}
	rows, err := s.IncidentEvents(ctx, f)
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

	// Server-side kind/operation filters.
	f.Kinds = []string{"ConfigMap"}
	if rows, err = s.IncidentEvents(ctx, f); err != nil || len(rows) != 0 {
		t.Fatalf("kind filter: want 0 rows, got %d (err %v)", len(rows), err)
	}
	f.Kinds = []string{"Deployment"}
	f.Operations = []string{"UPDATE"}
	if rows, err = s.IncidentEvents(ctx, f); err != nil || len(rows) != 1 {
		t.Fatalf("kind+op filter: want 1 row, got %d (err %v)", len(rows), err)
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
