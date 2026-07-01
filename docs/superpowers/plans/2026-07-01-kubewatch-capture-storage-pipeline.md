# KubeWatch — Change Capture & Storage Pipeline (Plan 1 of 2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the write path of KubeWatch — a per-cluster agent that captures Kubernetes changes via a validating webhook and forwards them, batched, to a central hub that stores them in ClickHouse.

**Architecture:** Hub-and-spoke. Each spoke cluster runs an **agent** hosting a validating admission webhook; it normalizes each change into a `ChangeEvent`, buffers it, and POSTs batches over HTTPS to the **hub**. The hub authenticates the agent by a per-cluster bearer token, computes a structured diff, buffers, and batch-inserts into ClickHouse. The agent never blocks cluster operations (`failurePolicy: Ignore`, always `Allowed: true`).

**Tech Stack:** Go 1.26, `github.com/ClickHouse/clickhouse-go/v2`, `github.com/google/uuid`, `k8s.io/api` + `k8s.io/apimachinery` (admission types). Standard-library `net/http` and `testing` only (no third-party test/assert libs).

**Scope note:** This is Plan 1 of 2. Plan 2 (dashboard API + web UI, the read path) is written separately after this plan lands. This plan produces working, testable software on its own: changes are captured and queryable in ClickHouse.

## Global Constraints

- Go module path: `github.com/gauravrautela/kubewatch`
- Go version: `1.26`
- ClickHouse table name: `change_events` (schema defined in Task 4)
- Test framework: standard-library `testing` only — no testify or other assert libraries
- The agent webhook must **always** respond `Allowed: true` and must never depend on the hub being reachable
- The shared wire/storage type is `event.ChangeEvent` (Task 2) — every other task consumes it verbatim
- Dependency versions: `clickhouse-go/v2 v2.46.0` (or latest v2 tag at implementation time), `google/uuid` latest, `k8s.io/api` + `k8s.io/apimachinery` matching a recent stable release (e.g. `v0.31.x`)

---

### Task 1: Project scaffold

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `internal/version/version.go`
- Test: `internal/version/version_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: the Go module and directory layout every later task builds on.

- [ ] **Step 1: Initialize the module**

Run:
```bash
cd /Users/gauravrautela/GolandProjects/KubeWatch
go mod init github.com/gauravrautela/kubewatch
```

- [ ] **Step 2: Add a .gitignore**

Create `.gitignore`:
```gitignore
/bin/
*.test
*.out
.env
```

- [ ] **Step 3: Write the failing test**

Create `internal/version/version_test.go`:
```go
package version

import "testing"

