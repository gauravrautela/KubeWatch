# KubeWatch Dashboard API (Plan 2a of 2b) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the read-only Go JSON API for the KubeWatch dashboard — structured search, keyset-paginated feed with `since`-polling, single-event detail, activity histogram, and filter facets — over the existing ClickHouse `change_events` table.

**Architecture:** A new stateless binary `cmd/dashboard` serves a JSON API and (optionally) the built SPA's static assets. Read logic is added to the existing `internal/storage` ClickHouse store as new methods, split into a pure query-building layer (unit-testable, no DB) and thin execute/scan methods (integration-tested). HTTP handlers live in a new `internal/dashboardapi` package and depend on a `ReadStore` interface, so they are testable with a fake store.

**Tech Stack:** Go 1.26, `github.com/ClickHouse/clickhouse-go/v2`, `github.com/google/uuid`, standard-library `net/http` (Go 1.22+ method+wildcard routing) and `testing`. Reuses `internal/event` and `internal/storage` from Plan 1.

**Scope note:** This is Plan 2a of 2. Plan 2b (the React/TypeScript SPA) is written separately and consumes this API. This plan produces working, independently testable software: a running JSON API over the audit store.

## Global Constraints

- Go module path: `github.com/gauravrautela/kubewatch`
- Go version: `1.26`
- ClickHouse table: `change_events` (schema from Plan 1 — unchanged; this plan adds no migrations)
- Test framework: standard-library `testing` only — no testify
- Read-only: this plan issues only `SELECT` queries; no writes
- All ClickHouse queries use positional `?` parameter binding — never string-concatenate user input
- Reuse the existing `*storage.Store` (holds the `driver.Conn`) and `event` package; do not create a second ClickHouse connection layer
- `limit` is capped server-side: default 50, max 200
- Cursor tokens are opaque base64 of `(event_time_unix_nano, event_id)`

---

### Task 1: Pure query layer (types, cursor, query builders)

**Files:**
- Create: `internal/storage/query.go`
- Test: `internal/storage/query_test.go`

**Interfaces:**
- Consumes: `github.com/google/uuid`
- Produces (used by Task 2 and Task 3):
  - `type Filter struct { Cluster, Namespace, Kind, Name, User, Operation string; From, To time.Time }`
  - `type Cursor struct { Time time.Time; ID uuid.UUID }`
  - `func EncodeCursor(c Cursor) string`
  - `func DecodeCursor(s string) (Cursor, error)`
  - `type Row struct {...}`, `type Detail struct { Row; OldObject, NewObject, UserUID, UserAgent string }`, `type Page struct { Rows []Row; NextCursor string }`, `type Bucket struct { BucketStart time.Time; Count uint64 }`, `type Facets struct { Clusters, Kinds, Operations []string }`, `type ListParams struct { Filter Filter; Cursor, Since string; Limit int }`
  - unexported: `buildListQuery(f Filter, older, newer *Cursor, limit int) (string, []any)`, `buildActivityQuery(f Filter, bucket string) (string, []any, error)`, the `getEventQuery`/facet query consts, `listColumns`/`detailColumns` consts.

- [ ] **Step 1: Write the failing tests**

