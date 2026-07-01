package storage

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gauravrautela/kubewatch/internal/event"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("set CLICKHOUSE_DSN to run storage integration tests")
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

func TestInsertBatch(t *testing.T) {
	s := testStore(t)
	defer s.Close()

	ev := event.ChangeEvent{
		EventID:    uuid.NewString(),
		EventTime:  time.Now().UTC(),
		Cluster:    "test-cluster",
		Source:     "webhook",
		Operation:  event.OpUpdate,
		APIGroup:   "apps",
		APIVersion: "v1",
		Kind:       "Deployment",
		Namespace:  "default",
		Name:       "web",
		UserName:   "alice",
		UserGroups: []string{"system:authenticated"},
		NewObject:  `{"spec":{"replicas":3}}`,
		Diff:       `[{"path":"spec.replicas","op":"replace","old":2,"new":3}]`,
	}
	if err := s.InsertBatch(context.Background(), []event.ChangeEvent{ev}); err != nil {
		t.Fatalf("insert: %v", err)
	}
}