func TestString(t *testing.T) {
	if String() == "" {
		t.Fatal("version string must not be empty")
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/version/`
Expected: FAIL — `undefined: String`

- [ ] **Step 5: Implement**

Create `internal/version/version.go`:
```go
// Package version exposes the build version of KubeWatch binaries.
package version

// Version is overridable at build time with -ldflags "-X ...".
var Version = "dev"

// String returns the current version string.
func String() string { return Version }
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add go.mod .gitignore internal/version/
git commit -m "chore: scaffold kubewatch go module"
```

---

### Task 2: Shared change-event model

**Files:**
- Create: `internal/event/event.go`
- Test: `internal/event/event_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `type Operation string` with `OpCreate`, `OpUpdate`, `OpDelete`
  - `type ChangeEvent struct { ... }` — the normalized change record (fields exactly as below)
  - `type Batch struct { Events []ChangeEvent }` — the agent→hub wire payload

- [ ] **Step 1: Write the failing test**

Create `internal/event/event_test.go`:
```go
package event

import (
	"encoding/json"
	"testing"
	"time"
)

func TestChangeEventJSONRoundTrip(t *testing.T) {
	in := ChangeEvent{
		EventID:    "11111111-1111-1111-1111-111111111111",
		EventTime:  time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
		Source:     "webhook",
		Operation:  OpUpdate,
		Kind:       "Deployment",
		Namespace:  "default",
		Name:       "web",
		UserName:   "alice",
		UserGroups: []string{"system:authenticated"},
		NewObject:  `{"spec":{"replicas":3}}`,
	}
	raw, err := json.Marshal(Batch{Events: []ChangeEvent{in}})
	if err != nil {
		t.Fatal(err)
	}
	var out Batch
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Events) != 1 || out.Events[0] != in {
		t.Fatalf("round trip mismatch: %+v", out.Events)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/event/`
Expected: FAIL — undefined `ChangeEvent`, `Batch`, `OpUpdate`

- [ ] **Step 3: Implement**

Create `internal/event/event.go`:
```go
// Package event defines the normalized change-event model shared by the agent
// (which produces it) and the hub (which stores it).
package event

import "time"

// Operation is the Kubernetes verb that produced a change.
type Operation string

const (
	OpCreate Operation = "CREATE"
	OpUpdate Operation = "UPDATE"
	OpDelete Operation = "DELETE"
)

// ChangeEvent is the normalized representation of a single Kubernetes change.
// Produced by the agent from an AdmissionReview, forwarded to the hub, and
// stored as one ClickHouse row.
type ChangeEvent struct {
	EventID     string    `json:"event_id"`
	EventTime   time.Time `json:"event_time"`
	Cluster     string    `json:"cluster"` // set by the hub from the agent token
	Source      string    `json:"source"`  // "webhook" | "reconcile"
	Operation   Operation `json:"operation"`
	APIGroup    string    `json:"api_group"`
	APIVersion  string    `json:"api_version"`
	Kind        string    `json:"kind"`
	Namespace   string    `json:"namespace"`
	Name        string    `json:"name"`
	ResourceUID string    `json:"resource_uid"`
	SubResource string    `json:"sub_resource"`
	UserName    string    `json:"user_name"`
	UserGroups  []string  `json:"user_groups"`
	UserUID     string    `json:"user_uid"`
	UserAgent   string    `json:"user_agent"` // reserved; webhook source leaves empty
	DryRun      bool      `json:"dry_run"`
	OldObject   string    `json:"old_object"` // raw JSON, before
	NewObject   string    `json:"new_object"` // raw JSON, after
	Diff        string    `json:"diff"`       // structured JSON diff, computed at hub
}

// Batch is the payload agents POST to the hub ingest API.
type Batch struct {
	Events []ChangeEvent `json:"events"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/event/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/event/
git commit -m "feat: add shared ChangeEvent model"
```

---

### Task 3: Structured JSON diff engine

**Files:**
- Create: `internal/diff/diff.go`
- Test: `internal/diff/diff_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `type Change struct { Path string; Op string; Old any; New any }`
  - `func Compute(oldJSON, newJSON []byte) ([]byte, error)` — returns a JSON array of `Change` (sorted by path, deterministic). Empty `oldJSON` ⇒ all `add`; empty `newJSON` ⇒ all `remove`.

- [ ] **Step 1: Write the failing test**

Create `internal/diff/diff_test.go`:
```go
package diff

import (
	"encoding/json"
	"testing"
)

func computeChanges(t *testing.T, oldJSON, newJSON string) []Change {
	t.Helper()
	raw, err := Compute([]byte(oldJSON), []byte(newJSON))
	if err != nil {
		t.Fatal(err)
	}
	var got []Change
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestComputeReplace(t *testing.T) {
	got := computeChanges(t,
		`{"spec":{"replicas":2}}`,
		`{"spec":{"replicas":3}}`)
	if len(got) != 1 {
		t.Fatalf("want 1 change, got %d: %+v", len(got), got)
	}
	c := got[0]
	if c.Path != "spec.replicas" || c.Op != "replace" {
		t.Fatalf("unexpected change: %+v", c)
	}
	if c.Old.(float64) != 2 || c.New.(float64) != 3 {
		t.Fatalf("unexpected old/new: %+v", c)
	}
}

func TestComputeAddAndRemove(t *testing.T) {
	got := computeChanges(t,
		`{"a":1}`,
		`{"b":2}`)
	ops := map[string]string{}
	for _, c := range got {
		ops[c.Path] = c.Op
	}
	if ops["a"] != "remove" || ops["b"] != "add" {
		t.Fatalf("unexpected ops: %+v", ops)
	}
}

func TestComputeCreate(t *testing.T) {
	got := computeChanges(t, ``, `{"a":1}`)
	if len(got) != 1 || got[0].Op != "add" || got[0].Path != "a" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestComputeNoChange(t *testing.T) {
	got := computeChanges(t, `{"a":1}`, `{"a":1}`)
	if len(got) != 0 {
		t.Fatalf("want no changes, got %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/diff/`
Expected: FAIL — undefined `Compute`, `Change`

- [ ] **Step 3: Implement**

Create `internal/diff/diff.go`:
```go
// Package diff computes a structured, path-oriented diff between two JSON
// documents (a Kubernetes object before and after a change).
package diff

import (
	"encoding/json"
	"reflect"
	"sort"
)

// Change describes one changed field path. Op is "add", "remove", or "replace".
type Change struct {
	Path string `json:"path"`
	Op   string `json:"op"`
	Old  any    `json:"old,omitempty"`
	New  any    `json:"new,omitempty"`
}

// Compute returns a JSON array of Change describing how newJSON differs from
// oldJSON. Nested objects are walked key by key; arrays and scalars are compared
// as whole values. Output order is deterministic (sorted by path).
func Compute(oldJSON, newJSON []byte) ([]byte, error) {
	var oldV, newV any
	if len(oldJSON) > 0 {
		if err := json.Unmarshal(oldJSON, &oldV); err != nil {
			return nil, err
		}
	}
	if len(newJSON) > 0 {
		if err := json.Unmarshal(newJSON, &newV); err != nil {
			return nil, err
		}
	}
	var changes []Change
	walk("", oldV, newV, &changes)
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	if changes == nil {
		changes = []Change{}
	}
	return json.Marshal(changes)
}

func walk(path string, oldV, newV any, out *[]Change) {
	switch {
	case oldV == nil && newV == nil:
		return
	case oldV == nil:
		*out = append(*out, Change{Path: path, Op: "add", New: newV})
	case newV == nil:
		*out = append(*out, Change{Path: path, Op: "remove", Old: oldV})
	default:
		oldMap, oldIsMap := oldV.(map[string]any)
		newMap, newIsMap := newV.(map[string]any)
		if oldIsMap && newIsMap {
			for _, k := range unionKeys(oldMap, newMap) {
				walk(join(path, k), oldMap[k], newMap[k], out)
			}
			return
		}
		if !reflect.DeepEqual(oldV, newV) {
			*out = append(*out, Change{Path: path, Op: "replace", Old: oldV, New: newV})
		}
	}
}

func unionKeys(a, b map[string]any) []string {
	seen := map[string]struct{}{}
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/diff/`
Expected: PASS (all four tests)

- [ ] **Step 5: Commit**

```bash
git add internal/diff/
git commit -m "feat: add structured JSON diff engine"
```

---

### Task 4: ClickHouse storage layer

**Files:**
- Create: `internal/storage/clickhouse.go`
- Test: `internal/storage/clickhouse_test.go`

**Interfaces:**
- Consumes: `event.ChangeEvent` (Task 2)
- Produces:
  - `func New(dsn string) (*Store, error)`
  - `func (s *Store) Ping(ctx context.Context) error`
  - `func (s *Store) Migrate(ctx context.Context) error`
  - `func (s *Store) InsertBatch(ctx context.Context, events []event.ChangeEvent) error`
  - `func (s *Store) Close() error`
  - `const Schema string` — the `change_events` DDL

- [ ] **Step 1: Add dependencies**

Run:
```bash
go get github.com/ClickHouse/clickhouse-go/v2@latest
go get github.com/google/uuid@latest
```

- [ ] **Step 2: Write the failing integration test**

This test talks to a real ClickHouse; it **skips** when `CLICKHOUSE_DSN` is unset so unit runs stay green. Create `internal/storage/clickhouse_test.go`:
```go
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/storage/`
Expected: FAIL — undefined `New`, `Store`, etc. (compile error)

- [ ] **Step 4: Implement**

Create `internal/storage/clickhouse.go`:
```go
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
```

- [ ] **Step 5: Tidy modules and build**

Run:
```bash
go mod tidy
go build ./...
```
Expected: builds clean.

- [ ] **Step 6: Run the integration test against a local ClickHouse**

Start ClickHouse and run the test:
```bash
docker run -d --name kw-ch -p 9000:9000 -p 8123:8123 clickhouse/clickhouse-server:latest
CLICKHOUSE_DSN="clickhouse://default:@localhost:9000/default" go test ./internal/storage/ -v
```
Expected: PASS (`TestInsertBatch`). Without the env var, `go test ./internal/storage/` skips.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/storage/
git commit -m "feat: add ClickHouse storage layer with schema and batch insert"
```

---

### Task 5: Hub ingest API (auth + batcher + handler)

**Files:**
- Create: `internal/ingest/auth.go`
- Create: `internal/ingest/batcher.go`
- Create: `internal/ingest/handler.go`
- Test: `internal/ingest/ingest_test.go`

**Interfaces:**
- Consumes: `event.ChangeEvent`, `event.Batch` (Task 2); `diff.Compute` (Task 3); an `Inserter` satisfied by `*storage.Store` (Task 4)
- Produces:
  - `type Inserter interface { InsertBatch(ctx context.Context, events []event.ChangeEvent) error }`
  - `type Authenticator interface { ClusterFor(token string) (string, bool) }`
  - `type StaticAuth map[string]string` (implements `Authenticator`)
  - `type Batcher` with `NewBatcher(inserter Inserter, maxSize int, maxWait time.Duration) *Batcher`, `Add(e event.ChangeEvent) bool`, `Run(ctx context.Context)`, `Dropped() int64`
  - `type Handler` with `NewHandler(auth Authenticator, batcher *Batcher) *Handler` implementing `http.Handler`

- [ ] **Step 1: Write the failing test**

Create `internal/ingest/ingest_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ingest/`
Expected: FAIL — undefined `NewBatcher`, `NewHandler`, `StaticAuth`

- [ ] **Step 3: Implement the authenticator**

Create `internal/ingest/auth.go`:
```go
// Package ingest is the hub-side API that receives, buffers, and stores events.
package ingest

// Authenticator maps a bearer token to the cluster identity it represents.
type Authenticator interface {
	ClusterFor(token string) (cluster string, ok bool)
}

// StaticAuth is a fixed token->cluster map loaded from configuration.
type StaticAuth map[string]string

// ClusterFor implements Authenticator.
func (a StaticAuth) ClusterFor(token string) (string, bool) {
	c, ok := a[token]
	return c, ok
}
```

- [ ] **Step 4: Implement the batcher**

Create `internal/ingest/batcher.go`:
```go
package ingest

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/gauravrautela/kubewatch/internal/event"
)

// Inserter persists a batch of events. Satisfied by *storage.Store.
type Inserter interface {
	InsertBatch(ctx context.Context, events []event.ChangeEvent) error
}

// Batcher accumulates events and flushes them to the Inserter on a size or time
// threshold. This is the hub-side buffer that keeps ClickHouse inserts batched.
type Batcher struct {
	inserter Inserter
	in       chan event.ChangeEvent
	maxSize  int
	maxWait  time.Duration
	dropped  atomic.Int64
}

// NewBatcher constructs a Batcher. Call Run in a goroutine to start flushing.
func NewBatcher(inserter Inserter, maxSize int, maxWait time.Duration) *Batcher {
	return &Batcher{
		inserter: inserter,
		in:       make(chan event.ChangeEvent, maxSize*2),
		maxSize:  maxSize,
		maxWait:  maxWait,
	}
}

// Add enqueues an event without blocking. Returns false (and meters) when the
// internal channel is full — an honest, visible drop, never silent.
func (b *Batcher) Add(e event.ChangeEvent) bool {
	select {
	case b.in <- e:
		return true
	default:
		b.dropped.Add(1)
		return false
	}
}

// Dropped returns how many events were dropped due to a full channel.
func (b *Batcher) Dropped() int64 { return b.dropped.Load() }

// Run flushes batches until ctx is cancelled, then flushes once more.
func (b *Batcher) Run(ctx context.Context) {
	ticker := time.NewTicker(b.maxWait)
	defer ticker.Stop()
	pending := make([]event.ChangeEvent, 0, b.maxSize)

	flush := func() {
		if len(pending) == 0 {
			return
		}
		if err := b.inserter.InsertBatch(ctx, pending); err != nil {
			log.Printf("ingest: insert batch of %d failed: %v", len(pending), err)
		}
		pending = pending[:0]
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case e := <-b.in:
			pending = append(pending, e)
			if len(pending) >= b.maxSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}
```

- [ ] **Step 5: Implement the HTTP handler**

Create `internal/ingest/handler.go`:
```go
package ingest

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gauravrautela/kubewatch/internal/diff"
	"github.com/gauravrautela/kubewatch/internal/event"
)

// Handler accepts batched events from agents, authenticates the cluster, tags
// and diffs each event, and forwards them to the batcher.
type Handler struct {
	auth    Authenticator
	batcher *Batcher
}

// NewHandler constructs the ingest HTTP handler.
func NewHandler(auth Authenticator, batcher *Batcher) *Handler {
	return &Handler{auth: auth, batcher: batcher}
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(h, "Bearer "); ok {
		return after
	}
	return ""
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cluster, ok := h.auth.ClusterFor(bearerToken(r))
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var batch event.Batch
	if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	for i := range batch.Events {
		e := batch.Events[i]
		e.Cluster = cluster
		if d, err := diff.Compute([]byte(e.OldObject), []byte(e.NewObject)); err == nil {
			e.Diff = string(d)
		}
		h.batcher.Add(e)
	}
	w.WriteHeader(http.StatusAccepted)
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/ingest/ -v`
Expected: PASS (`TestBatcherFlushesOnSize`, `TestHandlerRejectsBadToken`, `TestHandlerTagsClusterAndComputesDiff`)

- [ ] **Step 7: Commit**

```bash
git add internal/ingest/
git commit -m "feat: add hub ingest API with auth, batcher, and diff-on-ingest"
```

---

### Task 6: Agent webhook (parse + handler)

**Files:**
- Create: `internal/webhook/parse.go`
- Create: `internal/webhook/handler.go`
- Test: `internal/webhook/webhook_test.go`

**Interfaces:**
- Consumes: `event.ChangeEvent` (Task 2); `k8s.io/api/admission/v1` types
- Produces:
  - `func Parse(review *admissionv1.AdmissionReview) (event.ChangeEvent, bool, error)` — bool is false for operations we skip (e.g. CONNECT) or a nil request
  - `type Sink func(event.ChangeEvent)`
  - `type Handler` with `NewHandler(sink Sink) *Handler` implementing `http.Handler`; always responds `Allowed: true`

- [ ] **Step 1: Add the admission dependency**

Run:
```bash
go get k8s.io/api@latest
go get k8s.io/apimachinery@latest
```

- [ ] **Step 2: Write the failing test**

Create `internal/webhook/webhook_test.go`:
```go
package webhook

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/gauravrautela/kubewatch/internal/event"
)

func sampleReview() *admissionv1.AdmissionReview {
	return &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{APIVersion: "admission.k8s.io/v1", Kind: "AdmissionReview"},
		Request: &admissionv1.AdmissionRequest{
			UID:       types.UID("abc"),
			Operation: admissionv1.Update,
			Kind:      metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
			Namespace: "default",
			Name:      "web",
			UserInfo:  authenticationv1.UserInfo{Username: "alice", UID: "u1", Groups: []string{"g1"}},
			OldObject: runtime.RawExtension{Raw: []byte(`{"metadata":{"uid":"xyz"},"spec":{"replicas":2}}`)},
			Object:    runtime.RawExtension{Raw: []byte(`{"metadata":{"uid":"xyz"},"spec":{"replicas":3}}`)},
		},
	}
}