Create `internal/storage/query_test.go`:
```go
package storage

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCursorRoundTrip(t *testing.T) {
	c := Cursor{Time: time.Unix(0, 1234567890123).UTC(), ID: uuid.MustParse("11111111-1111-1111-1111-111111111111")}
	got, err := DecodeCursor(EncodeCursor(c))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Time.Equal(c.Time) || got.ID != c.ID {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, c)
	}
}

func TestDecodeCursorRejectsGarbage(t *testing.T) {
	if _, err := DecodeCursor("not-base64!!"); err == nil {
		t.Fatal("expected error for bad base64")
	}
}

func TestBuildListQueryFiltersAndKeyset(t *testing.T) {
	f := Filter{Cluster: "c1", Name: "web", From: time.Unix(100, 0).UTC()}
	older := &Cursor{Time: time.Unix(200, 0).UTC(), ID: uuid.MustParse("22222222-2222-2222-2222-222222222222")}
	q, args := buildListQuery(f, older, nil, 50)

	for _, want := range []string{
		"FROM change_events",
		"cluster = ?",
		"name ILIKE ?",
		"event_time >= ?",
		"(event_time, event_id) < (?, ?)",
		"ORDER BY event_time DESC, event_id DESC LIMIT ?",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("query missing %q\n%s", want, q)
		}
	}
	// args order: cluster, name-pattern, from, older.Time, older.ID, limit
	if len(args) != 6 {
		t.Fatalf("want 6 args, got %d: %v", len(args), args)
	}
	if args[0] != "c1" || args[1] != "%web%" || args[5] != 50 {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestBuildListQueryNoFilters(t *testing.T) {
	q, args := buildListQuery(Filter{}, nil, nil, 10)
	if strings.Contains(q, "WHERE") {
		t.Errorf("expected no WHERE clause: %s", q)
	}
	if len(args) != 1 || args[0] != 10 {
		t.Fatalf("want just the limit arg, got %v", args)
	}
}

func TestBuildActivityQueryBucketAllowlist(t *testing.T) {
	if _, _, err := buildActivityQuery(Filter{}, "century"); err == nil {
		t.Fatal("expected error for invalid bucket")
	}
	q, _, err := buildActivityQuery(Filter{Kind: "Deployment"}, "hour")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"toStartOfHour(event_time)", "count()", "GROUP BY bucket_start", "kind = ?"} {
		if !strings.Contains(q, want) {
			t.Errorf("activity query missing %q\n%s", want, q)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/storage/ -run 'TestCursor|TestDecodeCursor|TestBuildList|TestBuildActivity'`
Expected: FAIL — undefined `Cursor`, `buildListQuery`, etc.

- [ ] **Step 3: Implement the query layer**

