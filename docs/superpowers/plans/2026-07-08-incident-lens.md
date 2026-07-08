# Incident Lens Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A ranked, noise-suppressed "what changed?" incident-triage view: ingest-time change classification, a 30-day change-frequency stats table, a deterministic scoring engine, `GET /api/incident`, and a new `/incident` SPA page.

**Architecture:** All hub-side (zero agent changes). The hub classifies each event where it already computes the diff, storing `change_class` and `actor_type` on `change_events`. A materialized view maintains per-resource daily change counts (30-day TTL) to power rarity/churn signals past the raw TTL. A new dashboard-API endpoint scores and groups the incident window's events in Go; a new React page renders the ranked suspects.

**Tech Stack:** Go 1.24, ClickHouse (clickhouse-go/v2), React + TypeScript + Tailwind + @tanstack/react-query + vitest.

**Spec:** `docs/superpowers/specs/2026-07-08-incident-lens-design.md`

## Global Constraints

- Go is NOT on PATH. Always run Go as `~/sdk/go1.24.10/bin/go` (e.g. `~/sdk/go1.24.10/bin/go test ./...`).
- Go module path: `github.com/gauravrautela/kubewatch`.
- Web commands run from `web/`: tests `npm test` (vitest run), build `npm run build`.
- Storage integration tests skip unless `CLICKHOUSE_DSN` is set (existing pattern in `internal/storage/clickhouse_test.go`). Do NOT start clusters or port-forwards yourself; unit tests must pass without ClickHouse.
- TTLs (verbatim from spec): `change_events` raw TTL `7 DAY`; `resource_change_stats` TTL `30 DAY`.
- Class values (verbatim): `rbac`, `network`, `config-data`, `image`, `env`, `resources`, `scale`, `metadata-only`, `status-only`, `other`.
- Actor values (verbatim): `unknown`, `human`, `serviceaccount`, `system`. Column default is `unknown` — pre-existing rows must never read as `human`.
- Scoring weights (verbatim): `image` 1.5 · `rbac`/`network`/`config-data` 1.4 · `env`/`resources` 1.2 · `scale` 1.0 · `other` 0.8 · `metadata-only` 0.4 · `status-only` 0.1; actor `human` 1.5 · `serviceaccount` 1.0 · `unknown` 1.0 · `system` 0.6; rarity `rare` 1.5 (gap ≥ 14 days or no prior change) · `churn` 0.2 (> 50 changes/day average); recency half-life 30 min before the incident time, 4× faster decay after.
- API limits (verbatim): `lookback` default `1h` max `24h`; `limit` default 50 max 100 ranked resource groups.
- Work on a branch: `git checkout -b feat/incident-lens` before Task 1. Commit after every task.

---

### Task 1: `internal/classify` — actor typing

**Files:**
- Create: `internal/classify/classify.go`
- Test: `internal/classify/classify_test.go`

**Interfaces:**
- Consumes: nothing (leaf package, stdlib only).
- Produces: `func ActorType(userName string) string` returning one of the exported constants `ActorUnknown`, `ActorHuman`, `ActorServiceAccount`, `ActorSystem` (values `"unknown"`, `"human"`, `"serviceaccount"`, `"system"`). Also exports class constants `ClassRBAC`, `ClassNetwork`, `ClassConfigData`, `ClassImage`, `ClassEnv`, `ClassResources`, `ClassScale`, `ClassMetadataOnly`, `ClassStatusOnly`, `ClassOther` (values as in Global Constraints). Tasks 2, 3, 5, 7 use these names.

- [ ] **Step 1: Write the failing test**

Create `internal/classify/classify_test.go`:

```go
package classify

import "testing"

func TestActorType(t *testing.T) {
	cases := []struct {
		user string
		want string
	}{
		{"gaurav.rautela", ActorHuman},
		{"alice@example.com", ActorHuman},
		{"system:serviceaccount:ci:deployer", ActorServiceAccount},
		{"system:kube-controller-manager", ActorSystem},
		{"system:node:node-1", ActorSystem},
		{"", ActorUnknown},
	}
	for _, c := range cases {
		if got := ActorType(c.user); got != c.want {
			t.Errorf("ActorType(%q) = %q, want %q", c.user, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `~/sdk/go1.24.10/bin/go test ./internal/classify/`
Expected: FAIL (build error: undefined ActorType / package missing)

- [ ] **Step 3: Write the implementation**

Create `internal/classify/classify.go`:

```go
// Package classify derives semantic change classes and actor types from
// Kubernetes change events, at hub ingest time.
package classify

import "strings"

// Change classes stored in change_events.change_class.
const (
	ClassRBAC         = "rbac"
	ClassNetwork      = "network"
	ClassConfigData   = "config-data"
	ClassImage        = "image"
	ClassEnv          = "env"
	ClassResources    = "resources"
	ClassScale        = "scale"
	ClassMetadataOnly = "metadata-only"
	ClassStatusOnly   = "status-only"
	ClassOther        = "other"
)

// Actor types stored in change_events.actor_type.
const (
	ActorUnknown        = "unknown"
	ActorHuman          = "human"
	ActorServiceAccount = "serviceaccount"
	ActorSystem         = "system"
)