func TestParseUpdate(t *testing.T) {
	ev, ok, err := Parse(sampleReview())
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if ev.Operation != event.OpUpdate || ev.Kind != "Deployment" || ev.Name != "web" {
		t.Fatalf("bad event: %+v", ev)
	}
	if ev.UserName != "alice" || ev.ResourceUID != "xyz" || ev.Source != "webhook" {
		t.Fatalf("bad attribution: %+v", ev)
	}
	if ev.EventID == "" {
		t.Fatal("event id must be set")
	}
}

func TestParseSkipsConnect(t *testing.T) {
	r := sampleReview()
	r.Request.Operation = admissionv1.Connect
	_, ok, err := Parse(r)
	if err != nil || ok {
		t.Fatalf("connect should be skipped: ok=%v err=%v", ok, err)
	}
}

func TestHandlerAlwaysAllows(t *testing.T) {
	var got event.ChangeEvent
	h := NewHandler(func(e event.ChangeEvent) { got = e })

	body, _ := json.Marshal(sampleReview())
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var resp admissionv1.AdmissionReview
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Response == nil || !resp.Response.Allowed {
		t.Fatal("webhook must always allow")
	}
	if got.Name != "web" {
		t.Fatalf("sink did not receive event: %+v", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/webhook/`
Expected: FAIL — undefined `Parse`, `NewHandler`

- [ ] **Step 4: Implement Parse**

Create `internal/webhook/parse.go`:
```go
// Package webhook turns Kubernetes admission requests into change events and
// serves the validating webhook endpoint. It never blocks admission.
package webhook

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	admissionv1 "k8s.io/api/admission/v1"

	"github.com/gauravrautela/kubewatch/internal/event"
)

type objectMeta struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		UID       string `json:"uid"`
	} `json:"metadata"`
}