Create `internal/storage/query.go`:
```go
package storage

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Filter holds the structured, indexable search dimensions for the dashboard.
type Filter struct {
	Cluster   string
	Namespace string
	Kind      string
	Name      string // substring, case-insensitive
	User      string // substring, case-insensitive
	Operation string
	From      time.Time // zero => unbounded
	To        time.Time // zero => unbounded
}

// Cursor is a keyset position: the (event_time, event_id) of a row.
type Cursor struct {
	Time time.Time
	ID   uuid.UUID
}

// EncodeCursor renders a cursor as an opaque base64 token.
func EncodeCursor(c Cursor) string {
	raw := fmt.Sprintf("%d:%s", c.Time.UnixNano(), c.ID.String())
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor parses an opaque cursor token.
func DecodeCursor(s string) (Cursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, fmt.Errorf("cursor: %w", err)
	}
	parts := strings.SplitN(string(b), ":", 2)
	if len(parts) != 2 {
		return Cursor{}, fmt.Errorf("cursor: malformed")
	}
	ns, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return Cursor{}, fmt.Errorf("cursor: bad time: %w", err)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return Cursor{}, fmt.Errorf("cursor: bad id: %w", err)
	}
	return Cursor{Time: time.Unix(0, ns).UTC(), ID: id}, nil
}

// Row is a lightweight change-event row for the feed/search (no full objects).
type Row struct {
	EventID     string    `json:"event_id"`
	EventTime   time.Time `json:"event_time"`
	IngestedAt  time.Time `json:"ingested_at"`
	Cluster     string    `json:"cluster"`
	Source      string    `json:"source"`
	Operation   string    `json:"operation"`
	APIGroup    string    `json:"api_group"`
	APIVersion  string    `json:"api_version"`
	Kind        string    `json:"kind"`
	Namespace   string    `json:"namespace"`
	Name        string    `json:"name"`
	ResourceUID string    `json:"resource_uid"`
	SubResource string    `json:"sub_resource"`
	UserName    string    `json:"user_name"`
	UserGroups  []string  `json:"user_groups"`
	DryRun      bool      `json:"dry_run"`
	Diff        string    `json:"diff"`
}

// Detail is a full change event including the before/after objects.
type Detail struct {
	Row
	OldObject string `json:"old_object"`
	NewObject string `json:"new_object"`
	UserUID   string `json:"user_uid"`
	UserAgent string `json:"user_agent"`
}

// Page is one keyset page of rows plus the cursor for the next (older) page.
type Page struct {
	Rows       []Row  `json:"events"`
	NextCursor string `json:"next_cursor"`
}

// Bucket is one time bucket of the activity histogram.
type Bucket struct {
	BucketStart time.Time `json:"bucket_start"`
	Count       uint64    `json:"count"`
}

// Facets are distinct low-cardinality filter values for the UI dropdowns.
type Facets struct {
	Clusters   []string `json:"clusters"`
	Kinds      []string `json:"kinds"`
	Operations []string `json:"operations"`
}

// ListParams are the inputs to ListEvents.
type ListParams struct {
	Filter Filter
	Cursor string // opaque; "" => from newest
	Since  string // opaque; "" => not a since-query
	Limit  int
}

const listColumns = `event_id, event_time, ingested_at, cluster, source, operation, ` +
	`api_group, api_version, kind, namespace, name, resource_uid, sub_resource, ` +
	`user_name, user_groups, dry_run, diff`

const detailColumns = listColumns + `, old_object, new_object, user_uid, user_agent`

const getEventQuery = "SELECT " + detailColumns + " FROM change_events WHERE event_id = ? LIMIT 1"

const (
	facetClustersQuery   = "SELECT DISTINCT cluster FROM change_events ORDER BY cluster"
	facetKindsQuery      = "SELECT DISTINCT kind FROM change_events ORDER BY kind"
	facetOperationsQuery = "SELECT DISTINCT operation FROM change_events ORDER BY operation"
)

var bucketExprs = map[string]string{
	"minute": "toStartOfMinute(event_time)",
	"hour":   "toStartOfHour(event_time)",
	"day":    "toStartOfDay(event_time)",
}

// IsValidBucket reports whether bucket is an accepted activity bucket unit.
// Handlers use it to return 400 for a bad unit before hitting the database.
func IsValidBucket(bucket string) bool {
	_, ok := bucketExprs[bucket]
	return ok
}

// condBuilder accumulates WHERE conditions and their bound args in lockstep so
// the arg order always matches the ? placeholder order.
type condBuilder struct {
	conds []string
	args  []any
}

func (b *condBuilder) add(cond string, arg any) {
	b.conds = append(b.conds, cond)
	b.args = append(b.args, arg)
}

func (b *condBuilder) where() string {
	if len(b.conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(b.conds, " AND ")
}

func filterConds(f Filter) *condBuilder {
	b := &condBuilder{}
	if f.Cluster != "" {
		b.add("cluster = ?", f.Cluster)
	}
	if f.Namespace != "" {
		b.add("namespace = ?", f.Namespace)
	}
	if f.Kind != "" {
		b.add("kind = ?", f.Kind)
	}
	if f.Operation != "" {
		b.add("operation = ?", f.Operation)
	}
	if f.Name != "" {
		b.add("name ILIKE ?", "%"+f.Name+"%")
	}
	if f.User != "" {
		b.add("user_name ILIKE ?", "%"+f.User+"%")
	}
	if !f.From.IsZero() {
		b.add("event_time >= ?", f.From)
	}
	if !f.To.IsZero() {
		b.add("event_time <= ?", f.To)
	}
	return b
}

func buildListQuery(f Filter, older, newer *Cursor, limit int) (string, []any) {
	b := filterConds(f)
	if older != nil {
		b.add("(event_time, event_id) < (?, ?)", older.Time)
		b.args = append(b.args, older.ID) // second placeholder of the tuple
	}
	if newer != nil {
		b.add("(event_time, event_id) > (?, ?)", newer.Time)
		b.args = append(b.args, newer.ID)
	}
	q := "SELECT " + listColumns + " FROM change_events" + b.where() +
		" ORDER BY event_time DESC, event_id DESC LIMIT ?"
	return q, append(b.args, limit)
}

func buildActivityQuery(f Filter, bucket string) (string, []any, error) {
	expr, ok := bucketExprs[bucket]
	if !ok {
		return "", nil, fmt.Errorf("invalid bucket %q", bucket)
	}
	b := filterConds(f)
	q := "SELECT " + expr + " AS bucket_start, count() AS c FROM change_events" +
		b.where() + " GROUP BY bucket_start ORDER BY bucket_start ASC"
	return q, b.args, nil
}
```

