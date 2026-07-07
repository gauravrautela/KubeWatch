# Table Include/Exclude Filter Buttons Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Hovering a value in the dashboard event table shows ⊕/⊖ buttons that include/exclude that value as a filter in one click.

**Architecture:** Backend gains four new CSV exclude params (`exclude_users`, `exclude_clusters`, `exclude_names`, `exclude_operations`) following the existing `exclude_kinds`/`exclude_namespaces` pattern — new `storage.Filter` fields become `NOT IN` conditions in the shared `filterConds` builder, so list, activity, and live-feed queries all honor them. Frontend gains a small `CellFilter` wrapper component that renders hover ⊕/⊖ buttons and applies filters through the existing URL-param `useFilters` model; `EventList` wraps its six cell values in it and `FilterChips` renders chips for the new excludes.

**Tech Stack:** Go 1.24 (`net/http`, ClickHouse query builder), React 18 + TypeScript, react-router URL params, Tailwind v4, Vitest + Testing Library, Go stdlib testing.

**Spec:** `docs/superpowers/specs/2026-07-07-table-include-exclude-design.md`

## Global Constraints

- Go binary is NOT on PATH: use `~/sdk/go1.24.10/bin/go` for all Go commands.
- Frontend tests run from `web/`: `npm test -- <file>` (vitest run).
- Include params stay single-value (`kind=Deployment`); exclude params are CSV multi-value (`exclude_kinds=a,b`) — match existing semantics exactly.
- Mutual exclusion per field: applying an include removes that value from the field's exclude list; applying an exclude clears a matching include. `kind=Deployment&exclude_kinds=Deployment` must never be produced by the UI.
- Field → param mapping (used across every task):

| Field     | Include param | Exclude param        | Go Filter field     | DB column   |
|-----------|---------------|----------------------|---------------------|-------------|
| kind      | `kind`        | `exclude_kinds`      | `ExcludeKinds`      | `kind`      |
| namespace | `namespace`   | `exclude_namespaces` | `ExcludeNamespaces` | `namespace` |
| name      | `name`        | `exclude_names`      | `ExcludeNames`      | `name`      |
| user      | `user`        | `exclude_users`      | `ExcludeUsers`      | `user_name` |
| cluster   | `cluster`     | `exclude_clusters`   | `ExcludeClusters`   | `cluster`   |
| operation | `operation`   | `exclude_operations` | `ExcludeOperations` | `operation` |

---

### Task 1: Storage — new exclude fields on Filter and query conditions

**Files:**
- Modify: `internal/storage/query.go` (Filter struct ~line 25, `filterConds` ~line 192)
- Test: `internal/storage/query_test.go`

**Interfaces:**
- Produces: `storage.Filter` gains fields `ExcludeUsers, ExcludeClusters, ExcludeNames, ExcludeOperations []string`. Task 2's API parser sets them; `filterConds` (used by list + activity queries) turns each into a `NOT IN` condition.

- [ ] **Step 1: Write the failing test**

Append to `internal/storage/query_test.go`:

```go
func TestBuildListQueryNewExcludes(t *testing.T) {
	f := Filter{
		ExcludeUsers:      []string{"system:serviceaccount:kube-system:generic-garbage-collector"},
		ExcludeClusters:   []string{"staging"},
		ExcludeNames:      []string{"web"},
		ExcludeOperations: []string{"UPDATE"},
	}
	q, args := buildListQuery(f, nil, nil, 50)

	for _, want := range []string{
		"user_name NOT IN (?)",
		"cluster NOT IN (?)",
		"name NOT IN (?)",
		"operation NOT IN (?)",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("query missing %q\n%s", want, q)
		}
	}
	// args: 4 exclude slices + limit
	if len(args) != 5 {
		t.Fatalf("want 5 args, got %d: %v", len(args), args)
	}
}

func TestBuildActivityQueryAppliesNewExcludes(t *testing.T) {
	q, args, err := buildActivityQuery(Filter{ExcludeUsers: []string{"bot"}}, "hour")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q, "user_name NOT IN (?)") {
		t.Errorf("activity query missing user exclude:\n%s", q)
	}
	if len(args) != 1 {
		t.Fatalf("want 1 arg, got %v", args)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `~/sdk/go1.24.10/bin/go test ./internal/storage/ -run 'NewExcludes' -v`
Expected: FAIL — compile error `unknown field ExcludeUsers in struct literal`.

- [ ] **Step 3: Implement**

In `internal/storage/query.go`, extend the `Filter` struct (after `ExcludeNamespaces`):

```go
	ExcludeKinds      []string // exact kinds to exclude (ignore filter)
	ExcludeNamespaces []string // exact namespaces to exclude (ignore filter)
	ExcludeUsers      []string // exact user_names to exclude (ignore filter)
	ExcludeClusters   []string // exact clusters to exclude (ignore filter)
	ExcludeNames      []string // exact names to exclude (ignore filter)
	ExcludeOperations []string // exact operations to exclude (ignore filter)