func opFor(o admissionv1.Operation) (event.Operation, bool) {
	switch o {
	case admissionv1.Create:
		return event.OpCreate, true
	case admissionv1.Update:
		return event.OpUpdate, true
	case admissionv1.Delete:
		return event.OpDelete, true
	default:
		return "", false
	}
}

// Parse converts an AdmissionReview request into a normalized ChangeEvent. The
// bool is false when the request is nil or the operation is not one we record
// (e.g. CONNECT), in which case the caller should skip it.
func Parse(review *admissionv1.AdmissionReview) (event.ChangeEvent, bool, error) {
	req := review.Request
	if req == nil {
		return event.ChangeEvent{}, false, nil
	}
	op, ok := opFor(req.Operation)
	if !ok {
		return event.ChangeEvent{}, false, nil
	}

	ev := event.ChangeEvent{
		EventID:     uuid.NewString(),
		EventTime:   time.Now().UTC(),
		Source:      "webhook",
		Operation:   op,
		APIGroup:    req.Kind.Group,
		APIVersion:  req.Kind.Version,
		Kind:        req.Kind.Kind,
		Namespace:   req.Namespace,
		Name:        req.Name,
		SubResource: req.SubResource,
		UserName:    req.UserInfo.Username,
		UserGroups:  req.UserInfo.Groups,
		UserUID:     req.UserInfo.UID,
		OldObject:   string(req.OldObject.Raw),
		NewObject:   string(req.Object.Raw),
	}
	if req.DryRun != nil {
		ev.DryRun = *req.DryRun
	}

	// On CREATE the request Name/UID may be empty; on DELETE the new object is
	// empty. Pull identity from whichever object body is present.
	raw := req.Object.Raw
	if len(raw) == 0 {
		raw = req.OldObject.Raw
	}
	if len(raw) > 0 {
		var m objectMeta
		if err := json.Unmarshal(raw, &m); err != nil {
			return event.ChangeEvent{}, false, err
		}
		if ev.Name == "" {
			ev.Name = m.Metadata.Name
		}
		if ev.Namespace == "" {
			ev.Namespace = m.Metadata.Namespace
		}
		ev.ResourceUID = m.Metadata.UID
	}

	return ev, true, nil
}
```

- [ ] **Step 5: Implement the handler**

Create `internal/webhook/handler.go`:
```go
package webhook