Note: the tuple condition `(event_time, event_id) < (?, ?)` needs TWO args (time then id). `condBuilder.add` appends one cond + one arg (the time); the id is appended directly right after so both placeholders are filled in order. This keeps one cond string mapping to two `?`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/storage/ -run 'TestCursor|TestDecodeCursor|TestBuildList|TestBuildActivity' -v`
Expected: PASS (all five tests)

- [ ] **Step 5: Commit**

```bash
git add internal/storage/query.go internal/storage/query_test.go
git commit -m "feat: add dashboard query layer (filters, cursor, query builders)"
```

---

### Task 2: Storage read methods (execute + scan)

**Files:**
- Create: `internal/storage/read.go`
- Test: `internal/storage/read_test.go`

**Interfaces:**
- Consumes: Task 1 types/builders; the existing `*Store` (`s.conn driver.Conn`); `event.ChangeEvent` + `InsertBatch` (Plan 1) for seeding in the test.
- Produces (used by Task 3):
  - `func (s *Store) ListEvents(ctx context.Context, p ListParams) (Page, error)`
  - `func (s *Store) GetEvent(ctx context.Context, id string) (Detail, bool, error)` — bool false = not found
  - `func (s *Store) Activity(ctx context.Context, f Filter, bucket string) ([]Bucket, error)`
  - `func (s *Store) Facets(ctx context.Context) (Facets, error)`

- [ ] **Step 1: Write the failing integration test**

Skips without `CLICKHOUSE_DSN` (Docker unavailable in some environments). Create `internal/storage/read_test.go`:
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/ -run TestReadRoundTrip`
Expected: FAIL — undefined `ListEvents`, `GetEvent`, `Activity`, `Facets` (compile error). (Without `CLICKHOUSE_DSN` the test would skip once it compiles — so this RED is the compile failure.)

- [ ] **Step 3: Implement the read methods**