// ActorType derives the actor category from a request username.
func ActorType(userName string) string {
	switch {
	case userName == "":
		return ActorUnknown
	case strings.HasPrefix(userName, "system:serviceaccount:"):
		return ActorServiceAccount
	case strings.HasPrefix(userName, "system:"):
		return ActorSystem
	default:
		return ActorHuman
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `~/sdk/go1.24.10/bin/go test ./internal/classify/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/classify/
git commit -m "feat(classify): actor typing and change-class constants"
```

---

### Task 2: `internal/classify` — change classification core

**Files:**
- Modify: `internal/classify/classify.go`
- Test: `internal/classify/classify_test.go`

**Interfaces:**
- Consumes: constants from Task 1.
- Produces: `func Classify(kind, subResource, operation, diffJSON string) []string` — `operation` is `"CREATE"`/`"UPDATE"`/`"DELETE"`; `diffJSON` is the hub-computed diff (JSON array of `{path, op, old, new}` from `internal/diff`). Returns a sorted, non-empty class list. Container-path handling (`image`/`env`/`resources`) is stubbed here and completed in Task 3 via `containerClasses(oldRaw, newRaw json.RawMessage) []string`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/classify/classify_test.go`:

```go
import "reflect" // add to imports

func TestClassifyKindAndPaths(t *testing.T) {
	cases := []struct {
		name        string
		kind        string
		subResource string
		operation   string
		diffJSON    string
		want        []string
	}{
		{"create role", "Role", "", "CREATE", `[{"path":"rules","op":"add","new":[]}]`, []string{ClassRBAC}},
		{"delete deployment", "Deployment", "", "DELETE", `[]`, []string{ClassOther}},
		{"create configmap", "ConfigMap", "", "CREATE", `[{"path":"data","op":"add"}]`, []string{ClassConfigData}},
		{"create networkpolicy", "NetworkPolicy", "", "CREATE", `[]`, []string{ClassNetwork}},
		{"status only", "Deployment", "", "UPDATE",
			`[{"path":"status.replicas","op":"replace","old":2,"new":3},{"path":"metadata.resourceVersion","op":"replace","old":"1","new":"2"}]`,
			[]string{ClassStatusOnly}},
		{"managed fields only", "Deployment", "", "UPDATE",
			`[{"path":"metadata.managedFields","op":"replace"}]`,
			[]string{ClassStatusOnly}},
		{"labels only", "Deployment", "", "UPDATE",
			`[{"path":"metadata.labels.team","op":"add","new":"payments"}]`,
			[]string{ClassMetadataOnly}},
		{"empty diff", "Deployment", "", "UPDATE", `[]`, []string{ClassStatusOnly}},
		{"unparseable diff", "Deployment", "", "UPDATE", `not-json`, []string{ClassOther}},
		{"configmap data", "ConfigMap", "", "UPDATE",
			`[{"path":"data.timeout","op":"replace","old":"5s","new":"1s"}]`,
			[]string{ClassConfigData}},
		{"service spec", "Service", "", "UPDATE",
			`[{"path":"spec.ports","op":"replace"}]`,
			[]string{ClassNetwork}},
		{"replicas", "Deployment", "", "UPDATE",
			`[{"path":"spec.replicas","op":"replace","old":2,"new":5}]`,
			[]string{ClassScale}},
		{"scale subresource", "Deployment", "scale", "UPDATE", `[]`, []string{ClassScale}},
		{"rolebinding subjects", "RoleBinding", "", "UPDATE",
			`[{"path":"subjects","op":"replace"}]`,
			[]string{ClassRBAC}},
		{"ingress status only stays noise", "Ingress", "", "UPDATE",
			`[{"path":"status.loadBalancer","op":"replace"}]`,
			[]string{ClassStatusOnly}},
		{"unmatched spec path", "Deployment", "", "UPDATE",
			`[{"path":"spec.strategy.type","op":"replace","old":"Recreate","new":"RollingUpdate"}]`,
			[]string{ClassOther}},
		{"noise plus signal keeps signal only", "Deployment", "", "UPDATE",
			`[{"path":"status.replicas","op":"replace"},{"path":"spec.replicas","op":"replace","old":2,"new":5}]`,
			[]string{ClassScale}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Classify(c.kind, c.subResource, c.operation, c.diffJSON)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Classify() = %v, want %v", got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `~/sdk/go1.24.10/bin/go test ./internal/classify/`
Expected: FAIL with "undefined: Classify"

- [ ] **Step 3: Write the implementation**

Append to `internal/classify/classify.go` (add `"encoding/json"` and `"sort"` to imports):

```go
// diffChange mirrors internal/diff.Change with raw values, so container
// arrays can be re-parsed without a package dependency.
type diffChange struct {
	Path string          `json:"path"`
	Op   string          `json:"op"`
	Old  json.RawMessage `json:"old,omitempty"`
	New  json.RawMessage `json:"new,omitempty"`
}

var rbacKinds = map[string]bool{
	"Role": true, "ClusterRole": true, "RoleBinding": true,
	"ClusterRoleBinding": true, "ServiceAccount": true,
}

var networkKinds = map[string]bool{"NetworkPolicy": true, "Ingress": true}

var configKinds = map[string]bool{"ConfigMap": true, "Secret": true}

// containerPaths are diff paths where a whole container array is replaced
// (the diff compares arrays as whole values).
var containerPaths = []string{
	"spec.containers",
	"spec.template.spec.containers",
	"spec.jobTemplate.spec.template.spec.containers",
}

var noisePaths = []string{
	"status", "metadata.resourceVersion", "metadata.generation", "metadata.managedFields",
}

// Classify derives the semantic change classes for one event. It always
// returns a sorted, non-empty list.
func Classify(kind, subResource, operation, diffJSON string) []string {
	if operation != "UPDATE" {
		return createDeleteClasses(kind)
	}
	var changes []diffChange
	if err := json.Unmarshal([]byte(diffJSON), &changes); err != nil {
		return []string{ClassOther}
	}
	set := map[string]bool{}
	substantive := false
	metaOnly := false
	for _, c := range changes {
		switch {
		case isNoisePath(c.Path):
			continue
		case isMetadataPath(c.Path):
			metaOnly = true
		default:
			substantive = true
			classifyPath(kind, c, set)
		}
	}
	if subResource == "scale" {
		set[ClassScale] = true
		substantive = true
	}
	if !substantive {
		if metaOnly {
			return []string{ClassMetadataOnly}
		}
		return []string{ClassStatusOnly}
	}
	if rbacKinds[kind] {
		set[ClassRBAC] = true
	}
	if networkKinds[kind] {
		set[ClassNetwork] = true
	}
	if len(set) == 0 {
		set[ClassOther] = true
	}
	return sortedKeys(set)
}

// classifyPath maps one substantive diff path to zero or more classes.
func classifyPath(kind string, c diffChange, set map[string]bool) {
	switch {
	case configKinds[kind] && (pathUnder(c.Path, "data") || pathUnder(c.Path, "stringData") || pathUnder(c.Path, "binaryData")):
		set[ClassConfigData] = true
	case kind == "Service" && pathUnder(c.Path, "spec"):
		set[ClassNetwork] = true
	case c.Path == "spec.replicas":
		set[ClassScale] = true
	case isContainersPath(c.Path):
		for _, cl := range containerClasses(c.Old, c.New) {
			set[cl] = true
		}
	}
}

// createDeleteClasses classifies CREATE/DELETE by kind only; such events are
// never noise.
func createDeleteClasses(kind string) []string {
	set := map[string]bool{}
	if rbacKinds[kind] {
		set[ClassRBAC] = true
	}
	if networkKinds[kind] {
		set[ClassNetwork] = true
	}
	if configKinds[kind] {
		set[ClassConfigData] = true
	}
	if len(set) == 0 {
		set[ClassOther] = true
	}
	return sortedKeys(set)
}

// containerClasses is completed in the container-classification task.
func containerClasses(oldRaw, newRaw json.RawMessage) []string {
	return []string{ClassOther}
}

func pathUnder(p, prefix string) bool {
	return p == prefix || strings.HasPrefix(p, prefix+".")
}

func isNoisePath(p string) bool {
	for _, n := range noisePaths {
		if pathUnder(p, n) {
			return true
		}
	}
	return false
}

func isMetadataPath(p string) bool {
	return pathUnder(p, "metadata.labels") || pathUnder(p, "metadata.annotations")
}

func isContainersPath(p string) bool {
	for _, cp := range containerPaths {
		if pathUnder(p, cp) {
			return true
		}
	}
	return false
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `~/sdk/go1.24.10/bin/go test ./internal/classify/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/classify/
git commit -m "feat(classify): kind- and path-based change classification"
```

---

### Task 3: `internal/classify` — element-wise container classification

**Files:**
- Modify: `internal/classify/classify.go` (replace the `containerClasses` stub)
- Test: `internal/classify/classify_test.go`

**Interfaces:**
- Consumes: `diffChange.Old`/`.New` raw JSON of whole container arrays (the diff compares arrays as whole values, so a container edit arrives as one `replace` at a containers path).
- Produces: `containerClasses(oldRaw, newRaw json.RawMessage) []string` emitting `ClassImage`, `ClassEnv`, `ClassResources` (or `[ClassOther]` for container changes it cannot attribute, e.g. args/command). Containers are matched by name; added/removed containers count as `ClassImage`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/classify/classify_test.go`:

```go
func TestClassifyContainerChanges(t *testing.T) {
	cases := []struct {
		name     string
		kind     string
		diffJSON string
		want     []string
	}{
		{"image bump", "Deployment",
			`[{"path":"spec.template.spec.containers","op":"replace","old":[{"name":"app","image":"repo/app:v1"}],"new":[{"name":"app","image":"repo/app:v2"}]}]`,
			[]string{ClassImage}},
		{"env change", "Deployment",
			`[{"path":"spec.template.spec.containers","op":"replace","old":[{"name":"app","image":"i","env":[{"name":"A","value":"1"}]}],"new":[{"name":"app","image":"i","env":[{"name":"A","value":"2"}]}]}]`,
			[]string{ClassEnv}},
		{"envFrom change", "Deployment",
			`[{"path":"spec.template.spec.containers","op":"replace","old":[{"name":"app","image":"i"}],"new":[{"name":"app","image":"i","envFrom":[{"configMapRef":{"name":"cm"}}]}]}]`,
			[]string{ClassEnv}},
		{"resources change", "Deployment",
			`[{"path":"spec.template.spec.containers","op":"replace","old":[{"name":"app","image":"i","resources":{"limits":{"cpu":"1"}}}],"new":[{"name":"app","image":"i","resources":{"limits":{"cpu":"2"}}}]}]`,
			[]string{ClassResources}},
		{"image and env together", "Deployment",
			`[{"path":"spec.template.spec.containers","op":"replace","old":[{"name":"app","image":"v1","env":[{"name":"A","value":"1"}]}],"new":[{"name":"app","image":"v2","env":[{"name":"A","value":"2"}]}]}]`,
			[]string{ClassEnv, ClassImage}},
		{"container added", "Deployment",
			`[{"path":"spec.template.spec.containers","op":"replace","old":[{"name":"app","image":"i"}],"new":[{"name":"app","image":"i"},{"name":"sidecar","image":"s"}]}]`,
			[]string{ClassImage}},
		{"container removed", "Deployment",
			`[{"path":"spec.template.spec.containers","op":"replace","old":[{"name":"app","image":"i"},{"name":"sidecar","image":"s"}],"new":[{"name":"app","image":"i"}]}]`,
			[]string{ClassImage}},
		{"command change unattributed", "Deployment",
			`[{"path":"spec.template.spec.containers","op":"replace","old":[{"name":"app","image":"i","command":["a"]}],"new":[{"name":"app","image":"i","command":["b"]}]}]`,
			[]string{ClassOther}},
		{"cronjob container path", "CronJob",
			`[{"path":"spec.jobTemplate.spec.template.spec.containers","op":"replace","old":[{"name":"job","image":"v1"}],"new":[{"name":"job","image":"v2"}]}]`,
			[]string{ClassImage}},
		{"bare pod container path", "Pod",
			`[{"path":"spec.containers","op":"replace","old":[{"name":"app","image":"v1"}],"new":[{"name":"app","image":"v2"}]}]`,
			[]string{ClassImage}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Classify(c.kind, "", "UPDATE", c.diffJSON)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Classify() = %v, want %v", got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `~/sdk/go1.24.10/bin/go test ./internal/classify/`
Expected: FAIL — container cases return `[other]` instead of the specific classes (the stub), e.g. "image bump" wants `[image]`.

- [ ] **Step 3: Replace the stub implementation**

In `internal/classify/classify.go`, add `"bytes"` to imports and replace the `containerClasses` stub with:

```go
// container is the subset of a Kubernetes container spec the classifier
// distinguishes. Remaining fields (command, args, probes, ...) fall through
// to ClassOther.
type container struct {
	Name      string          `json:"name"`
	Image     string          `json:"image"`
	Env       json.RawMessage `json:"env"`
	EnvFrom   json.RawMessage `json:"envFrom"`
	Resources json.RawMessage `json:"resources"`
}

// containerClasses compares two whole container arrays element-wise, matched
// by container name. Both arrays come from the same hub-marshaled diff, so
// byte equality on sub-documents is canonical (encoding/json sorts map keys).
func containerClasses(oldRaw, newRaw json.RawMessage) []string {
	var oldC, newC []container
	if oldRaw != nil {
		_ = json.Unmarshal(oldRaw, &oldC)
	}
	if newRaw != nil {
		_ = json.Unmarshal(newRaw, &newC)
	}
	oldByName := make(map[string]container, len(oldC))
	for _, c := range oldC {
		oldByName[c.Name] = c
	}
	set := map[string]bool{}
	for _, n := range newC {
		o, ok := oldByName[n.Name]
		if !ok {
			set[ClassImage] = true // added container
			continue
		}
		if o.Image != n.Image {
			set[ClassImage] = true
		}
		if !rawEqual(o.Env, n.Env) || !rawEqual(o.EnvFrom, n.EnvFrom) {
			set[ClassEnv] = true
		}
		if !rawEqual(o.Resources, n.Resources) {
			set[ClassResources] = true
		}
	}
	if len(newC) < len(oldC) {
		set[ClassImage] = true // removed container
	}
	if len(set) == 0 {
		return []string{ClassOther}
	}
	return sortedKeys(set)
}

// rawEqual compares raw JSON treating absent and explicit null as equal.
func rawEqual(a, b json.RawMessage) bool {
	na, nb := a, b
	if string(na) == "null" {
		na = nil
	}
	if string(nb) == "null" {
		nb = nil
	}
	return bytes.Equal(na, nb)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `~/sdk/go1.24.10/bin/go test ./internal/classify/`
Expected: PASS (all TestClassify* including Task 2 cases)

- [ ] **Step 5: Commit**

```bash
git add internal/classify/
git commit -m "feat(classify): element-wise container image/env/resources classification"
```

---

### Task 4: Event model, schema migration, TTLs, stats table

**Files:**
- Modify: `internal/event/event.go`
- Modify: `internal/storage/clickhouse.go`
- Test: `internal/storage/clickhouse_test.go` (append)
- Create: `internal/storage/schema_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `event.ChangeEvent` gains `ChangeClass []string` (json `change_class`) and `ActorType string` (json `actor_type`). Storage DDL: `change_events` gains columns `change_class Array(LowCardinality(String)) DEFAULT []` and `actor_type Enum8('unknown' = 0, 'human' = 1, 'serviceaccount' = 2, 'system' = 3) DEFAULT 'unknown'`; raw TTL becomes 7 days; new table `resource_change_stats` (SummingMergeTree keyed `(cluster, namespace, kind, name, day)`, TTL 30 days) fed by materialized view `resource_change_stats_mv`. `Store.Migrate` runs all statements; `InsertBatch` writes the two new columns.

- [ ] **Step 1: Write the failing schema tests**

Create `internal/storage/schema_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `~/sdk/go1.24.10/bin/go test ./internal/storage/ -run 'TestSchema|TestMigration|TestInsertStatement'`
Expected: FAIL with "undefined: migrationStatements" (and missing-string errors once it compiles)

- [ ] **Step 3: Update the event model**

In `internal/event/event.go`, add two fields to `ChangeEvent` after `Diff`:

```go
	ChangeClass []string `json:"change_class"` // semantic classes, computed at hub
	ActorType   string   `json:"actor_type"`   // "unknown"|"human"|"serviceaccount"|"system", computed at hub
```

- [ ] **Step 4: Update the storage schema and inserts**

In `internal/storage/clickhouse.go`:

1. Update the `Schema` doc comment: change "with a 1-day TTL" to "with a 7-day TTL".
2. In the `Schema` DDL, after the `diff          String,` line add:

```
    change_class  Array(LowCardinality(String)) DEFAULT [],
    actor_type    Enum8('unknown' = 0, 'human' = 1, 'serviceaccount' = 2, 'system' = 3) DEFAULT 'unknown',
```

3. Change the TTL line to:

```
TTL toDateTime(event_time) + INTERVAL 7 DAY
```

4. Below the `Schema` const, add:

```go
// statsSchema keeps per-resource daily change counts alive for 30 days so
// rarity/churn scoring can look past the raw event TTL. Rows are tiny (no
// object bodies).
const statsSchema = `
CREATE TABLE IF NOT EXISTS resource_change_stats (
    cluster      LowCardinality(String),
    namespace    String,
    kind         LowCardinality(String),
    name         String,
    day          Date,
    change_count UInt64
)
ENGINE = SummingMergeTree
ORDER BY (cluster, namespace, kind, name, day)
TTL day + INTERVAL 30 DAY
`

const statsMV = `
CREATE MATERIALIZED VIEW IF NOT EXISTS resource_change_stats_mv TO resource_change_stats AS
SELECT cluster, namespace, kind, name, toDate(event_time) AS day, count() AS change_count
FROM change_events
GROUP BY cluster, namespace, kind, name, day
`

// migrationStatements is every DDL statement Migrate runs, in order. The
// ALTERs bring pre-existing installs up to the current Schema; each is a
// no-op once applied.
func migrationStatements() []string {
	return []string{
		Schema,
		`ALTER TABLE change_events ADD COLUMN IF NOT EXISTS change_class Array(LowCardinality(String)) DEFAULT []`,
		`ALTER TABLE change_events ADD COLUMN IF NOT EXISTS actor_type Enum8('unknown' = 0, 'human' = 1, 'serviceaccount' = 2, 'system' = 3) DEFAULT 'unknown'`,
		`ALTER TABLE change_events MODIFY TTL toDateTime(event_time) + INTERVAL 7 DAY`,
		statsSchema,
		statsMV,
	}
}
```

5. Replace `Migrate` with:

```go
// Migrate creates and upgrades the KubeWatch tables.
func (s *Store) Migrate(ctx context.Context) error {
	for _, q := range migrationStatements() {
		if err := s.conn.Exec(ctx, q); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}
```

6. Update `insertStmt` to include the new columns:

```go
const insertStmt = `INSERT INTO change_events (
    event_id, event_time, ingested_at, cluster, source, operation,
    api_group, api_version, kind, namespace, name, resource_uid, sub_resource,
    user_name, user_groups, user_uid, user_agent, dry_run,
    old_object, new_object, diff, change_class, actor_type
)`
```

7. In `InsertBatch`, before `batch.Append`, add:

```go
		classes := e.ChangeClass
		if classes == nil {
			classes = []string{}
		}
		actor := e.ActorType
		if actor == "" {
			actor = "unknown"
		}
```

and extend the `Append` call's final line from `e.OldObject, e.NewObject, e.Diff,` to:

```go
			e.OldObject, e.NewObject, e.Diff, classes, actor,
```

- [ ] **Step 5: Run unit tests, then the full build**

Run: `~/sdk/go1.24.10/bin/go test ./internal/storage/ -run 'TestSchema|TestMigration|TestInsertStatement'`
Expected: PASS
Run: `~/sdk/go1.24.10/bin/go build ./...`
Expected: no errors

- [ ] **Step 6: Extend the integration test (runs only with CLICKHOUSE_DSN)**

In `internal/storage/clickhouse_test.go`, inside `TestInsertBatch`'s `ChangeEvent` literal, add:

```go
		ChangeClass: []string{"scale"},
		ActorType:   "human",
```

(If a later assertion in that test reads the row back, no change needed there — GetEvent columns are unchanged. This just proves inserts succeed with the new columns.)

Run: `~/sdk/go1.24.10/bin/go test ./internal/storage/`
Expected: PASS (integration tests SKIP without `CLICKHOUSE_DSN` — that's fine)

- [ ] **Step 7: Commit**

```bash
git add internal/event/ internal/storage/
git commit -m "feat(storage): change_class/actor_type columns, 7-day TTL, resource_change_stats table"
```

---

### Task 5: Classify at ingest

**Files:**
- Modify: `internal/ingest/handler.go`
- Test: `internal/ingest/ingest_test.go` (append)

**Interfaces:**
- Consumes: `classify.Classify(kind, subResource, operation, diffJSON string) []string`, `classify.ActorType(userName string) string` (Tasks 1–3); `event.ChangeEvent.ChangeClass/ActorType` (Task 4).
- Produces: every ingested event reaches the batcher with `ChangeClass` and `ActorType` populated.

- [ ] **Step 1: Write the failing test**

Append to `internal/ingest/ingest_test.go` (add `"github.com/gauravrautela/kubewatch/internal/classify"` to imports):

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `~/sdk/go1.24.10/bin/go test ./internal/ingest/ -run TestHandlerClassifiesEvents`
Expected: FAIL — `got.ChangeClass` is empty

- [ ] **Step 3: Wire classification into the handler**

In `internal/ingest/handler.go`, add `"github.com/gauravrautela/kubewatch/internal/classify"` to imports, and in `ServeHTTP` after the diff computation (`e.Diff = string(d)` block), before `h.batcher.Add(e)`:

```go
		e.ChangeClass = classify.Classify(e.Kind, e.SubResource, string(e.Operation), e.Diff)
		e.ActorType = classify.ActorType(e.UserName)
```

- [ ] **Step 4: Run the package tests**

Run: `~/sdk/go1.24.10/bin/go test ./internal/ingest/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ingest/
git commit -m "feat(ingest): derive change_class and actor_type at ingest"
```

---

### Task 6: Storage incident reads — window events and resource stats

**Files:**
- Create: `internal/storage/incident.go`
- Create: `internal/storage/incident_test.go`

**Interfaces:**
- Consumes: schema from Task 4.
- Produces (used by Task 9):

```go
type IncidentRow struct {
	EventID   string    `json:"event_id"`
	EventTime time.Time `json:"event_time"`
	Operation string    `json:"operation"`
	Kind      string    `json:"kind"`
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	UserName  string    `json:"user_name"`
	ActorType string    `json:"actor_type"`
	Classes   []string  `json:"classes"`
}
type ResourceKey struct{ Namespace, Kind, Name string }
type ResourceStats struct {
	PerDay       float64   // avg changes per active day over the 30-day window
	LastPriorDay time.Time // last day with changes strictly before the window; zero if none
	PriorTotal   uint64    // total changes before the window start day
}
func (s *Store) IncidentEvents(ctx context.Context, cluster string, from, to time.Time) ([]IncidentRow, error) // ordered event_time DESC
func (s *Store) ResourceStats(ctx context.Context, cluster string, windowStart time.Time) (map[ResourceKey]ResourceStats, error)
```

- [ ] **Step 1: Write the failing unit test**

Create `internal/storage/incident_test.go`:

```go
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
	if st.PerDay < 1 {
		t.Fatalf("want PerDay >= 1, got %v", st.PerDay)
	}
	if st.PriorTotal != 0 || !st.LastPriorDay.IsZero() {
		t.Fatalf("want no prior activity, got %+v", st)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `~/sdk/go1.24.10/bin/go test ./internal/storage/ -run 'TestStatsWindow|TestIncidentQueriesShape'`
Expected: FAIL with "undefined: statsWindow" (build error)

- [ ] **Step 3: Write the implementation**

Create `internal/storage/incident.go`:

```go
package storage

import (
	"context"
	"time"
)

// IncidentRow is a lightweight change event for incident scoring (no object
// bodies, no diff).
type IncidentRow struct {
	EventID   string    `json:"event_id"`
	EventTime time.Time `json:"event_time"`
	Operation string    `json:"operation"`
	Kind      string    `json:"kind"`
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	UserName  string    `json:"user_name"`
	ActorType string    `json:"actor_type"`
	Classes   []string  `json:"classes"`
}

// ResourceKey identifies one resource within a cluster.
type ResourceKey struct {
	Namespace string
	Kind      string
	Name      string
}

// ResourceStats summarizes a resource's 30-day change history for rarity and
// churn scoring.
type ResourceStats struct {
	PerDay       float64   // avg changes per active day over the 30-day window
	LastPriorDay time.Time // last day with changes strictly before the window; zero if none
	PriorTotal   uint64    // total changes before the window start day
}

// incidentEventsLimit caps the window scan so a pathological window cannot
// pull unbounded rows into memory.
const incidentEventsLimit = 20000

const incidentEventsQuery = `SELECT event_id, event_time, operation, kind, namespace, name, user_name, actor_type, change_class
FROM change_events
WHERE cluster = ? AND event_time >= ? AND event_time <= ? AND dry_run = 0
ORDER BY event_time DESC
LIMIT ?`

// IncidentEvents returns the cluster's non-dry-run events in [from, to],
// newest first.
func (s *Store) IncidentEvents(ctx context.Context, cluster string, from, to time.Time) ([]IncidentRow, error) {
	rows, err := s.conn.Query(ctx, incidentEventsQuery, cluster, from, to, incidentEventsLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IncidentRow{}
	for rows.Next() {
		var r IncidentRow
		if err := rows.Scan(
			&r.EventID, &r.EventTime, &r.Operation, &r.Kind, &r.Namespace,
			&r.Name, &r.UserName, &r.ActorType, &r.Classes,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// statsEpoch is the sentinel max(if(...)) yields when a resource has no
// activity before the incident window.
var statsEpoch = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)

const resourceStatsQuery = `SELECT namespace, kind, name,
    sum(change_count) AS total,
    uniqExact(day) AS days_active,
    max(if(day < ?, day, toDate('1970-01-01'))) AS last_prior_day,
    sum(if(day < ?, change_count, 0)) AS prior_total
FROM resource_change_stats
WHERE cluster = ? AND day >= ?
GROUP BY namespace, kind, name`

// statsWindow returns the UTC day the incident window starts on and the
// 30-day floor for the stats query.
func statsWindow(windowStart time.Time) (windowDay, floor time.Time) {
	windowDay = windowStart.UTC().Truncate(24 * time.Hour)
	return windowDay, windowDay.AddDate(0, 0, -30)
}

// ResourceStats returns 30-day change stats for every resource in the
// cluster, keyed by resource identity.
func (s *Store) ResourceStats(ctx context.Context, cluster string, windowStart time.Time) (map[ResourceKey]ResourceStats, error) {
	windowDay, floor := statsWindow(windowStart)
	rows, err := s.conn.Query(ctx, resourceStatsQuery, windowDay, windowDay, cluster, floor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[ResourceKey]ResourceStats{}
	for rows.Next() {
		var k ResourceKey
		var total, priorTotal, daysActive uint64
		var lastPrior time.Time
		if err := rows.Scan(&k.Namespace, &k.Kind, &k.Name, &total, &daysActive, &lastPrior, &priorTotal); err != nil {
			return nil, err
		}
		st := ResourceStats{PriorTotal: priorTotal}
		if daysActive > 0 {
			st.PerDay = float64(total) / float64(daysActive)
		}
		if lastPrior.After(statsEpoch) {
			st.LastPriorDay = lastPrior
		}
		out[k] = st
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `~/sdk/go1.24.10/bin/go test ./internal/storage/`
Expected: PASS (`TestIncidentEventsAndStatsIntegration` SKIPs without `CLICKHOUSE_DSN`)

- [ ] **Step 5: Commit**

```bash
git add internal/storage/incident.go internal/storage/incident_test.go
git commit -m "feat(storage): incident window events and 30-day resource stats reads"
```

---

### Task 7: `internal/rank` — event scoring

**Files:**
- Create: `internal/rank/weights.go`
- Create: `internal/rank/rank.go`
- Test: `internal/rank/rank_test.go`

**Interfaces:**
- Consumes: `classify` class/actor constants.
- Produces (used by Task 8 and Task 9):

```go
type Event struct {
	EventID   string
	EventTime time.Time
	Operation string
	Kind      string
	Namespace string
	Name      string
	UserName  string
	ActorType string
	Classes   []string
}
type Key struct{ Namespace, Kind, Name string }
type Stats struct {
	PerDay       float64
	LastPriorDay time.Time
	PriorTotal   uint64
}
func scoreEvent(e Event, st Stats, hasStats bool, at time.Time) (float64, []string) // 0..100 + reason chips
```

- [ ] **Step 1: Write the failing tests**

Create `internal/rank/rank_test.go`:

```go
package rank

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/gauravrautela/kubewatch/internal/classify"
)

var at = time.Date(2026, 7, 8, 14, 0, 0, 0, time.UTC)

func ev(minsBefore float64, classes []string, actor, user string) Event {
	return Event{
		EventID:   "e1",
		EventTime: at.Add(-time.Duration(minsBefore * float64(time.Minute))),
		Operation: "UPDATE",
		Kind:      "Deployment", Namespace: "payments", Name: "checkout",
		UserName: user, ActorType: actor, Classes: classes,
	}
}

func TestRecencyDecay(t *testing.T) {
	base := ev(0, []string{classify.ClassScale}, classify.ActorServiceAccount, "sa")
	s0, _ := scoreEvent(base, Stats{}, false, at)
	if math.Abs(s0-100) > 0.01 { // 100 × 1.0 recency × 1.0 scale × 1.0 sa × 1.0 no-stats
		t.Fatalf("score at incident time = %v, want 100", s0)
	}
	s30, _ := scoreEvent(ev(30, []string{classify.ClassScale}, classify.ActorServiceAccount, "sa"), Stats{}, false, at)
	if math.Abs(s30-50) > 0.01 { // one half-life
		t.Fatalf("score 30m before = %v, want 50", s30)
	}
	after := base
	after.EventTime = at.Add(7*time.Minute + 30*time.Second) // one after-half-life (30m/4)
	sAfter, _ := scoreEvent(after, Stats{}, false, at)
	if math.Abs(sAfter-50) > 0.01 {
		t.Fatalf("score 7.5m after = %v, want 50", sAfter)
	}
}

func TestScoreCapAndWeights(t *testing.T) {
	// human image change at incident time: 100 × 1.5 × 1.5 = 225 → capped 100
	s, reasons := scoreEvent(ev(0, []string{classify.ClassImage}, classify.ActorHuman, "alice"), Stats{}, false, at)
	if s != 100 {
		t.Fatalf("want capped 100, got %v", s)
	}
	if !slices.Contains(reasons, "image change") || !slices.Contains(reasons, "human: alice") {
		t.Fatalf("missing chips, got %v", reasons)
	}

	// status-only system update: 100 × 0.1 × 0.6 = 6
	s2, _ := scoreEvent(ev(0, []string{classify.ClassStatusOnly}, classify.ActorSystem, "system:x"), Stats{}, false, at)
	if math.Abs(s2-6) > 0.01 {
		t.Fatalf("status-only system = %v, want 6", s2)
	}

	// multi-class takes the max weight: [scale, image] scores as image
	s3, _ := scoreEvent(ev(0, []string{classify.ClassScale, classify.ClassImage}, classify.ActorServiceAccount, "sa"), Stats{}, false, at)
	if s3 != 100 { // 100 × 1.5 × 1.0 = 150 → capped
		t.Fatalf("multi-class = %v, want 100", s3)
	}

	// empty classes score as other (0.8)
	s4, _ := scoreEvent(ev(0, nil, classify.ActorServiceAccount, "sa"), Stats{}, false, at)
	if math.Abs(s4-80) > 0.01 {
		t.Fatalf("classless = %v, want 80", s4)
	}
}

func TestRarityAndChurn(t *testing.T) {
	base := ev(0, []string{classify.ClassScale}, classify.ActorServiceAccount, "sa")

	// churner: heavily dampened, chip present
	s, reasons := scoreEvent(base, Stats{PerDay: 200, PriorTotal: 6000, LastPriorDay: at.AddDate(0, 0, -1)}, true, at)
	if math.Abs(s-20) > 0.01 { // 100 × 1.0 × 1.0 × 0.2
		t.Fatalf("churner = %v, want 20", s)
	}
	if !slices.Contains(reasons, "changes ~200×/day") {
		t.Fatalf("missing churn chip: %v", reasons)
	}

	// rare: first change in 20 days
	s2, reasons2 := scoreEvent(base, Stats{PerDay: 1, PriorTotal: 3, LastPriorDay: at.AddDate(0, 0, -20)}, true, at)
	if math.Abs(s2-100) > 0.01 { // 100 × 1.5 capped
		t.Fatalf("rare = %v, want 100", s2)
	}
	if !slices.Contains(reasons2, "first change in 20d") {
		t.Fatalf("missing rare chip: %v", reasons2)
	}

	// brand new: no prior activity at all
	_, reasons3 := scoreEvent(base, Stats{PerDay: 1}, true, at)
	if !slices.Contains(reasons3, "new resource") {
		t.Fatalf("missing new-resource chip: %v", reasons3)
	}

	// normal cadence: neutral, no rarity chip
	s4, reasons4 := scoreEvent(base, Stats{PerDay: 3, PriorTotal: 40, LastPriorDay: at.AddDate(0, 0, -2)}, true, at)
	if math.Abs(s4-100) > 0.01 {
		t.Fatalf("normal = %v, want 100", s4)
	}
	if len(reasons4) != 0 {
		t.Fatalf("want no chips for neutral sa/scale/normal, got %v", reasons4)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `~/sdk/go1.24.10/bin/go test ./internal/rank/`
Expected: FAIL (package does not exist / undefined scoreEvent)

- [ ] **Step 3: Write the weights file**

Create `internal/rank/weights.go`:

```go
package rank

import (
	"time"

	"github.com/gauravrautela/kubewatch/internal/classify"
)

// Every tunable scoring weight lives in this file.
const (
	halfLifeBefore   = 30 * time.Minute
	afterDecayFactor = 4.0 // events after the incident time decay this much faster

	weightHuman          = 1.5
	weightServiceAccount = 1.0
	weightUnknownActor   = 1.0
	weightSystem         = 0.6

	weightRare  = 1.5
	weightChurn = 0.2

	rareGapDays    = 14
	churnPerDayMax = 50.0

	maxEventsPerSuspect = 20
)

var classWeights = map[string]float64{
	classify.ClassImage:        1.5,
	classify.ClassRBAC:         1.4,
	classify.ClassNetwork:      1.4,
	classify.ClassConfigData:   1.4,
	classify.ClassEnv:          1.2,
	classify.ClassResources:    1.2,
	classify.ClassScale:        1.0,
	classify.ClassOther:        0.8,
	classify.ClassMetadataOnly: 0.4,
	classify.ClassStatusOnly:   0.1,
}

// classChips are the human-readable reason labels per class. Only classes
// that indicate signal get a chip; neutral/noise classes stay silent except
// as implied by the low score.
var classChips = map[string]string{
	classify.ClassImage:        "image change",
	classify.ClassRBAC:         "RBAC change",
	classify.ClassNetwork:      "network change",
	classify.ClassConfigData:   "config change",
	classify.ClassEnv:          "env change",
	classify.ClassResources:    "resource limits change",
	classify.ClassScale:        "", // neutral weight: no chip
	classify.ClassOther:        "",
	classify.ClassMetadataOnly: "metadata only",
	classify.ClassStatusOnly:   "status only",
}
```

- [ ] **Step 4: Write the scoring implementation**

Create `internal/rank/rank.go`:

```go
// Package rank scores and groups change events for incident triage. Scoring
// is deterministic and every non-neutral factor emits a reason chip.
package rank

import (
	"fmt"
	"math"
	"time"

	"github.com/gauravrautela/kubewatch/internal/classify"
)

// Event is one change event in the incident window.
type Event struct {
	EventID   string
	EventTime time.Time
	Operation string
	Kind      string
	Namespace string
	Name      string
	UserName  string
	ActorType string
	Classes   []string
}

// Key identifies one resource.
type Key struct{ Namespace, Kind, Name string }

// Stats is a resource's 30-day change history.
type Stats struct {
	PerDay       float64
	LastPriorDay time.Time // zero => no activity before the window
	PriorTotal   uint64
}

// scoreEvent returns the event's score (0..100) and its reason chips.
func scoreEvent(e Event, st Stats, hasStats bool, at time.Time) (float64, []string) {
	var reasons []string
	cw, cChip := classWeight(e.Classes)
	if cChip != "" {
		reasons = append(reasons, cChip)
	}
	aw, aChip := actorWeight(e.ActorType, e.UserName)
	if aChip != "" {
		reasons = append(reasons, aChip)
	}
	rw, rChip := rarityWeight(st, hasStats, at)
	if rChip != "" {
		reasons = append(reasons, rChip)
	}
	s := 100 * recency(at, e.EventTime) * cw * aw * rw
	return math.Min(s, 100), reasons
}

// recency decays exponentially with distance from the incident time; events
// after it are kept for clock skew but decay afterDecayFactor times faster.
func recency(at, t time.Time) float64 {
	if !t.After(at) {
		return math.Pow(0.5, at.Sub(t).Minutes()/halfLifeBefore.Minutes())
	}
	return math.Pow(0.5, t.Sub(at).Minutes()/(halfLifeBefore.Minutes()/afterDecayFactor))
}

// classWeight is the max weight across the event's classes, with the chip of
// the class that supplied it. Classless events score as "other".
func classWeight(classes []string) (float64, string) {
	if len(classes) == 0 {
		return classWeights[classify.ClassOther], classChips[classify.ClassOther]
	}
	best, chip := 0.0, ""
	for _, c := range classes {
		w, ok := classWeights[c]
		if !ok {
			w = classWeights[classify.ClassOther]
		}
		if w > best {
			best, chip = w, classChips[c]
		}
	}
	return best, chip
}

func actorWeight(actorType, userName string) (float64, string) {
	switch actorType {
	case classify.ActorHuman:
		return weightHuman, "human: " + userName
	case classify.ActorServiceAccount:
		return weightServiceAccount, ""
	case classify.ActorSystem:
		return weightSystem, ""
	default:
		return weightUnknownActor, ""
	}
}

// rarityWeight dampens churners and boosts rarely-touched or brand-new
// resources. Missing stats are neutral so the endpoint works from day one.
func rarityWeight(st Stats, hasStats bool, at time.Time) (float64, string) {
	if !hasStats {
		return 1.0, ""
	}
	if st.PerDay > churnPerDayMax {
		return weightChurn, fmt.Sprintf("changes ~%.0f×/day", st.PerDay)
	}
	if st.PriorTotal == 0 {
		return weightRare, "new resource"
	}
	gapDays := int(at.Sub(st.LastPriorDay).Hours() / 24)
	if gapDays >= rareGapDays {
		return weightRare, fmt.Sprintf("first change in %dd", gapDays)
	}
	return 1.0, ""
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `~/sdk/go1.24.10/bin/go test ./internal/rank/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/rank/
git commit -m "feat(rank): deterministic event scoring with reason chips"
```

---

### Task 8: `internal/rank` — grouping and ranking

**Files:**
- Modify: `internal/rank/rank.go`
- Test: `internal/rank/rank_test.go` (append)

**Interfaces:**
- Consumes: `scoreEvent` (Task 7).
- Produces (used by Task 9; JSON shapes are the API contract used by Task 10's TS types):

```go
type EventSummary struct {
	EventID   string    `json:"event_id"`
	EventTime time.Time `json:"event_time"`
	Operation string    `json:"operation"`
	Classes   []string  `json:"classes"`
	ActorType string    `json:"actor_type"`
	UserName  string    `json:"user_name"`
}
type Suspect struct {
	Namespace       string         `json:"namespace"`
	Kind            string         `json:"kind"`
	Name            string         `json:"name"`
	Score           int            `json:"score"`
	Reasons         []string       `json:"reasons"`
	EventCount      int            `json:"event_count"`
	LatestEventTime time.Time      `json:"latest_event_time"`
	Events          []EventSummary `json:"events"`
}
// events must be ordered newest-first (IncidentEvents guarantees this).
func Rank(events []Event, stats map[Key]Stats, at time.Time, limit int) []Suspect
```

- [ ] **Step 1: Write the failing tests**

Append to `internal/rank/rank_test.go`:

```go
func TestRankGroupsAndSorts(t *testing.T) {
	churnKey := Key{Namespace: "infra", Kind: "ConfigMap", Name: "leader-lock"}
	stats := map[Key]Stats{
		churnKey: {PerDay: 300, PriorTotal: 9000, LastPriorDay: at.AddDate(0, 0, -1)},
	}
	mk := func(id string, minsBefore float64, ns, kind, name string, classes []string, actor string) Event {
		e := ev(minsBefore, classes, actor, "u-"+id)
		e.EventID, e.Namespace, e.Kind, e.Name = id, ns, kind, name
		return e
	}
	events := []Event{ // newest first, matching IncidentEvents order
		mk("a", 1, "infra", "ConfigMap", "leader-lock", []string{classify.ClassConfigData}, classify.ActorSystem),
		mk("b", 2, "payments", "Deployment", "checkout", []string{classify.ClassImage}, classify.ActorHuman),
		mk("c", 5, "payments", "Deployment", "checkout", []string{classify.ClassScale}, classify.ActorServiceAccount),
		mk("d", 8, "infra", "ConfigMap", "leader-lock", []string{classify.ClassConfigData}, classify.ActorSystem),
	}

	suspects := Rank(events, stats, at, 50)
	if len(suspects) != 2 {
		t.Fatalf("want 2 suspects, got %d", len(suspects))
	}
	top := suspects[0]
	if top.Name != "checkout" {
		t.Fatalf("want fresh image change ranked above churner, got %q", top.Name)
	}
	if top.EventCount != 2 || len(top.Events) != 2 {
		t.Fatalf("want checkout grouped (2 events), got count=%d events=%d", top.EventCount, len(top.Events))
	}
	if top.Events[0].EventID != "b" {
		t.Fatalf("want events newest-first, got %q first", top.Events[0].EventID)
	}
	if top.Score != 100 {
		t.Fatalf("want best-event score 100, got %d", top.Score)
	}
	if !top.LatestEventTime.Equal(at.Add(-2 * time.Minute)) {
		t.Fatalf("latest_event_time = %v", top.LatestEventTime)
	}
	if suspects[1].Score >= top.Score {
		t.Fatal("churner must rank below")
	}
}

func TestRankLimitAndEventCap(t *testing.T) {
	var events []Event
	for i := 0; i < 25; i++ { // 25 events on one resource
		e := ev(float64(i), []string{classify.ClassScale}, classify.ActorServiceAccount, "sa")
		e.EventID = fmt.Sprintf("e%d", i)
		events = append(events, e)
	}
	for i := 0; i < 3; i++ { // 3 more single-event resources
		e := ev(float64(i), []string{classify.ClassScale}, classify.ActorServiceAccount, "sa")
		e.EventID = fmt.Sprintf("x%d", i)
		e.Name = fmt.Sprintf("other-%d", i)
		events = append(events, e)
	}

	suspects := Rank(events, nil, at, 2)
	if len(suspects) != 2 {
		t.Fatalf("want limit-truncated 2 suspects, got %d", len(suspects))
	}
	all := Rank(events, nil, at, 50)
	for _, s := range all {
		if s.Name == "checkout" {
			if s.EventCount != 25 {
				t.Fatalf("event_count must be the true count, got %d", s.EventCount)
			}
			if len(s.Events) != maxEventsPerSuspect {
				t.Fatalf("events must cap at %d, got %d", maxEventsPerSuspect, len(s.Events))
			}
		}
	}
}
```

Add `"fmt"` to the test file's imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `~/sdk/go1.24.10/bin/go test ./internal/rank/`
Expected: FAIL with "undefined: Rank"

- [ ] **Step 3: Write the implementation**

Append to `internal/rank/rank.go` (add `"sort"` to imports):

```go
// EventSummary is the per-event payload inside a Suspect.
type EventSummary struct {
	EventID   string    `json:"event_id"`
	EventTime time.Time `json:"event_time"`
	Operation string    `json:"operation"`
	Classes   []string  `json:"classes"`
	ActorType string    `json:"actor_type"`
	UserName  string    `json:"user_name"`
}

// Suspect is one ranked resource group.
type Suspect struct {
	Namespace       string         `json:"namespace"`
	Kind            string         `json:"kind"`
	Name            string         `json:"name"`
	Score           int            `json:"score"`
	Reasons         []string       `json:"reasons"`
	EventCount      int            `json:"event_count"`
	LatestEventTime time.Time      `json:"latest_event_time"`
	Events          []EventSummary `json:"events"`
}

// Rank scores every event, groups by resource, and returns at most limit
// suspects ordered by score (desc), latest event time (desc), then name.
// events must be ordered newest-first.
func Rank(events []Event, stats map[Key]Stats, at time.Time, limit int) []Suspect {
	type group struct {
		best    float64
		reasons []string
		count   int
		latest  time.Time
		events  []EventSummary
	}
	groups := map[Key]*group{}
	var order []Key // stable iteration for determinism
	for _, e := range events {
		k := Key{Namespace: e.Namespace, Kind: e.Kind, Name: e.Name}
		st, ok := stats[k]
		score, reasons := scoreEvent(e, st, ok, at)
		g := groups[k]
		if g == nil {
			g = &group{}
			groups[k] = g
			order = append(order, k)
		}
		if score > g.best || g.count == 0 {
			g.best, g.reasons = score, reasons
		}
		if e.EventTime.After(g.latest) {
			g.latest = e.EventTime
		}
		g.count++
		if len(g.events) < maxEventsPerSuspect {
			g.events = append(g.events, EventSummary{
				EventID: e.EventID, EventTime: e.EventTime, Operation: e.Operation,
				Classes: e.Classes, ActorType: e.ActorType, UserName: e.UserName,
			})
		}
	}

	suspects := make([]Suspect, 0, len(groups))
	for _, k := range order {
		g := groups[k]
		reasons := g.reasons
		if reasons == nil {
			reasons = []string{}
		}
		suspects = append(suspects, Suspect{
			Namespace: k.Namespace, Kind: k.Kind, Name: k.Name,
			Score:   int(math.Round(g.best)),
			Reasons: reasons, EventCount: g.count,
			LatestEventTime: g.latest, Events: g.events,
		})
	}
	sort.SliceStable(suspects, func(i, j int) bool {
		if suspects[i].Score != suspects[j].Score {
			return suspects[i].Score > suspects[j].Score
		}
		if !suspects[i].LatestEventTime.Equal(suspects[j].LatestEventTime) {
			return suspects[i].LatestEventTime.After(suspects[j].LatestEventTime)
		}
		return suspects[i].Name < suspects[j].Name
	})
	if limit > 0 && len(suspects) > limit {
		suspects = suspects[:limit]
	}
	return suspects
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `~/sdk/go1.24.10/bin/go test ./internal/rank/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rank/
git commit -m "feat(rank): group events per resource and rank suspects"
```

---

### Task 9: `GET /api/incident` endpoint

**Files:**
- Create: `internal/dashboardapi/incident.go`
- Create: `internal/dashboardapi/incident_test.go`
- Modify: `internal/dashboardapi/api.go` (ReadStore interface + route)
- Modify: `internal/dashboardapi/api_test.go` (extend `fakeStore`)

**Interfaces:**
- Consumes: `storage.IncidentRow`, `storage.ResourceKey`, `storage.ResourceStats`, `Store.IncidentEvents`, `Store.ResourceStats` (Task 6); `rank.Rank`, `rank.Event`, `rank.Key`, `rank.Stats`, `rank.Suspect` (Tasks 7–8).
- Produces: `GET /api/incident?cluster=&at=&lookback=&limit=` returning `{"incident":{"cluster","at","lookback"},"suspects":[rank.Suspect...]}`. Params: `cluster` required (400 if missing); `at` RFC3339 default now; `lookback` Go duration, default `1h`, `> 24h` → 400; `limit` default 50, capped at 100. Window queried: `[at−lookback, at+10m]` (10-minute clock-skew allowance).

- [ ] **Step 1: Extend the ReadStore interface and register the route**

In `internal/dashboardapi/api.go`, add to the `ReadStore` interface (after `Facets`):

```go
	IncidentEvents(ctx context.Context, cluster string, from, to time.Time) ([]storage.IncidentRow, error)
	ResourceStats(ctx context.Context, cluster string, windowStart time.Time) (map[storage.ResourceKey]storage.ResourceStats, error)
```

In `NewRouter`, after the `/api/facets` line add:

```go
	mux.HandleFunc("GET /api/incident", h.incident)
```

In `internal/dashboardapi/api_test.go`, add to `fakeStore` (satisfies the widened interface; the existing tests don't exercise these):

```go
func (f *fakeStore) IncidentEvents(_ context.Context, _ string, _, _ time.Time) ([]storage.IncidentRow, error) {
	return nil, nil
}

func (f *fakeStore) ResourceStats(_ context.Context, _ string, _ time.Time) (map[storage.ResourceKey]storage.ResourceStats, error) {
	return nil, nil
}
```

(Add `"time"` to api_test.go imports if not present.)

- [ ] **Step 2: Write the failing tests**

Create `internal/dashboardapi/incident_test.go`:

```go
package dashboardapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gauravrautela/kubewatch/internal/storage"
)

// incidentFakeStore records the incident query and serves canned rows.
type incidentFakeStore struct {
	fakeStore
	events    []storage.IncidentRow
	stats     map[storage.ResourceKey]storage.ResourceStats
	gotFrom   time.Time
	gotTo     time.Time
	gotWindow time.Time
	eventsErr error
}

func (f *incidentFakeStore) IncidentEvents(_ context.Context, cluster string, from, to time.Time) ([]storage.IncidentRow, error) {
	f.gotFrom, f.gotTo = from, to
	return f.events, f.eventsErr
}

func (f *incidentFakeStore) ResourceStats(_ context.Context, cluster string, windowStart time.Time) (map[storage.ResourceKey]storage.ResourceStats, error) {
	f.gotWindow = windowStart
	return f.stats, nil
}

func getIncident(t *testing.T, store ReadStore, target string) *httptest.ResponseRecorder {
	t.Helper()
	h := NewRouter(store, "")
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestIncidentRequiresCluster(t *testing.T) {
	rec := getIncident(t, &incidentFakeStore{}, "/api/incident")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestIncidentRejectsBadParams(t *testing.T) {
	for _, target := range []string{
		"/api/incident?cluster=c1&at=yesterday",
		"/api/incident?cluster=c1&lookback=nope",
		"/api/incident?cluster=c1&lookback=25h",
		"/api/incident?cluster=c1&lookback=-1h",
		"/api/incident?cluster=c1&limit=0",
		"/api/incident?cluster=c1&limit=abc",
	} {
		if rec := getIncident(t, &incidentFakeStore{}, target); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", target, rec.Code)
		}
	}
}

func TestIncidentWindowAndResponse(t *testing.T) {
	at := time.Date(2026, 7, 8, 14, 0, 0, 0, time.UTC)
	fake := &incidentFakeStore{
		events: []storage.IncidentRow{{
			EventID: "11111111-1111-1111-1111-111111111111", EventTime: at.Add(-2 * time.Minute),
			Operation: "UPDATE", Kind: "Deployment", Namespace: "payments", Name: "checkout",
			UserName: "alice", ActorType: "human", Classes: []string{"image"},
		}},
		stats: map[storage.ResourceKey]storage.ResourceStats{},
	}
	rec := getIncident(t, fake, "/api/incident?cluster=c1&at=2026-07-08T14:00:00Z&lookback=1h")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if want := at.Add(-time.Hour); !fake.gotFrom.Equal(want) {
		t.Fatalf("from = %v, want %v", fake.gotFrom, want)
	}
	if want := at.Add(10 * time.Minute); !fake.gotTo.Equal(want) {
		t.Fatalf("to = %v, want %v (skew allowance)", fake.gotTo, want)
	}
	if want := at.Add(-time.Hour); !fake.gotWindow.Equal(want) {
		t.Fatalf("stats windowStart = %v, want %v", fake.gotWindow, want)
	}

	var resp struct {
		Incident struct {
			Cluster  string `json:"cluster"`
			Lookback string `json:"lookback"`
		} `json:"incident"`
		Suspects []struct {
			Name    string   `json:"name"`
			Score   int      `json:"score"`
			Reasons []string `json:"reasons"`
			Events  []struct {
				EventID string `json:"event_id"`
			} `json:"events"`
		} `json:"suspects"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Incident.Cluster != "c1" || resp.Incident.Lookback != "1h0m0s" {
		t.Fatalf("incident meta = %+v", resp.Incident)
	}
	if len(resp.Suspects) != 1 || resp.Suspects[0].Name != "checkout" {
		t.Fatalf("suspects = %+v", resp.Suspects)
	}
	if resp.Suspects[0].Score != 100 { // fresh human image change caps at 100
		t.Fatalf("score = %d, want 100", resp.Suspects[0].Score)
	}
	if len(resp.Suspects[0].Events) != 1 || resp.Suspects[0].Events[0].EventID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("events = %+v", resp.Suspects[0].Events)
	}
}

func TestIncidentQueryFailure(t *testing.T) {
	fake := &incidentFakeStore{eventsErr: context.DeadlineExceeded}
	rec := getIncident(t, fake, "/api/incident?cluster=c1")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", rec.Code)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `~/sdk/go1.24.10/bin/go test ./internal/dashboardapi/`
Expected: FAIL with "h.incident undefined" (build error)

- [ ] **Step 4: Write the handler**

Create `internal/dashboardapi/incident.go`:

```go
package dashboardapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gauravrautela/kubewatch/internal/rank"
	"github.com/gauravrautela/kubewatch/internal/storage"
)

const (
	defaultLookback = time.Hour
	maxLookback     = 24 * time.Hour
	skewAllowance   = 10 * time.Minute // events slightly after "at" still shown, heavily discounted
	defaultSuspects = 50
	maxSuspects     = 100
)

type incidentMeta struct {
	Cluster  string    `json:"cluster"`
	At       time.Time `json:"at"`
	Lookback string    `json:"lookback"`
}

type incidentResponse struct {
	Incident incidentMeta   `json:"incident"`
	Suspects []rank.Suspect `json:"suspects"`
}

func (h *Handler) incident(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cluster := q.Get("cluster")
	if cluster == "" {
		writeErr(w, http.StatusBadRequest, "cluster is required")
		return
	}
	at := time.Now().UTC()
	if v := q.Get("at"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid at: must be RFC3339")
			return
		}
		at = t.UTC()
	}
	lookback := defaultLookback
	if v := q.Get("lookback"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			writeErr(w, http.StatusBadRequest, "invalid lookback")
			return
		}
		if d > maxLookback {
			writeErr(w, http.StatusBadRequest, "lookback exceeds 24h")
			return
		}
		lookback = d
	}
	limit := defaultSuspects
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeErr(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = min(n, maxSuspects)
	}

	from := at.Add(-lookback)
	events, err := h.store.IncidentEvents(r.Context(), cluster, from, at.Add(skewAllowance))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	stats, err := h.store.ResourceStats(r.Context(), cluster, from)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}

	suspects := rank.Rank(toRankEvents(events), toRankStats(stats), at, limit)
	writeJSON(w, http.StatusOK, incidentResponse{
		Incident: incidentMeta{Cluster: cluster, At: at, Lookback: lookback.String()},
		Suspects: suspects,
	})
}

func toRankEvents(rows []storage.IncidentRow) []rank.Event {
	out := make([]rank.Event, len(rows))
	for i, r := range rows {
		out[i] = rank.Event{
			EventID: r.EventID, EventTime: r.EventTime, Operation: r.Operation,
			Kind: r.Kind, Namespace: r.Namespace, Name: r.Name,
			UserName: r.UserName, ActorType: r.ActorType, Classes: r.Classes,
		}
	}
	return out
}

func toRankStats(stats map[storage.ResourceKey]storage.ResourceStats) map[rank.Key]rank.Stats {
	out := make(map[rank.Key]rank.Stats, len(stats))
	for k, v := range stats {
		out[rank.Key{Namespace: k.Namespace, Kind: k.Kind, Name: k.Name}] = rank.Stats{
			PerDay: v.PerDay, LastPriorDay: v.LastPriorDay, PriorTotal: v.PriorTotal,
		}
	}
	return out
}
```

- [ ] **Step 5: Run all Go tests**

Run: `~/sdk/go1.24.10/bin/go test ./...`
Expected: PASS (storage integration tests SKIP without DSN)

- [ ] **Step 6: Commit**

```bash
git add internal/dashboardapi/
git commit -m "feat(dashboardapi): GET /api/incident ranked suspects endpoint"
```

---

### Task 10: Web — types and API client

**Files:**
- Modify: `web/src/types.ts`
- Modify: `web/src/api/client.ts`
- Test: `web/src/api/client.test.ts` (append)

**Interfaces:**
- Consumes: JSON contract from Task 9.
- Produces (used by Task 11): types `SuspectEvent`, `Suspect`, `IncidentMeta`, `IncidentResponse`; `fetchIncident(q: IncidentQuery): Promise<IncidentResponse>` where `IncidentQuery = { cluster: string; at?: string; lookback?: string; limit?: number }`.

- [ ] **Step 1: Write the failing tests**

Append to `web/src/api/client.test.ts` (import `fetchIncident` from `./client`):

```ts
test('fetchIncident builds URL with cluster, at, lookback', async () => {
  const f = mockFetch({ incident: { cluster: 'c1', at: '', lookback: '1h' }, suspects: [] })
  await fetchIncident({ cluster: 'c1', at: '2026-07-08T14:00:00Z', lookback: '1h' })
  const url = f.mock.calls[0][0] as string
  expect(url).toContain('/api/incident?')
  expect(url).toContain('cluster=c1')
  expect(url).toContain('at=2026-07-08T14%3A00%3A00Z')
  expect(url).toContain('lookback=1h')
})

test('fetchIncident omits optional params', async () => {
  const f = mockFetch({ incident: {}, suspects: [] })
  await fetchIncident({ cluster: 'c1' })
  const url = f.mock.calls[0][0] as string
  expect(url).not.toContain('at=')
  expect(url).not.toContain('lookback=')
  expect(url).not.toContain('limit=')
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && npm test -- client`
Expected: FAIL — `fetchIncident` is not exported

- [ ] **Step 3: Add types**

Append to `web/src/types.ts`:

```ts
export interface IncidentMeta {
  cluster: string
  at: string
  lookback: string
}

export interface SuspectEvent {
  event_id: string
  event_time: string
  operation: Operation
  classes: string[]
  actor_type: string
  user_name: string
}

export interface Suspect {
  namespace: string
  kind: string
  name: string
  score: number
  reasons: string[]
  event_count: number
  latest_event_time: string
  events: SuspectEvent[]
}

export interface IncidentResponse {
  incident: IncidentMeta
  suspects: Suspect[]
}
```

- [ ] **Step 4: Add the client function**

In `web/src/api/client.ts`, add `IncidentResponse` to the type import from `'../types'`, and append:

```ts
export interface IncidentQuery {
  cluster: string
  at?: string
  lookback?: string
  limit?: number
}

export function fetchIncident(q: IncidentQuery): Promise<IncidentResponse> {
  const p = new URLSearchParams({ cluster: q.cluster })
  if (q.at) p.set('at', q.at)
  if (q.lookback) p.set('lookback', q.lookback)
  if (q.limit) p.set('limit', String(q.limit))
  return getJSON<IncidentResponse>(`/api/incident?${p.toString()}`)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd web && npm test -- client`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add web/src/types.ts web/src/api/client.ts web/src/api/client.test.ts
git commit -m "feat(web): incident types and fetchIncident client"
```

---

### Task 11: Web — SuspectList and IncidentPage components

**Files:**
- Create: `web/src/components/SuspectList.tsx`
- Create: `web/src/components/SuspectList.test.tsx`
- Create: `web/src/components/IncidentPage.tsx`
- Create: `web/src/components/IncidentPage.test.tsx`

**Interfaces:**
- Consumes: `fetchIncident`, `IncidentQuery` (Task 10); `useFacets()` from `web/src/api/hooks.ts`; `Suspect` type.
- Produces: `SuspectList({ suspects: Suspect[] })` — ranked table with expandable rows linking to `/events/:id`; `IncidentPage()` — full page with cluster/time/lookback controls (used by Task 12 routing).

- [ ] **Step 1: Write the failing SuspectList test**

Create `web/src/components/SuspectList.test.tsx`:

```tsx
import { test, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { SuspectList } from './SuspectList'
import type { Suspect } from '../types'

const suspect: Suspect = {
  namespace: 'payments', kind: 'Deployment', name: 'checkout',
  score: 87, reasons: ['image change', 'human: alice', 'first change in 12d'],
  event_count: 2, latest_event_time: '2026-07-08T13:58:00Z',
  events: [
    { event_id: 'ev-1', event_time: '2026-07-08T13:58:00Z', operation: 'UPDATE',
      classes: ['image'], actor_type: 'human', user_name: 'alice' },
    { event_id: 'ev-2', event_time: '2026-07-08T13:40:00Z', operation: 'UPDATE',
      classes: ['scale'], actor_type: 'serviceaccount', user_name: 'system:serviceaccount:ci:d' },
  ],
}

function renderList() {
  render(
    <MemoryRouter>
      <SuspectList suspects={[suspect]} />
    </MemoryRouter>,
  )
}

test('renders score, resource identity, and reason chips', () => {
  renderList()
  expect(screen.getByText('87')).toBeInTheDocument()
  expect(screen.getByText('checkout')).toBeInTheDocument()
  expect(screen.getByText('payments')).toBeInTheDocument()
  expect(screen.getByText('Deployment')).toBeInTheDocument()
  expect(screen.getByText('image change')).toBeInTheDocument()
  expect(screen.getByText('first change in 12d')).toBeInTheDocument()
})

test('expanding a row reveals event links to the detail page', async () => {
  renderList()
  expect(screen.queryByRole('link')).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: /2 changes/i }))
  const links = screen.getAllByRole('link')
  expect(links[0]).toHaveAttribute('href', '/events/ev-1')
  expect(links[1]).toHaveAttribute('href', '/events/ev-2')
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npm test -- SuspectList`
Expected: FAIL — cannot resolve `./SuspectList`

- [ ] **Step 3: Implement SuspectList**

Create `web/src/components/SuspectList.tsx`:

```tsx
import { Fragment, useState } from 'react'
import { Link } from 'react-router-dom'
import type { Suspect } from '../types'

function scoreClasses(score: number): string {
  if (score >= 70) return 'border-red-500/30 bg-red-500/15 text-red-400'
  if (score >= 40) return 'border-amber-500/30 bg-amber-500/15 text-amber-400'
  return 'border-zinc-500/30 bg-zinc-500/15 text-zinc-400'
}

export function SuspectList({ suspects }: { suspects: Suspect[] }) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const keyOf = (s: Suspect) => `${s.namespace}/${s.kind}/${s.name}`
  const toggle = (k: string) =>
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(k)) next.delete(k)
      else next.add(k)
      return next
    })

  return (
    <table className="mt-6 w-full text-left text-sm">
      <thead>
        <tr className="border-b border-zinc-800 text-xs uppercase tracking-wide text-zinc-500">
          <th className="py-2 pr-3">Score</th>
          <th className="py-2 pr-3">Resource</th>
          <th className="py-2 pr-3">Why</th>
          <th className="py-2 pr-3">Changes</th>
          <th className="py-2">Latest</th>
        </tr>
      </thead>
      <tbody>
        {suspects.map((s) => {
          const k = keyOf(s)
          const open = expanded.has(k)
          return (
            <Fragment key={k}>
              <tr className="border-b border-zinc-900">
                <td className="py-2 pr-3">
                  <span className={`inline-block rounded border px-2 py-0.5 text-xs font-semibold ${scoreClasses(s.score)}`}>
                    {s.score}
                  </span>
                </td>
                <td className="py-2 pr-3">
                  <span className="text-zinc-500">{s.namespace}</span>{' '}
                  <span className="text-zinc-400">{s.kind}</span>{' '}
                  <span className="font-medium text-zinc-100">{s.name}</span>
                </td>
                <td className="py-2 pr-3">
                  <span className="flex flex-wrap gap-1">
                    {s.reasons.map((r) => (
                      <span key={r} className="rounded-full border border-zinc-700 px-2 py-0.5 text-xs text-zinc-300">
                        {r}
                      </span>
                    ))}
                  </span>
                </td>
                <td className="py-2 pr-3">
                  <button
                    onClick={() => toggle(k)}
                    className="text-xs text-zinc-400 underline decoration-dotted hover:text-zinc-200"
                  >
                    {s.event_count} change{s.event_count === 1 ? '' : 's'} {open ? '▾' : '▸'}
                  </button>
                </td>
                <td className="py-2 text-xs text-zinc-400">
                  {new Date(s.latest_event_time).toLocaleTimeString()}
                </td>
              </tr>
              {open ? (
                <tr className="border-b border-zinc-900 bg-zinc-900/40">
                  <td colSpan={5} className="px-4 py-2">
                    <ul className="space-y-1 text-xs">
                      {s.events.map((e) => (
                        <li key={e.event_id}>
                          <Link to={`/events/${e.event_id}`} className="text-emerald-400 hover:underline">
                            {new Date(e.event_time).toLocaleTimeString()} {e.operation}
                          </Link>{' '}
                          <span className="text-zinc-400">{e.classes.join(', ')}</span>{' '}
                          <span className="text-zinc-500">by {e.user_name}</span>
                        </li>
                      ))}
                    </ul>
                  </td>
                </tr>
              ) : null}
            </Fragment>
          )
        })}
      </tbody>
    </table>
  )
}
```

- [ ] **Step 4: Run SuspectList tests**

Run: `cd web && npm test -- SuspectList`
Expected: PASS

- [ ] **Step 5: Write the failing IncidentPage test**

Create `web/src/components/IncidentPage.test.tsx`:

```tsx
import { afterEach, test, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { IncidentPage } from './IncidentPage'

afterEach(() => vi.unstubAllGlobals())

const suspect = {
  namespace: 'payments', kind: 'Deployment', name: 'checkout',
  score: 87, reasons: ['image change'], event_count: 1,
  latest_event_time: '2026-07-08T13:58:00Z',
  events: [{ event_id: 'ev-1', event_time: '2026-07-08T13:58:00Z', operation: 'UPDATE',
    classes: ['image'], actor_type: 'human', user_name: 'alice' }],
}

function stubApi(suspects: unknown[]) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string) => {
      let body: unknown = {}
      if (url.includes('/api/facets')) {
        body = { clusters: ['c1', 'c2'], namespaces: [], kinds: [], operations: [] }
      } else if (url.includes('/api/incident')) {
        body = { incident: { cluster: 'c1', at: '2026-07-08T14:00:00Z', lookback: '1h0m0s' }, suspects }
      }
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) })
    }),
  )
}

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <MemoryRouter>
      <QueryClientProvider client={qc}>
        <IncidentPage />
      </QueryClientProvider>
    </MemoryRouter>,
  )
}

test('analyze fetches and renders ranked suspects', async () => {
  stubApi([suspect])
  renderPage()
  await userEvent.selectOptions(await screen.findByLabelText(/cluster/i), 'c1')
  await userEvent.click(screen.getByRole('button', { name: /analyze/i }))
  expect(await screen.findByText('checkout')).toBeInTheDocument()
  expect(screen.getByText('87')).toBeInTheDocument()
  expect(screen.getByText('image change')).toBeInTheDocument()
})

test('empty result shows widen-lookback hint', async () => {
  stubApi([])
  renderPage()
  await userEvent.selectOptions(await screen.findByLabelText(/cluster/i), 'c1')
  await userEvent.click(screen.getByRole('button', { name: /analyze/i }))
  expect(await screen.findByText(/no changes found/i)).toBeInTheDocument()
})

test('analyze disabled without a cluster', async () => {
  stubApi([])
  renderPage()
  await screen.findByLabelText(/cluster/i)
  expect(screen.getByRole('button', { name: /analyze/i })).toBeDisabled()
})
```

- [ ] **Step 6: Run test to verify it fails**

Run: `cd web && npm test -- IncidentPage`
Expected: FAIL — cannot resolve `./IncidentPage`

- [ ] **Step 7: Implement IncidentPage**

Create `web/src/components/IncidentPage.tsx`:

```tsx
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchIncident, type IncidentQuery } from '../api/client'
import { useFacets } from '../api/hooks'
import { SuspectList } from './SuspectList'

const LOOKBACKS = ['15m', '1h', '6h', '24h']

const inputClasses =
  'rounded border border-zinc-700 bg-zinc-900 px-2 py-1.5 text-sm text-zinc-100'

export function IncidentPage() {
  const { data: facets } = useFacets()
  const [cluster, setCluster] = useState('')
  const [at, setAt] = useState('') // datetime-local; '' means "now"
  const [lookback, setLookback] = useState('1h')
  const [query, setQuery] = useState<IncidentQuery | null>(null)

  const { data, isFetching, error } = useQuery({
    queryKey: ['incident', query],
    queryFn: () => fetchIncident(query!),
    enabled: query !== null,
  })

  const analyze = () => {
    if (!cluster) return
    setQuery({
      cluster,
      lookback,
      ...(at ? { at: new Date(at).toISOString() } : {}),
    })
  }

  return (
    <main className="mx-auto max-w-6xl px-4 py-6">
      <h1 className="text-lg font-semibold text-zinc-100">Incident Lens</h1>
      <p className="mt-1 text-sm text-zinc-400">
        Ranked, noise-filtered view of what changed in a cluster around an incident.
      </p>
      <div className="mt-4 flex flex-wrap items-end gap-3">
        <label className="flex flex-col gap-1 text-xs text-zinc-400">
          Cluster
          <select value={cluster} onChange={(e) => setCluster(e.target.value)} className={inputClasses}>
            <option value="">select cluster…</option>
            {(facets?.clusters ?? []).map((c) => (
              <option key={c} value={c}>
                {c}
              </option>
            ))}
          </select>
        </label>
        <label className="flex flex-col gap-1 text-xs text-zinc-400">
          Incident time (blank = now)
          <input type="datetime-local" value={at} onChange={(e) => setAt(e.target.value)} className={inputClasses} />
        </label>
        <label className="flex flex-col gap-1 text-xs text-zinc-400">
          Lookback
          <select value={lookback} onChange={(e) => setLookback(e.target.value)} className={inputClasses}>
            {LOOKBACKS.map((l) => (
              <option key={l} value={l}>
                {l}
              </option>
            ))}
          </select>
        </label>
        <button
          onClick={analyze}
          disabled={!cluster || isFetching}
          className="rounded bg-emerald-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-emerald-500 disabled:opacity-50"
        >
          {isFetching ? 'Analyzing…' : 'Analyze'}
        </button>
      </div>
      {error ? <p className="mt-6 text-sm text-red-400">{(error as Error).message}</p> : null}
      {data && data.suspects.length === 0 ? (
        <p className="mt-6 text-sm text-zinc-400">
          No changes found in this window. Try widening the lookback.
        </p>
      ) : null}
      {data && data.suspects.length > 0 ? <SuspectList suspects={data.suspects} /> : null}
    </main>
  )
}
```

- [ ] **Step 8: Run all web tests**

Run: `cd web && npm test`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add web/src/components/SuspectList.tsx web/src/components/SuspectList.test.tsx web/src/components/IncidentPage.tsx web/src/components/IncidentPage.test.tsx
git commit -m "feat(web): incident lens page with ranked suspect list"
```

---

### Task 12: Routing, nav link, and full verification

**Files:**
- Modify: `web/src/App.tsx`
- Modify: `web/src/components/Header.tsx`
- Test: `web/src/App.routing.test.tsx` (append)

**Interfaces:**
- Consumes: `IncidentPage` (Task 11).
- Produces: `/incident` route and a persistent header link to it.

- [ ] **Step 1: Write the failing routing test**

Append to `web/src/App.routing.test.tsx` a test that follows the file's existing `stubApi()` + render pattern (reuse its helpers; extend `stubApi`'s URL dispatch with an `/api/incident` branch returning `{ incident: { cluster: 'c1', at: '', lookback: '1h0m0s' }, suspects: [] }`):

```tsx
test('header link navigates to the incident lens page', async () => {
  stubApi()
  renderApp('/') // use the file's existing render helper; if it renders <App /> inline, mirror that setup with initialEntries ['/']
  await userEvent.click(await screen.findByRole('link', { name: /incident/i }))
  expect(await screen.findByText(/incident lens/i)).toBeInTheDocument()
  expect(screen.getByRole('button', { name: /analyze/i })).toBeInTheDocument()
})
```

If the file has no shared render helper, render exactly as its other tests do (MemoryRouter + QueryClientProvider around `<App />`) with `initialEntries={['/']}`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npm test -- App.routing`
Expected: FAIL — no link named "Incident"

- [ ] **Step 3: Add the route and nav link**

In `web/src/App.tsx`, import `IncidentPage` and add the route above the catch-all:

```tsx
import { IncidentPage } from './components/IncidentPage'
// inside <Routes>, before the "*" route:
<Route path="/incident" element={<IncidentPage />} />
```

In `web/src/components/Header.tsx`, add after the live badge `<span>`:

```tsx
        <nav className="ml-auto">
          <Link to="/incident" className="text-sm text-zinc-400 hover:text-zinc-100">
            Incident
          </Link>
        </nav>
```

- [ ] **Step 4: Run the routing tests**

Run: `cd web && npm test -- App.routing`
Expected: PASS

- [ ] **Step 5: Full verification**

Run all of:

```bash
~/sdk/go1.24.10/bin/go vet ./...
~/sdk/go1.24.10/bin/go test ./...
cd web && npm test && npm run build
```

Expected: vet clean; all Go tests PASS (storage integration SKIPs without `CLICKHOUSE_DSN`); all vitest suites PASS; `npm run build` (tsc + vite) succeeds.

- [ ] **Step 6: Commit**

```bash
git add web/src/App.tsx web/src/components/Header.tsx web/src/App.routing.test.tsx
git commit -m "feat(web): /incident route and header navigation"
```

---

## Post-implementation notes (not tasks)

- Deploying: the hub's `Migrate` applies the schema changes on startup; the MODIFY TTL rewrite is cheap because current retention is 1 day. Rarity/churn chips sharpen as `resource_change_stats` accumulates (~2 weeks for full fidelity).
- End-to-end smoke verification against a real ClickHouse (set `CLICKHOUSE_DSN` and re-run `~/sdk/go1.24.10/bin/go test ./internal/storage/`) is worth doing before merging; per user preference, ask before running anything against the cluster.