import (
	"encoding/json"
	"io"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"

	"github.com/gauravrautela/kubewatch/internal/event"
)

// Sink receives parsed change events. It must not block for long.
type Sink func(event.ChangeEvent)

// Handler serves the validating webhook. It records the change and always
// admits the request (never blocks cluster operations).
type Handler struct {
	sink Sink
}

// NewHandler constructs a webhook Handler that feeds parsed events to sink.
func NewHandler(sink Sink) *Handler {
	return &Handler{sink: sink}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	var review admissionv1.AdmissionReview
	if err := json.Unmarshal(body, &review); err != nil {
		http.Error(w, "decode", http.StatusBadRequest)
		return
	}

	if ev, ok, perr := Parse(&review); perr == nil && ok {
		h.sink(ev)
	}

	resp := admissionv1.AdmissionReview{TypeMeta: review.TypeMeta}
	if review.Request != nil {
		resp.Response = &admissionv1.AdmissionResponse{
			UID:     review.Request.UID,
			Allowed: true,
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run:
```bash
go mod tidy
go test ./internal/webhook/ -v
```
Expected: PASS (`TestParseUpdate`, `TestParseSkipsConnect`, `TestHandlerAlwaysAllows`)

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/webhook/
git commit -m "feat: add agent admission webhook parse and handler"
```

---

### Task 7: Agent buffer and forwarder

**Files:**
- Create: `internal/buffer/buffer.go`
- Create: `internal/forward/forward.go`
- Test: `internal/buffer/buffer_test.go`
- Test: `internal/forward/forward_test.go`

**Interfaces:**
- Consumes: `event.ChangeEvent`, `event.Batch` (Task 2)
- Produces:
  - `buffer`: `func New(max int) *Buffer`, `func (b *Buffer) Add(e event.ChangeEvent)`, `func (b *Buffer) Drain() []event.ChangeEvent`, `func (b *Buffer) Dropped() int64`. `Add` has signature `func(event.ChangeEvent)` so it satisfies `webhook.Sink`.
  - `forward`: `func New(url, token string) *Client`, `func (c *Client) Send(ctx context.Context, events []event.ChangeEvent) error`

- [ ] **Step 1: Write the failing buffer test**

Create `internal/buffer/buffer_test.go`:
```go
package buffer

import (
	"testing"

	"github.com/gauravrautela/kubewatch/internal/event"
)

func TestDrainReturnsAndClears(t *testing.T) {
	b := New(10)
	b.Add(event.ChangeEvent{EventID: "1"})
	b.Add(event.ChangeEvent{EventID: "2"})
	got := b.Drain()
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	if len(b.Drain()) != 0 {
		t.Fatal("buffer should be empty after drain")
	}
}

func TestOverflowDropsOldestAndMeters(t *testing.T) {
	b := New(2)
	b.Add(event.ChangeEvent{EventID: "1"})
	b.Add(event.ChangeEvent{EventID: "2"})
	b.Add(event.ChangeEvent{EventID: "3"}) // drops "1"
	if b.Dropped() != 1 {
		t.Fatalf("want 1 dropped, got %d", b.Dropped())
	}
	got := b.Drain()
	if len(got) != 2 || got[0].EventID != "2" || got[1].EventID != "3" {
		t.Fatalf("unexpected retained events: %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/buffer/`
Expected: FAIL — undefined `New`

- [ ] **Step 3: Implement the buffer**

Create `internal/buffer/buffer.go`:
```go
// Package buffer is the agent's bounded, in-memory queue of change events.
package buffer

import (
	"sync"
	"sync/atomic"

	"github.com/gauravrautela/kubewatch/internal/event"
)

// Buffer is a bounded FIFO of change events. When full, Add drops the oldest
// event and increments Dropped — an honest, metered gap, never silent.
type Buffer struct {
	mu      sync.Mutex
	events  []event.ChangeEvent
	max     int
	dropped atomic.Int64
}

// New returns a Buffer that holds at most max events.
func New(max int) *Buffer {
	return &Buffer{max: max}
}

// Add appends an event, dropping the oldest if the buffer is full. Its
// signature matches webhook.Sink.
func (b *Buffer) Add(e event.ChangeEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.events) >= b.max {
		b.events = b.events[1:]
		b.dropped.Add(1)
	}
	b.events = append(b.events, e)
}

// Drain returns all buffered events and clears the buffer.
func (b *Buffer) Drain() []event.ChangeEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.events) == 0 {
		return nil
	}
	out := b.events
	b.events = nil
	return out
}

// Dropped returns the number of events dropped due to overflow.
func (b *Buffer) Dropped() int64 { return b.dropped.Load() }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/buffer/`
Expected: PASS

- [ ] **Step 5: Write the failing forwarder test**

Create `internal/forward/forward_test.go`:
```go
package forward

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gauravrautela/kubewatch/internal/event"
)

func TestSendPostsBatchWithToken(t *testing.T) {
	var gotToken string
	var gotCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("Authorization")
		var b event.Batch
		json.NewDecoder(r.Body).Decode(&b)
		gotCount = len(b.Events)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	c := New(srv.URL, "secret")
	err := c.Send(context.Background(), []event.ChangeEvent{{EventID: "1"}, {EventID: "2"}})
	if err != nil {
		t.Fatal(err)
	}
	if gotToken != "Bearer secret" {
		t.Fatalf("want bearer token, got %q", gotToken)
	}
	if gotCount != 2 {
		t.Fatalf("want 2 events, got %d", gotCount)
	}
}

func TestSendRetriesThenSucceeds(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	c := New(srv.URL, "secret")
	if err := c.Send(context.Background(), []event.ChangeEvent{{EventID: "1"}}); err != nil {
		t.Fatalf("expected eventual success, got %v", err)
	}
	if attempts.Load() < 2 {
		t.Fatalf("expected a retry, got %d attempts", attempts.Load())
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `go test ./internal/forward/`
Expected: FAIL — undefined `New`

- [ ] **Step 7: Implement the forwarder**

Create `internal/forward/forward.go`:
```go
// Package forward posts batches of change events from the agent to the hub.
package forward

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gauravrautela/kubewatch/internal/event"
)

// Client posts batches to the hub ingest API with a per-cluster bearer token.
type Client struct {
	url     string
	token   string
	http    *http.Client
	retries int
	backoff time.Duration
}

// New constructs a forwarding Client targeting the hub ingest URL.
func New(url, token string) *Client {
	return &Client{
		url:     url,
		token:   token,
		http:    &http.Client{Timeout: 10 * time.Second},
		retries: 3,
		backoff: 200 * time.Millisecond,
	}
}

// Send posts the events as one batch, retrying with linear backoff. It returns
// an error only after all retries fail; the caller decides whether to drop
// (metered) or requeue.
func (c *Client) Send(ctx context.Context, events []event.ChangeEvent) error {
	if len(events) == 0 {
		return nil
	}
	body, err := json.Marshal(event.Batch{Events: events})
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt < c.retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * c.backoff):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.token)
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		lastErr = fmt.Errorf("hub returned %d", resp.StatusCode)
	}
	return lastErr
}
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/buffer/ ./internal/forward/ -v`
Expected: PASS (all four tests)

- [ ] **Step 9: Commit**

```bash
git add internal/buffer/ internal/forward/
git commit -m "feat: add agent bounded buffer and hub forwarder"
```

---

### Task 8: Wire the binaries and verify end-to-end

**Files:**
- Create: `cmd/hub/main.go`
- Create: `cmd/agent/main.go`
- Create: `deploy/validatingwebhookconfiguration.yaml`
- Create: `README.md`

**Interfaces:**
- Consumes: everything from Tasks 2–7
- Produces: two runnable binaries (`kubewatch-hub`, `kubewatch-agent`) and a deployment manifest.

- [ ] **Step 1: Implement the hub binary**

Create `cmd/hub/main.go`:
```go
// Command hub receives change events from agents and stores them in ClickHouse.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gauravrautela/kubewatch/internal/ingest"
	"github.com/gauravrautela/kubewatch/internal/storage"
)

func parseTokens(s string) ingest.StaticAuth {
	m := ingest.StaticAuth{}
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		if k, v, ok := strings.Cut(pair, "="); ok {
			m[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return m
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	dsn := os.Getenv("CLICKHOUSE_DSN")
	addr := envOr("LISTEN_ADDR", ":8080")
	auth := parseTokens(os.Getenv("AGENT_TOKENS"))

	store, err := storage.New(dsn)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	defer store.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := store.Ping(ctx); err != nil {
		log.Fatalf("clickhouse ping: %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	batcher := ingest.NewBatcher(store, 500, 2*time.Second)
	go batcher.Run(ctx)

	mux := http.NewServeMux()
	mux.Handle("/v1/events", ingest.NewHandler(auth, batcher))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		<-ctx.Done()
		sh, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = srv.Shutdown(sh)
	}()

	log.Printf("hub listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}
```

- [ ] **Step 2: Implement the agent binary**

Create `cmd/agent/main.go`:
```go
// Command agent hosts the validating webhook and forwards changes to the hub.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gauravrautela/kubewatch/internal/buffer"
	"github.com/gauravrautela/kubewatch/internal/forward"
	"github.com/gauravrautela/kubewatch/internal/webhook"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	addr := envOr("WEBHOOK_ADDR", ":8443")
	hubURL := os.Getenv("HUB_URL") // e.g. https://hub.internal/v1/events
	token := os.Getenv("CLUSTER_TOKEN")
	certFile := os.Getenv("TLS_CERT_FILE")
	keyFile := os.Getenv("TLS_KEY_FILE")

	buf := buffer.New(10000)
	client := forward.New(hubURL, token)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Periodic flush loop: drain the buffer and forward batches to the hub.
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				if events := buf.Drain(); len(events) > 0 {
					_ = client.Send(context.Background(), events)
				}
				return
			case <-ticker.C:
				events := buf.Drain()
				if len(events) == 0 {
					continue
				}
				if err := client.Send(ctx, events); err != nil {
					log.Printf("agent: forward failed, dropping %d events: %v", len(events), err)
				}
			}
		}
	}()

	mux := http.NewServeMux()
	mux.Handle("/webhook", webhook.NewHandler(buf.Add))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		<-ctx.Done()
		sh, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = srv.Shutdown(sh)
	}()

	log.Printf("agent webhook listening on %s", addr)
	if err := srv.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}
```

- [ ] **Step 3: Build both binaries**

Run:
```bash
go build -o bin/kubewatch-hub ./cmd/hub
go build -o bin/kubewatch-agent ./cmd/agent
```
Expected: both build clean.

- [ ] **Step 4: Add the webhook deployment manifest**

Create `deploy/validatingwebhookconfiguration.yaml`. The agent Service must be reachable by the API server; `failurePolicy: Ignore` guarantees KubeWatch never blocks operations. Fill `caBundle` with the base64 CA that signed the agent's serving cert.
```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: kubewatch-agent
webhooks:
  - name: changes.kubewatch.io
    admissionReviewVersions: ["v1"]
    sideEffects: None
    failurePolicy: Ignore          # never block cluster operations
    timeoutSeconds: 2
    clientConfig:
      service:
        name: kubewatch-agent
        namespace: kubewatch
        path: /webhook
        port: 8443
      caBundle: <BASE64_CA_BUNDLE>
    rules:
      - apiGroups: ["*"]
        apiVersions: ["*"]
        operations: ["CREATE", "UPDATE", "DELETE"]
        resources: ["*"]
        scope: "*"
```

- [ ] **Step 5: Write the README run instructions**

Create `README.md`:
```markdown
# KubeWatch

Hub-and-spoke Kubernetes change auditing. Agents host a validating webhook in
each cluster and forward changes to a central hub that stores them in ClickHouse.

## Run locally

Start ClickHouse:

    docker run -d --name kw-ch -p 9000:9000 clickhouse/clickhouse-server:latest

Start the hub:

    CLICKHOUSE_DSN="clickhouse://default:@localhost:9000/default" \
    AGENT_TOKENS="devtoken=local-cluster" \
    LISTEN_ADDR=":8080" \
    go run ./cmd/hub

The hub creates the `change_events` table on startup.

## Deploy an agent

The agent needs a TLS serving cert trusted by the API server (use cert-manager
or a generated CA), then:

- Deploy the agent Deployment + Service in the `kubewatch` namespace.
- Apply `deploy/validatingwebhookconfiguration.yaml` with the matching `caBundle`.
- Set `HUB_URL`, `CLUSTER_TOKEN`, `TLS_CERT_FILE`, `TLS_KEY_FILE`.

## Configuration

Hub: `CLICKHOUSE_DSN`, `LISTEN_ADDR`, `AGENT_TOKENS` (`token=cluster,token2=cluster2`).
Agent: `WEBHOOK_ADDR`, `HUB_URL`, `CLUSTER_TOKEN`, `TLS_CERT_FILE`, `TLS_KEY_FILE`.
```

- [ ] **Step 6: Manual end-to-end verification against the hub**

This confirms the full hub path (ingest → diff → batch → ClickHouse) without needing a cluster. With ClickHouse and the hub running (Step 5 commands), POST a batch as if from an agent:
```bash
curl -s -o /dev/null -w "%{http_code}\n" \
  -H "Authorization: Bearer devtoken" \
  -H "Content-Type: application/json" \
  -d '{"events":[{"event_id":"11111111-1111-1111-1111-111111111111","event_time":"2026-07-01T10:00:00Z","source":"webhook","operation":"UPDATE","kind":"Deployment","namespace":"default","name":"web","user_name":"alice","old_object":"{\"spec\":{\"replicas\":2}}","new_object":"{\"spec\":{\"replicas\":3}}"}]}' \
  http://localhost:8080/v1/events
```
Expected: prints `202`.

Then confirm the row landed with the cluster tagged and diff computed:
```bash
docker exec kw-ch clickhouse-client --query \
  "SELECT cluster, kind, name, user_name, diff FROM change_events FORMAT Vertical"
```
Expected: one row, `cluster = local-cluster`, `diff` contains `spec.replicas` `replace` `2`→`3`.

- [ ] **Step 7: Full-suite check**

Run:
```bash
go vet ./...
go test ./...
```
Expected: vet clean; all unit tests pass (storage integration test skips without `CLICKHOUSE_DSN`).

- [ ] **Step 8: Commit**

```bash
git add cmd/ deploy/ README.md
git commit -m "feat: wire hub and agent binaries with deploy manifest and docs"
```

- [ ] **Step 9 (optional): Full cluster smoke test**

On a `kind` cluster: build and load the agent image, deploy the agent + Service, apply the webhook config with a real `caBundle`, point `HUB_URL` at the hub, then `kubectl scale deployment/web --replicas=5` and confirm a new row appears in `change_events` with `user_name` = your kube identity. This is the true end-to-end proof and closes out Plan 1.

---

## Deferred to Plan 2 and beyond

- **Plan 2 (read path):** dashboard API (feed, diff viewer, search/filter, activity histogram, per-resource timeline) + web UI, all querying `change_events`.
- **Later:** periodic reconcile source in the agent (closes downtime gaps, current-state baseline); durable queue (Kafka/NATS); mTLS; alerting. Metrics counters (`buffer.Dropped()`, `batcher.Dropped()`) are exposed in code and ready to wire to a `/metrics` endpoint.