Create `internal/storage/read.go`:
```go
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// ListEvents returns one keyset page of lightweight rows matching p.Filter,
// newest first. Cursor pages older; Since fetches rows newer than a cursor.
func (s *Store) ListEvents(ctx context.Context, p ListParams) (Page, error) {
	var older, newer *Cursor
	if p.Cursor != "" {
		c, err := DecodeCursor(p.Cursor)
		if err != nil {
			return Page{}, err
		}
		older = &c
	}
	if p.Since != "" {
		c, err := DecodeCursor(p.Since)
		if err != nil {
			return Page{}, err
		}
		newer = &c
	}
	q, args := buildListQuery(p.Filter, older, newer, p.Limit)
	rows, err := s.conn.Query(ctx, q, args...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()

	page := Page{Rows: []Row{}}
	for rows.Next() {
		var r Row
		var dry uint8
		if err := rows.Scan(
			&r.EventID, &r.EventTime, &r.IngestedAt, &r.Cluster, &r.Source, &r.Operation,
			&r.APIGroup, &r.APIVersion, &r.Kind, &r.Namespace, &r.Name, &r.ResourceUID, &r.SubResource,
			&r.UserName, &r.UserGroups, &dry, &r.Diff,
		); err != nil {
			return Page{}, err
		}
		r.DryRun = dry != 0
		page.Rows = append(page.Rows, r)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	if p.Limit > 0 && len(page.Rows) == p.Limit {
		last := page.Rows[len(page.Rows)-1]
		if id, err := uuid.Parse(last.EventID); err == nil {
			page.NextCursor = EncodeCursor(Cursor{Time: last.EventTime, ID: id})
		}
	}
	return page, nil
}

// GetEvent returns the full detail of one change. found is false if no row exists.
func (s *Store) GetEvent(ctx context.Context, id string) (Detail, bool, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return Detail{}, false, fmt.Errorf("bad event id: %w", err)
	}
	var d Detail
	var dry uint8
	err = s.conn.QueryRow(ctx, getEventQuery, uid).Scan(
		&d.EventID, &d.EventTime, &d.IngestedAt, &d.Cluster, &d.Source, &d.Operation,
		&d.APIGroup, &d.APIVersion, &d.Kind, &d.Namespace, &d.Name, &d.ResourceUID, &d.SubResource,
		&d.UserName, &d.UserGroups, &dry, &d.Diff,
		&d.OldObject, &d.NewObject, &d.UserUID, &d.UserAgent,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Detail{}, false, nil
	}
	if err != nil {
		return Detail{}, false, err
	}
	d.DryRun = dry != 0
	return d, true, nil
}

// Activity returns time-bucketed change counts matching f. bucket is one of
// "minute", "hour", "day".
func (s *Store) Activity(ctx context.Context, f Filter, bucket string) ([]Bucket, error) {
	q, args, err := buildActivityQuery(f, bucket)
	if err != nil {
		return nil, err
	}
	rows, err := s.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Bucket{}
	for rows.Next() {
		var b Bucket
		if err := rows.Scan(&b.BucketStart, &b.Count); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// Facets returns distinct low-cardinality filter values for the UI.
func (s *Store) Facets(ctx context.Context) (Facets, error) {
	clusters, err := s.distinct(ctx, facetClustersQuery)
	if err != nil {
		return Facets{}, err
	}
	kinds, err := s.distinct(ctx, facetKindsQuery)
	if err != nil {
		return Facets{}, err
	}
	ops, err := s.distinct(ctx, facetOperationsQuery)
	if err != nil {
		return Facets{}, err
	}
	return Facets{Clusters: clusters, Kinds: kinds, Operations: ops}, nil
}

func (s *Store) distinct(ctx context.Context, q string) ([]string, error) {
	rows, err := s.conn.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Verify it compiles and the suite is green**

Run:
```bash
go build ./...
go test ./internal/storage/
```
Expected: builds clean; `TestReadRoundTrip` SKIPs without `CLICKHOUSE_DSN` (the Task 1 pure tests still pass). If a local ClickHouse is available, run the live check:
```bash
CLICKHOUSE_DSN="clickhouse://default:@localhost:9000/default" go test ./internal/storage/ -run TestReadRoundTrip -v
```
Expected (with DB): PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/storage/read.go internal/storage/read_test.go
git commit -m "feat: add ClickHouse read methods (list, detail, activity, facets)"
```

---

### Task 3: Dashboard API package (router, handlers, param parsing)

**Files:**
- Create: `internal/dashboardapi/api.go`
- Create: `internal/dashboardapi/static.go`
- Test: `internal/dashboardapi/api_test.go`

**Interfaces:**
- Consumes: Task 1/2 types and read methods via a `ReadStore` interface.
- Produces (used by Task 4):
  - `type ReadStore interface { ListEvents(...); GetEvent(...); Activity(...); Facets(...) }` (signatures below)
  - `func NewRouter(store ReadStore, spaDir string) http.Handler`

- [ ] **Step 1: Write the failing tests**

