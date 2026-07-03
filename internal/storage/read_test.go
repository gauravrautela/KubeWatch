package storage

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gauravrautela/kubewatch/internal/event"
)

func readTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("set CLICKHOUSE_DSN to run storage read integration tests")
	}
	s, err := New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return s
}

func seed(t *testing.T, s *Store, cluster, kind, name string, when time.Time) string {
	t.Helper()
	id := uuid.NewString()
	ev := event.ChangeEvent{
		EventID: id, EventTime: when, Cluster: cluster, Source: "webhook",
		Operation: event.OpUpdate, Kind: kind, Namespace: "default", Name: name,
		UserName: "alice", NewObject: `{"spec":{"replicas":3}}`,
		Diff: `[{"path":"spec.replicas","op":"replace","old":2,"new":3}]`,
	}
	if err := s.InsertBatch(context.Background(), []event.ChangeEvent{ev}); err != nil {
		t.Fatalf("seed insert: %v", err)
	}
	return id
}

func TestReadRoundTrip(t *testing.T) {
	s := readTestStore(t)
	defer s.Close()
	ctx := context.Background()

	tag := uuid.NewString()[:8]
	now := time.Now().UTC().Truncate(time.Millisecond)
	id := seed(t, s, "read-"+tag, "Deployment", "web-"+tag, now)

	// ListEvents with a filter finds the seeded row.
	page, err := s.ListEvents(ctx, ListParams{Filter: Filter{Cluster: "read-" + tag}, Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Rows) != 1 || page.Rows[0].EventID != id || page.Rows[0].Name != "web-"+tag {
		t.Fatalf("unexpected list result: %+v", page.Rows)
	}

	// GetEvent returns full detail including the object body.
	d, found, err := s.GetEvent(ctx, id)
	if err != nil || !found {
		t.Fatalf("get: found=%v err=%v", found, err)
	}
	if d.NewObject == "" || d.Diff == "" {
		t.Fatalf("detail missing objects/diff: %+v", d)
	}

	// GetEvent for an unknown id reports not found.
	if _, found, err := s.GetEvent(ctx, uuid.NewString()); err != nil || found {
		t.Fatalf("expected not found, got found=%v err=%v", found, err)
	}

	// Activity buckets the seeded row.
	buckets, err := s.Activity(ctx, Filter{Cluster: "read-" + tag}, "hour")
	if err != nil {
		t.Fatalf("activity: %v", err)
	}
	var total uint64
	for _, b := range buckets {
		total += b.Count
	}
	if total != 1 {
		t.Fatalf("want 1 event in activity, got %d", total)
	}

	// Facets include the seeded cluster.
	fc, err := s.Facets(ctx)
	if err != nil {
		t.Fatalf("facets: %v", err)
	}
	if !contains(fc.Clusters, "read-"+tag) {
		t.Fatalf("facets missing cluster: %v", fc.Clusters)
	}
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}
