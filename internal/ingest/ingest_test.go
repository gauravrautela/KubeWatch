package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gauravrautela/kubewatch/internal/event"
)

type fakeInserter struct {
	mu   sync.Mutex
	rows []event.ChangeEvent
}

func (f *fakeInserter) InsertBatch(_ context.Context, events []event.ChangeEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, events...)
	return nil
}

func (f *fakeInserter) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rows)
}

func TestBatcherFlushesOnSize(t *testing.T) {
	fake := &fakeInserter{}
	b := NewBatcher(fake, 2, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.Run(ctx)

	b.Add(event.ChangeEvent{EventID: "1"})
	b.Add(event.ChangeEvent{EventID: "2"})

	waitFor(t, func() bool { return fake.count() == 2 })
}

func TestHandlerRejectsBadToken(t *testing.T) {
	fake := &fakeInserter{}
	b := NewBatcher(fake, 10, time.Hour)
	h := NewHandler(StaticAuth{"good": "clusterA"}, b)

	body, _ := json.Marshal(event.Batch{Events: []event.ChangeEvent{{EventID: "1"}}})
	req := httptest.NewRequest(http.MethodPost, "/v1/events", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer nope")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestHandlerTagsClusterAndComputesDiff(t *testing.T) {
	fake := &fakeInserter{}
	b := NewBatcher(fake, 10, 20*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.Run(ctx)
	h := NewHandler(StaticAuth{"good": "clusterA"}, b)

	ev := event.ChangeEvent{
		EventID:   "1",
		OldObject: `{"spec":{"replicas":2}}`,
		NewObject: `{"spec":{"replicas":3}}`,
	}
	body, _ := json.Marshal(event.Batch{Events: []event.ChangeEvent{ev}})
	req := httptest.NewRequest(http.MethodPost, "/v1/events", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer good")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d", rec.Code)
	}

	waitFor(t, func() bool { return fake.count() == 1 })
	fake.mu.Lock()
	got := fake.rows[0]
	fake.mu.Unlock()
	if got.Cluster != "clusterA" {
		t.Fatalf("want cluster tagged, got %q", got.Cluster)
	}
	if got.Diff == "" || got.Diff == "[]" {
		t.Fatalf("want non-empty diff, got %q", got.Diff)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met before deadline")
}