```

In `filterConds`, after the existing `ExcludeNamespaces` block:

```go
	if len(f.ExcludeUsers) > 0 {
		b.add("user_name NOT IN (?)", f.ExcludeUsers)
	}
	if len(f.ExcludeClusters) > 0 {
		b.add("cluster NOT IN (?)", f.ExcludeClusters)
	}
	if len(f.ExcludeNames) > 0 {
		b.add("name NOT IN (?)", f.ExcludeNames)
	}
	if len(f.ExcludeOperations) > 0 {
		b.add("operation NOT IN (?)", f.ExcludeOperations)
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `~/sdk/go1.24.10/bin/go test ./internal/storage/ -v`
Expected: all PASS (new tests plus existing suite).

- [ ] **Step 5: Commit**

```bash
git add internal/storage/query.go internal/storage/query_test.go
git commit -m "feat(storage): exclude filters for user, cluster, name, operation"
```

---

### Task 2: Dashboard API — parse the four new exclude params

**Files:**
- Modify: `internal/dashboardapi/api.go` (`parseFilter`, ~line 154)
- Test: `internal/dashboardapi/api_test.go`

**Interfaces:**
- Consumes: `storage.Filter` fields `ExcludeUsers, ExcludeClusters, ExcludeNames, ExcludeOperations []string` from Task 1.
- Produces: GET `/api/events` and `/api/activity` accept `exclude_users`, `exclude_clusters`, `exclude_names`, `exclude_operations` as CSV query params (trimmed, empties dropped — same as `exclude_kinds`). This is the contract the frontend (Tasks 3–6) targets.

- [ ] **Step 1: Write the failing test**

Append to `internal/dashboardapi/api_test.go`:

```go
func TestListEventsParsesNewExcludes(t *testing.T) {
	fs := &fakeStore{}
	h := NewRouter(fs, "")
	rec := do(t, h, "/api/events?exclude_users=bot,%20alice,&exclude_clusters=staging&exclude_names=web&exclude_operations=UPDATE")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	f := fs.lastP.Filter
	if len(f.ExcludeUsers) != 2 || f.ExcludeUsers[0] != "bot" || f.ExcludeUsers[1] != "alice" {
		t.Errorf("exclude_users not parsed/trimmed: %+v", f.ExcludeUsers)
	}
	if len(f.ExcludeClusters) != 1 || f.ExcludeClusters[0] != "staging" {
		t.Errorf("exclude_clusters not parsed: %+v", f.ExcludeClusters)
	}
	if len(f.ExcludeNames) != 1 || f.ExcludeNames[0] != "web" {
		t.Errorf("exclude_names not parsed: %+v", f.ExcludeNames)
	}
	if len(f.ExcludeOperations) != 1 || f.ExcludeOperations[0] != "UPDATE" {
		t.Errorf("exclude_operations not parsed: %+v", f.ExcludeOperations)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `~/sdk/go1.24.10/bin/go test ./internal/dashboardapi/ -run 'ParsesNewExcludes' -v`
Expected: FAIL — the four `f.Exclude*` fields are empty (params ignored by parseFilter).

- [ ] **Step 3: Implement**

In `internal/dashboardapi/api.go` `parseFilter`, after the `exclude_namespaces` block:

```go
	if v := splitCSV(q.Get("exclude_users")); len(v) > 0 {
		f.ExcludeUsers = v
	}
	if v := splitCSV(q.Get("exclude_clusters")); len(v) > 0 {
		f.ExcludeClusters = v
	}
	if v := splitCSV(q.Get("exclude_names")); len(v) > 0 {
		f.ExcludeNames = v
	}
	if v := splitCSV(q.Get("exclude_operations")); len(v) > 0 {
		f.ExcludeOperations = v
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `~/sdk/go1.24.10/bin/go test ./internal/dashboardapi/ -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/dashboardapi/api.go internal/dashboardapi/api_test.go
git commit -m "feat(api): parse exclude_users/clusters/names/operations params"
```

---

### Task 3: Frontend filter plumbing — new keys in types and useFilters

**Files:**
- Modify: `web/src/types.ts` (Filters interface, ~line 47)
- Modify: `web/src/useFilters.ts` (FILTER_KEYS, ~line 5)
- Test: `web/src/useFilters.test.tsx`

**Interfaces:**
- Produces: `Filters` type gains optional string keys `exclude_users`, `exclude_clusters`, `exclude_names`, `exclude_operations` (CSV). `useFilters()` reads/writes/clears them like every other key. Tasks 4–6 rely on these key names exactly.

- [ ] **Step 1: Write the failing test**

Append to `web/src/useFilters.test.tsx`:

```tsx
test('tracks and clears the new exclude keys', () => {
  const { result } = renderHook(() => useFilters(), { wrapper })
  act(() => {
    result.current.setFilters({
      exclude_users: 'bot',
      exclude_clusters: 'staging',
      exclude_names: 'web',
      exclude_operations: 'UPDATE',
    })
  })
  expect(result.current.filters.exclude_users).toBe('bot')
  expect(result.current.filters.exclude_clusters).toBe('staging')
  expect(result.current.filters.exclude_names).toBe('web')
  expect(result.current.filters.exclude_operations).toBe('UPDATE')
  act(() => {
    result.current.clearFilters()
  })
  expect(result.current.filters).toEqual({})
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npm test -- src/useFilters.test.tsx`
Expected: FAIL — TypeScript rejects the unknown `Filters` keys (and/or the values don't round-trip because the keys aren't in `FILTER_KEYS`).

- [ ] **Step 3: Implement**

`web/src/types.ts` — extend `Filters`:

```ts
export interface Filters {
  q?: string
  cluster?: string
  namespace?: string
  kind?: string
  name?: string
  user?: string
  operation?: string
  from?: string
  to?: string
  exclude_kinds?: string // comma-separated kind list
  exclude_namespaces?: string // comma-separated namespace list
  exclude_users?: string // comma-separated user list
  exclude_clusters?: string // comma-separated cluster list
  exclude_names?: string // comma-separated name list
  exclude_operations?: string // comma-separated operation list
}
```

`web/src/useFilters.ts` — extend `FILTER_KEYS`:

```ts
const FILTER_KEYS: (keyof Filters)[] = [
  'q', 'cluster', 'namespace', 'kind', 'name', 'user', 'operation',
  'from', 'to', 'exclude_kinds', 'exclude_namespaces',
  'exclude_users', 'exclude_clusters', 'exclude_names', 'exclude_operations',
]
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd web && npm test -- src/useFilters.test.tsx`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/types.ts web/src/useFilters.ts web/src/useFilters.test.tsx
git commit -m "feat(web): filter plumbing for new exclude params"
```

---

### Task 4: CellFilter component — hover ⊕/⊖ buttons

**Files:**
- Create: `web/src/components/CellFilter.tsx`
- Test: `web/src/components/CellFilter.test.tsx`

**Interfaces:**
- Consumes: `useFilters`, `addToList`, `removeFromList` from `web/src/useFilters.ts`; `Filters` from `web/src/types.ts` (Task 3 keys).
- Produces: `CellFilter` React component with props `{ field: FilterField; value: string; children: React.ReactNode }` where `export type FilterField = 'kind' | 'namespace' | 'name' | 'user' | 'cluster' | 'operation'`. Renders children plus hover-revealed include/exclude buttons with aria-labels `` `include ${field} ${value}` `` and `` `exclude ${field} ${value}` ``. Task 5 wraps EventList cells in it.

- [ ] **Step 1: Write the failing test**

Create `web/src/components/CellFilter.test.tsx`:

```tsx
import { test, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, useSearchParams } from 'react-router-dom'
import { CellFilter } from './CellFilter'

function Harness({ children }: { children: React.ReactNode }) {
  const [params] = useSearchParams()
  return (
    <>
      {children}
      <span data-testid="qs">{decodeURIComponent(params.toString())}</span>
    </>
  )
}

function renderCell(initial: string, ui: React.ReactNode, onRow = vi.fn()) {
  render(
    <MemoryRouter initialEntries={[initial]}>
      <Harness>
        <div onClick={onRow}>{ui}</div>
      </Harness>
    </MemoryRouter>,
  )
  return onRow
}

test('include click sets the single-value param', async () => {
  renderCell('/', <CellFilter field="kind" value="Deployment">Deployment</CellFilter>)
  await userEvent.click(screen.getByRole('button', { name: 'include kind Deployment' }))
  expect(screen.getByTestId('qs').textContent).toBe('kind=Deployment')
})

test('exclude click appends to the CSV param', async () => {
  renderCell('/?exclude_kinds=Lease', <CellFilter field="kind" value="Endpoints">Endpoints</CellFilter>)
  await userEvent.click(screen.getByRole('button', { name: 'exclude kind Endpoints' }))
  expect(screen.getByTestId('qs').textContent).toBe('exclude_kinds=Lease,Endpoints')
})

test('excluding a value clears a matching include', async () => {
  renderCell('/?kind=Deployment', <CellFilter field="kind" value="Deployment">Deployment</CellFilter>)
  await userEvent.click(screen.getByRole('button', { name: 'exclude kind Deployment' }))
  expect(screen.getByTestId('qs').textContent).toBe('exclude_kinds=Deployment')
})

test('including a value removes it from the exclude list', async () => {
  renderCell('/?exclude_users=bot,alice', <CellFilter field="user" value="alice">alice</CellFilter>)
  await userEvent.click(screen.getByRole('button', { name: 'include user alice' }))
  expect(screen.getByTestId('qs').textContent).toBe('exclude_users=bot&user=alice')
})

test('button clicks do not bubble to the row', async () => {
  const onRow = renderCell('/', <CellFilter field="cluster" value="c1">c1</CellFilter>)
  await userEvent.click(screen.getByRole('button', { name: 'include cluster c1' }))
  await userEvent.click(screen.getByRole('button', { name: 'exclude cluster c1' }))
  expect(onRow).not.toHaveBeenCalled()
})

test('renders children without buttons when value is empty', () => {
  renderCell('/', <CellFilter field="namespace" value="">—</CellFilter>)
  expect(screen.getByText('—')).toBeInTheDocument()
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npm test -- src/components/CellFilter.test.tsx`
Expected: FAIL — cannot resolve `./CellFilter`.

- [ ] **Step 3: Implement**

Create `web/src/components/CellFilter.tsx`:

```tsx
import type { MouseEvent, ReactNode } from 'react'
import { useFilters, addToList, removeFromList } from '../useFilters'
import type { Filters } from '../types'

export type FilterField = 'kind' | 'namespace' | 'name' | 'user' | 'cluster' | 'operation'

const EXCLUDE_KEY: Record<FilterField, keyof Filters> = {
  kind: 'exclude_kinds',
  namespace: 'exclude_namespaces',
  name: 'exclude_names',
  user: 'exclude_users',
  cluster: 'exclude_clusters',
  operation: 'exclude_operations',
}

const btnCls = 'rounded px-0.5 leading-none text-zinc-500 hover:text-zinc-100'

// Wraps a table-cell value with hover-revealed one-click include (⊕) and
// exclude (⊖) filter buttons. Include sets the single-value param; exclude
// appends to the field's CSV exclude param. Applying one side always clears
// the same value from the other so the two can never contradict.
export function CellFilter({ field, value, children }: { field: FilterField; value: string; children: ReactNode }) {
  const { filters, setFilters } = useFilters()
  if (!value) return <>{children}</>
  const excludeKey = EXCLUDE_KEY[field]

  const include = (e: MouseEvent) => {
    e.stopPropagation()
    setFilters({ [field]: value, [excludeKey]: removeFromList(filters[excludeKey], value) })
  }
  const exclude = (e: MouseEvent) => {
    e.stopPropagation()
    setFilters({
      [excludeKey]: addToList(filters[excludeKey], value),
      ...(filters[field] === value ? { [field]: '' } : {}),
    })
  }

  return (
    <span className="group/cf inline-flex items-center gap-1">
      {children}
      <span
        className="hidden items-center group-hover/cf:inline-flex group-focus-within/cf:inline-flex"
        onKeyDown={(e) => e.stopPropagation()}
      >
        <button aria-label={`include ${field} ${value}`} title="Include" className={btnCls} onClick={include}>
          ⊕
        </button>
        <button aria-label={`exclude ${field} ${value}`} title="Exclude" className={btnCls} onClick={exclude}>
          ⊖
        </button>
      </span>
    </span>
  )
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd web && npm test -- src/components/CellFilter.test.tsx`
Expected: all 6 PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/CellFilter.tsx web/src/components/CellFilter.test.tsx
git commit -m "feat(web): CellFilter hover include/exclude buttons"
```

---

### Task 5: EventList — wrap cell values in CellFilter

**Files:**
- Modify: `web/src/components/EventList.tsx` (table body cells, ~lines 71–83)
- Test: `web/src/components/EventList.test.tsx`

**Interfaces:**
- Consumes: `CellFilter` + `FilterField` from Task 4.
- Produces: table cells for operation, kind, namespace, name, user, and cluster wrapped in `CellFilter`. The Resource cell renders kind, namespace, and name as three independent hover targets (namespace and name joined by a plain `/`).

Note: `EventList.test.tsx` currently renders without a router; `CellFilter` uses `useFilters`, which needs one — the test harness gains a `MemoryRouter` wrapper. The Resource cell no longer renders a single `default/web` text node, so the two existing assertions that match it change too.

- [ ] **Step 1: Update the test file**

Replace `web/src/components/EventList.test.tsx` with:

```tsx
import { afterEach, test, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { EventList } from './EventList'

afterEach(() => vi.unstubAllGlobals())

const row = {
  event_id: '1', event_time: '2026-07-01T10:00:00Z', ingested_at: '2026-07-01T10:00:01Z',
  cluster: 'c1', source: 'webhook', operation: 'DELETE', api_group: 'apps', api_version: 'v1',
  kind: 'Deployment', namespace: 'default', name: 'web', resource_uid: 'u', sub_resource: '',
  user_name: 'alice', user_groups: [], dry_run: true, diff: '[]',
}

function renderList(onSelect = vi.fn()) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({
      ok: true, status: 200,
      json: () => Promise.resolve({ events: [row], next_cursor: '' }),
    }),
  )
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <MemoryRouter>
      <QueryClientProvider client={qc}>
        <EventList filters={{}} onSelect={onSelect} />
      </QueryClientProvider>
    </MemoryRouter>,
  )
  return onSelect
}

test('renders operation badge, resource, dry-run marker', async () => {
  renderList()
  expect(await screen.findByText('DELETE')).toBeInTheDocument()
  expect(screen.getByText('default')).toBeInTheDocument()
  expect(screen.getByText('web')).toBeInTheDocument()
  expect(screen.getByText('dry-run')).toBeInTheDocument()
  expect(screen.getByText('alice')).toBeInTheDocument()
})

test('row activates with keyboard', async () => {
  // Find the row via a cell's text: role-based queries would be ambiguous now
  // that each row also contains the (CSS-hidden) include/exclude buttons,
  // which jsdom cannot hide because Tailwind styles are not loaded in tests.
  const onSelect = renderList()
  const rowEl = (await screen.findByText('dry-run')).closest('tr')!
  rowEl.focus()
  await userEvent.keyboard('{Enter}')
  expect(onSelect).toHaveBeenCalledWith('1')
})

test('cell values expose include/exclude filter buttons', async () => {
  renderList()
  await screen.findByText('DELETE')
  for (const name of [
    'include kind Deployment', 'exclude kind Deployment',
    'include namespace default', 'exclude namespace default',
    'include name web', 'exclude name web',
    'include user alice', 'exclude user alice',
    'include cluster c1', 'exclude cluster c1',
    'include operation DELETE', 'exclude operation DELETE',
  ]) {
    expect(screen.getByRole('button', { name })).toBeInTheDocument()
  }
})

test('include button click filters without opening the detail row', async () => {
  const onSelect = renderList()
  await screen.findByText('DELETE')
  await userEvent.click(screen.getByRole('button', { name: 'include user alice' }))
  expect(onSelect).not.toHaveBeenCalled()
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npm test -- src/components/EventList.test.tsx`
Expected: FAIL — no `include kind Deployment` buttons, and `getByText('default')` fails (cell still renders `default/web` as one node).

- [ ] **Step 3: Implement**

In `web/src/components/EventList.tsx`, add the import:

```tsx
import { CellFilter } from './CellFilter'
```

Replace the operation, resource, user, and cluster `<td>`s in the row render with:

```tsx
              <td className="whitespace-nowrap px-3 py-2">
                <CellFilter field="operation" value={r.operation}>
                  <OperationBadge op={r.operation} />
                </CellFilter>
                {r.dry_run && (
                  <span className="ml-1.5 rounded border border-zinc-600 px-1 py-0.5 text-xs text-zinc-400">dry-run</span>
                )}
              </td>
              <td className="px-3 py-2 font-mono text-xs">
                <CellFilter field="kind" value={r.kind}>
                  <span className="text-zinc-500">{r.kind}</span>
                </CellFilter>{' '}
                {r.namespace && (
                  <>
                    <CellFilter field="namespace" value={r.namespace}>
                      <span className="text-zinc-100">{r.namespace}</span>
                    </CellFilter>
                    <span className="text-zinc-500">/</span>
                  </>
                )}
                <CellFilter field="name" value={r.name}>
                  <span className="text-zinc-100">{r.name}</span>
                </CellFilter>
              </td>
              <td className="whitespace-nowrap px-3 py-2 text-zinc-300">
                <CellFilter field="user" value={r.user_name}>{r.user_name}</CellFilter>
              </td>
              <td className="whitespace-nowrap px-3 py-2 text-zinc-400">
                <CellFilter field="cluster" value={r.cluster}>{r.cluster}</CellFilter>
              </td>
```

(The Time and Changes cells are unchanged.)

- [ ] **Step 4: Run the full web test suite**

Run: `cd web && npm test`
Expected: all PASS. If `App.test.tsx` or `App.routing.test.tsx` assert on the old `default/web` text, update those assertions the same way as Step 1 (separate `default` and `web` nodes).

- [ ] **Step 5: Commit**

```bash
git add web/src/components/EventList.tsx web/src/components/EventList.test.tsx
git commit -m "feat(web): hover include/exclude buttons on event table cells"
```

---

### Task 6: FilterChips — chips for the new exclude params

**Files:**
- Modify: `web/src/components/FilterChips.tsx`
- Test: `web/src/components/FilterChips.test.tsx`

**Interfaces:**
- Consumes: `Filters` keys from Task 3.
- Produces: red "not …" chips for all six exclude params, each removable individually. Chip labels: `not kind: X`, `not ns: X`, `not user: X`, `not cluster: X`, `not name: X`, `not op: X`.

- [ ] **Step 1: Write the failing test**

Append to `web/src/components/FilterChips.test.tsx`:

```tsx
test('shows and removes chips for the new exclude params', async () => {
  renderChips('/?exclude_users=bot,alice&exclude_clusters=staging&exclude_names=web&exclude_operations=UPDATE')
  expect(screen.getByText('not user: bot')).toBeInTheDocument()
  expect(screen.getByText('not cluster: staging')).toBeInTheDocument()
  expect(screen.getByText('not name: web')).toBeInTheDocument()
  expect(screen.getByText('not op: UPDATE')).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'remove not user: bot' }))
  expect(screen.getByTestId('qs').textContent).toContain('exclude_users=alice')
  expect(screen.getByTestId('qs').textContent).not.toContain('bot')
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npm test -- src/components/FilterChips.test.tsx`
Expected: FAIL — `not user: bot` not found.

- [ ] **Step 3: Implement**

Replace the full contents of `web/src/components/FilterChips.tsx` with (only the exclude-chip logic changes; the JSX is identical to today's):

```tsx
import { useFilters, splitList, removeFromList } from '../useFilters'
import type { Filters } from '../types'

const SIMPLE_KEYS: (keyof Filters)[] = [
  'q', 'cluster', 'namespace', 'kind', 'name', 'user', 'operation', 'from', 'to',
]

const EXCLUDE_KEYS: { key: keyof Filters; label: string }[] = [
  { key: 'exclude_kinds', label: 'kind' },
  { key: 'exclude_namespaces', label: 'ns' },
  { key: 'exclude_users', label: 'user' },
  { key: 'exclude_clusters', label: 'cluster' },
  { key: 'exclude_names', label: 'name' },
  { key: 'exclude_operations', label: 'op' },
]

function chipLabel(key: string, value: string): string {
  if (key === 'from' || key === 'to') return `${key}: ${new Date(value).toLocaleString()}`
  return `${key}: ${value}`
}

export function FilterChips() {
  const { filters, setFilter, clearFilters } = useFilters()

  const chips: { label: string; onRemove: () => void }[] = []
  for (const key of SIMPLE_KEYS) {
    const v = filters[key]
    if (v) chips.push({ label: chipLabel(key, v), onRemove: () => setFilter(key, '') })
  }
  for (const { key, label } of EXCLUDE_KEYS) {
    for (const v of splitList(filters[key])) {
      chips.push({
        label: `not ${label}: ${v}`,
        onRemove: () => setFilter(key, removeFromList(filters[key], v)),
      })
    }
  }

  if (chips.length === 0) return null

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {chips.map((c) => (
        <span
          key={c.label}
          className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs ${
            c.label.startsWith('not ')
              ? 'border border-red-900 bg-red-950/50 text-red-300'
              : 'border border-zinc-700 bg-zinc-800 text-zinc-300'
          }`}
        >
          {c.label}
          <button
            aria-label={`remove ${c.label}`}
            className="text-zinc-500 hover:text-zinc-200"
            onClick={c.onRemove}
          >
            ×
          </button>
        </span>
      ))}
      <button
        className="ml-1 text-xs text-zinc-500 underline hover:text-zinc-300"
        onClick={clearFilters}
      >
        Clear all
      </button>
    </div>
  )
}
```

- [ ] **Step 4: Run the full web suite and Go suite**

Run: `cd web && npm test`
Expected: all PASS.

Run: `~/sdk/go1.24.10/bin/go test ./...`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/FilterChips.tsx web/src/components/FilterChips.test.tsx
git commit -m "feat(web): filter chips for new exclude params"
```
