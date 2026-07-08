package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gauravrautela/kubewatch/internal/classify"
	"github.com/gauravrautela/kubewatch/internal/event"
)

var errBoom = errors.New("boom")

type fakeInserter struct {
	mu         sync.Mutex
	rows       []event.ChangeEvent
	lastCtxErr error
	sawCtx     bool
	failFirst  int
}

func (f *fakeInserter) InsertBatch(ctx context.Context, events []event.ChangeEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failFirst > 0 {
		f.failFirst--
		return errBoom
	}
	f.rows = append(f.rows, events...)
	// Capture the context's error state at call time — the caller may
	// legitimately cancel this context for cleanup immediately after
	// InsertBatch returns, so checking ctx.Err() later would race with that
	// cleanup rather than reflect what InsertBatch actually observed.
	f.lastCtxErr = ctx.Err()
	f.sawCtx = true
	return nil
}

func (f *fakeInserter) lastInsertCtxErr() (err error, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastCtxErr, f.sawCtx
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

func TestBatcherDrainsAndFlushesOnShutdown(t *testing.T) {
	fake := &fakeInserter{}
	// Large size and long tick so neither triggers a flush before shutdown.
	b := NewBatcher(fake, 100, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	go b.Run(ctx)

	want := 5
	for i := 0; i < want; i++ {
		b.Add(event.ChangeEvent{EventID: string(rune('a' + i))})
	}

	// Give the events a moment to land in the channel before we cancel, so
	// this test exercises the drain-on-shutdown path rather than the
	// size/tick flush paths.
	time.Sleep(10 * time.Millisecond)
	cancel()

	waitFor(t, func() bool { return fake.count() == want })

	lastErr, ok := fake.lastInsertCtxErr()
	if !ok {
		t.Fatal("expected InsertBatch to have been called")
	}
	if lastErr != nil {
		t.Fatalf("want fresh, non-cancelled context for shutdown flush, got err: %v", lastErr)
	}
}

func TestBatcherRetriesThenInserts(t *testing.T) {
	fake := &fakeInserter{failFirst: 2}
	// Short backoff so the test runs fast; still exercises the retry path.
	b := NewBatcher(fake, 100, time.Hour)
	b.insertBackoff = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.Run(ctx)

	b.Add(event.ChangeEvent{EventID: "1"})
	// Force a flush by cancelling; the drain-on-shutdown path calls flush too.
	time.Sleep(10 * time.Millisecond)
	cancel()

	waitFor(t, func() bool { return fake.count() == 1 })
	if got := b.Dropped(); got != 0 {
		t.Fatalf("want 0 dropped, got %d", got)
	}
}

func TestBatcherDropsAfterRetriesExhausted(t *testing.T) {
	fake := &fakeInserter{failFirst: 1 << 30} // always fails
	size := 3
	b := NewBatcher(fake, size, time.Hour)
	b.insertBackoff = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.Run(ctx)

	for i := 0; i < size; i++ {
		b.Add(event.ChangeEvent{EventID: string(rune('a' + i))})
	}

	waitFor(t, func() bool { return b.Dropped() == int64(size) })
	if got := fake.count(); got != 0 {
		t.Fatalf("want 0 rows inserted, got %d", got)
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

func TestHandlerClassifiesEvents(t *testing.T) {
	fake := &fakeInserter{}
	b := NewBatcher(fake, 10, 20*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.Run(ctx)
	h := NewHandler(StaticAuth{"good": "clusterA"}, b)

	ev := event.ChangeEvent{
		EventID:   "1",
		Kind:      "Deployment",
		Operation: event.OpUpdate,
		UserName:  "system:serviceaccount:ci:deployer",
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
	if len(got.ChangeClass) != 1 || got.ChangeClass[0] != classify.ClassScale {
		t.Fatalf("want change_class [scale], got %v", got.ChangeClass)
	}
	if got.ActorType != classify.ActorServiceAccount {
		t.Fatalf("want actor_type serviceaccount, got %q", got.ActorType)
	}
}