Create `internal/dashboardapi/api_test.go`:
```go
package dashboardapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gauravrautela/kubewatch/internal/storage"
)

type fakeStore struct {
	page    storage.Page
	detail  storage.Detail
	found   bool
	lastP   storage.ListParams
	listErr error
}

func (f *fakeStore) ListEvents(_ context.Context, p storage.ListParams) (storage.Page, error) {
	f.lastP = p
	return f.page, f.listErr
}
func (f *fakeStore) GetEvent(_ context.Context, id string) (storage.Detail, bool, error) {
	return f.detail, f.found, nil
}
func (f *fakeStore) Activity(_ context.Context, _ storage.Filter, _ string) ([]storage.Bucket, error) {
	return []storage.Bucket{{Count: 5}}, nil
}
func (f *fakeStore) Facets(_ context.Context) (storage.Facets, error) {
	return storage.Facets{Clusters: []string{"c1"}}, nil
}

func do(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestListEventsParsesFiltersAndClampsLimit(t *testing.T) {
	fs := &fakeStore{page: storage.Page{Rows: []storage.Row{{EventID: "1"}}, NextCursor: "cur"}}
	h := NewRouter(fs, "")
	rec := do(t, h, "/api/events?cluster=c1&name=web&limit=9999")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if fs.lastP.Filter.Cluster != "c1" || fs.lastP.Filter.Name != "web" {
		t.Fatalf("filters not parsed: %+v", fs.lastP.Filter)
	}
	if fs.lastP.Limit != 200 {
		t.Fatalf("limit not clamped to 200, got %d", fs.lastP.Limit)
	}
	var page storage.Page
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.NextCursor != "cur" || len(page.Rows) != 1 {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestListEventsRejectsBadTime(t *testing.T) {
	h := NewRouter(&fakeStore{}, "")
	rec := do(t, h, "/api/events?from=not-a-time")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestListEventsRejectsBadCursor(t *testing.T) {
	h := NewRouter(&fakeStore{}, "")
	rec := do(t, h, "/api/events?cursor=!!bad!!")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestGetEventNotFound(t *testing.T) {
	h := NewRouter(&fakeStore{found: false}, "")
	rec := do(t, h, "/api/events/11111111-1111-1111-1111-111111111111")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
}

func TestActivityRejectsBadBucket(t *testing.T) {
	h := NewRouter(&fakeStore{}, "")
	rec := do(t, h, "/api/activity?bucket=century")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestFacetsOK(t *testing.T) {
	h := NewRouter(&fakeStore{}, "")
	rec := do(t, h, "/api/facets")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var fc storage.Facets
	if err := json.Unmarshal(rec.Body.Bytes(), &fc); err != nil {
		t.Fatal(err)
	}
	if len(fc.Clusters) != 1 || fc.Clusters[0] != "c1" {
		t.Fatalf("unexpected facets: %+v", fc)
	}
}

func TestHealthz(t *testing.T) {
	rec := do(t, NewRouter(&fakeStore{}, ""), "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/dashboardapi/`
Expected: FAIL — undefined `NewRouter`, `ReadStore`

- [ ] **Step 3: Implement the API**

Create `internal/dashboardapi/api.go`:
```go
// Package dashboardapi serves the read-only KubeWatch dashboard JSON API.
package dashboardapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/gauravrautela/kubewatch/internal/storage"
)

const (
	defaultLimit = 50
	maxLimit     = 200
	queryTimeout = 30 * time.Second
)

// ReadStore is the read surface the API needs; satisfied by *storage.Store.
type ReadStore interface {
	ListEvents(ctx context.Context, p storage.ListParams) (storage.Page, error)
	GetEvent(ctx context.Context, id string) (storage.Detail, bool, error)
	Activity(ctx context.Context, f storage.Filter, bucket string) ([]storage.Bucket, error)
	Facets(ctx context.Context) (storage.Facets, error)
}

// Handler holds the API dependencies.
type Handler struct {
	store ReadStore
}

// NewRouter builds the dashboard HTTP handler. If spaDir is non-empty, static
// SPA assets are served from it with index.html fallback for client routes.
func NewRouter(store ReadStore, spaDir string) http.Handler {
	h := &Handler{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/events", h.listEvents)
	mux.HandleFunc("GET /api/events/{id}", h.getEvent)
	mux.HandleFunc("GET /api/activity", h.activity)
	mux.HandleFunc("GET /api/facets", h.facets)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	if spaDir != "" {
		mux.Handle("GET /", spaFileServer(spaDir))
	}
	return withTimeout(mux)
}

// withTimeout bounds every request (and thus every ClickHouse query issued via
// r.Context()) so a slow or huge query cannot hang the server.
func withTimeout(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *Handler) listEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f, err := parseFilter(q)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	cursor := q.Get("cursor")
	since := q.Get("since")
	if cursor != "" {
		if _, err := storage.DecodeCursor(cursor); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid cursor")
			return
		}
	}
	if since != "" {
		if _, err := storage.DecodeCursor(since); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid since cursor")
			return
		}
	}
	page, err := h.store.ListEvents(r.Context(), storage.ListParams{
		Filter: f, Cursor: cursor, Since: since, Limit: parseLimit(q),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (h *Handler) getEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid event id")
		return
	}
	d, found, err := h.store.GetEvent(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	if !found {
		writeErr(w, http.StatusNotFound, "event not found")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (h *Handler) activity(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f, err := parseFilter(q)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	bucket := q.Get("bucket")
	if bucket == "" {
		bucket = "hour"
	}
	if !storage.IsValidBucket(bucket) {
		writeErr(w, http.StatusBadRequest, "invalid bucket")
		return
	}
	buckets, err := h.store.Activity(r.Context(), f, bucket)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, buckets)
}

func (h *Handler) facets(w http.ResponseWriter, r *http.Request) {
	fc, err := h.store.Facets(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, fc)
}

func parseFilter(q url.Values) (storage.Filter, error) {
	f := storage.Filter{
		Cluster:   q.Get("cluster"),
		Namespace: q.Get("namespace"),
		Kind:      q.Get("kind"),
		Name:      q.Get("name"),
		User:      q.Get("user"),
		Operation: q.Get("operation"),
	}
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return storage.Filter{}, err
		}
		f.From = t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return storage.Filter{}, err
		}
		f.To = t
	}
	return f, nil
}

func parseLimit(q url.Values) int {
	v := q.Get("limit")
	if v == "" {
		return defaultLimit
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultLimit
	}
	if n > maxLimit {
		return maxLimit
	}
	return n
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
```

Create `internal/dashboardapi/static.go`:
```go
package dashboardapi

import (
	"net/http"
	"os"
	"path/filepath"
)

// spaFileServer serves static files from dir, falling back to index.html for
// paths that don't map to a real file (SPA client-side routing).
func spaFileServer(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := filepath.Clean(r.URL.Path)
		p := filepath.Join(dir, clean)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/dashboardapi/ -v`
Expected: PASS (all eight tests)

- [ ] **Step 5: Verify the store satisfies ReadStore**

Run:
```bash
go vet ./...
go build ./...
```
Expected: clean — this confirms `*storage.Store` satisfies `dashboardapi.ReadStore` (used by Task 4).

- [ ] **Step 6: Commit**

```bash
git add internal/dashboardapi/
git commit -m "feat: add dashboard API router, handlers, and param parsing"
```

---

### Task 4: Wire the dashboard binary

**Files:**
- Create: `cmd/dashboard/main.go`
- Test: `internal/dashboardapi/static_test.go`
- Modify: `README.md` (append a Dashboard section)

**Interfaces:**
- Consumes: `storage.New`/`Ping`/`Close`, `dashboardapi.NewRouter`.
- Produces: the runnable `kubewatch-dashboard` binary.

- [ ] **Step 1: Write the failing static-serving test**

This locks the SPA-fallback behavior that the binary relies on. Create `internal/dashboardapi/static_test.go`:
```go
package dashboardapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSPAFallbackServesIndexForUnknownPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("INDEX"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("JS"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := NewRouter(&fakeStore{}, dir)

	// A real asset is served as-is.
	rec := do(t, h, "/app.js")
	if rec.Code != http.StatusOK || rec.Body.String() != "JS" {
		t.Fatalf("asset not served: %d %q", rec.Code, rec.Body.String())
	}

	// An unknown client route falls back to index.html.
	rec = do(t, h, "/events/abc")
	if rec.Code != http.StatusOK || rec.Body.String() != "INDEX" {
		t.Fatalf("SPA fallback failed: %d %q", rec.Code, rec.Body.String())
	}

	// The API still wins over static.
	req := httptest.NewRequest(http.MethodGet, "/api/facets", nil)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK || rec2.Body.String() == "INDEX" {
		t.Fatalf("api route shadowed by static: %d %q", rec2.Code, rec2.Body.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/dashboardapi/ -run TestSPAFallback`
Expected: FAIL — the fallback path isn't asserted yet (this test fails only if the behavior is wrong; if `static.go` from Task 3 is correct it may pass. If it passes immediately, that's acceptable — proceed).

Note: if the test passes on first run because Task 3 already implemented `spaFileServer` correctly, that is fine — it is a regression lock for the binary. Continue.

- [ ] **Step 3: Implement the dashboard binary**

Create `cmd/dashboard/main.go`:
```go
// Command dashboard serves the read-only KubeWatch dashboard API (and SPA assets).
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gauravrautela/kubewatch/internal/dashboardapi"
	"github.com/gauravrautela/kubewatch/internal/storage"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	dsn := os.Getenv("CLICKHOUSE_DSN")
	addr := envOr("LISTEN_ADDR", ":8081")
	spaDir := os.Getenv("SPA_DIR")
	certFile := os.Getenv("TLS_CERT_FILE")
	keyFile := os.Getenv("TLS_KEY_FILE")

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

	srv := &http.Server{Addr: addr, Handler: dashboardapi.NewRouter(store, spaDir)}
	srvErr := make(chan error, 1)
	go func() {
		if certFile != "" && keyFile != "" {
			srvErr <- srv.ListenAndServeTLS(certFile, keyFile)
		} else {
			log.Printf("WARNING: dashboard serving cleartext HTTP on %s — front it with an authenticating TLS ingress/proxy in production (audit data is sensitive)", addr)
			srvErr <- srv.ListenAndServe()
		}
	}()
	log.Printf("dashboard listening on %s", addr)

	select {
	case err := <-srvErr:
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	case <-ctx.Done():
	}

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = srv.Shutdown(shutCtx)
	shutCancel()
}
```

- [ ] **Step 4: Build, vet, and run the full suite**

Run:
```bash
go build -o bin/kubewatch-dashboard ./cmd/dashboard
go vet ./...
go test ./...
```
Expected: binary builds; vet clean; all tests pass (storage read integration test SKIPs without `CLICKHOUSE_DSN`).

- [ ] **Step 5: Append the Dashboard section to the README**

Add to `README.md`:
```markdown

## Dashboard (read path)

The dashboard API serves the audit trail over HTTP:

    CLICKHOUSE_DSN="clickhouse://default:@localhost:9000/default" \
    LISTEN_ADDR=":8081" \
    go run ./cmd/dashboard

Endpoints: `GET /api/events` (filters: cluster, namespace, kind, name, user,
operation, from, to; pagination: cursor, since, limit≤200), `GET
/api/events/{id}`, `GET /api/activity?bucket=minute|hour|day`, `GET /api/facets`,
`GET /healthz`. Set `SPA_DIR` to serve the built SPA. Set `TLS_CERT_FILE`/`TLS_KEY_FILE`
for direct TLS.

**Security:** the dashboard exposes sensitive audit data and has no built-in auth.
In production it MUST run behind an authenticating ingress/SSO proxy.
```

- [ ] **Step 6: Manual smoke test (optional, needs ClickHouse)**

With a local ClickHouse and some rows (e.g. from the hub's Plan-1 smoke test):
```bash
CLICKHOUSE_DSN="clickhouse://default:@localhost:9000/default" go run ./cmd/dashboard &
curl -s "http://localhost:8081/api/events?limit=5" | head -c 400
curl -s "http://localhost:8081/api/facets"
curl -s "http://localhost:8081/api/activity?bucket=hour"
```
Expected: JSON responses; `/api/events` returns an `events` array + `next_cursor`.

- [ ] **Step 7: Commit**

```bash
git add cmd/dashboard/ internal/dashboardapi/static_test.go README.md
git commit -m "feat: wire kubewatch-dashboard binary with SPA serving and docs"
```

---

## Deferred to Plan 2b and beyond

- **Plan 2b (SPA):** React/TypeScript + Vite frontend (TanStack Query, Recharts, React Router) consuming this API — `FilterBar`, `EventList` (live feed), `ActivityHistogram`, `EventDetail` diff viewer, `ResourceTimeline`.
- **Later:** full-text search over object contents (ClickHouse token/ngram index + query param); SSE/WebSocket push transport; built-in SSO/OIDC auth; per-request ClickHouse statement timeouts tuned per endpoint.
