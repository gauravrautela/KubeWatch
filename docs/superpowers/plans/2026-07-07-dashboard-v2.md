# KubeWatch Dashboard v2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the unstyled KubeWatch SPA into a dark ops-console dashboard with complete filtering (search, namespace, time range, ignore/exclude, chips) and a full detail page rendering each change as a unified diff.

**Architecture:** Small additive changes to the Go read API (`q` search, exclude params, namespaces facet), then a frontend pass: Tailwind v4 theme + app shell, extended URL-backed filter state, new filter components, and a `/events/:id` full page whose diff is built client-side by a pure `buildUnifiedDiff` function over jsdiff `diffLines`.

**Tech Stack:** Go 1.2x (stdlib HTTP, clickhouse-go), React 18, TanStack Query 5, react-router-dom 6, Recharts 2, Tailwind CSS v4 (`@tailwindcss/vite`), `diff` (jsdiff), Vitest + Testing Library.

**Spec:** `docs/superpowers/specs/2026-07-07-dashboard-v2-design.md`

## Global Constraints

- Dark theme ONLY — no light mode, no theme toggle.
- `from`/`to` sent to the API MUST be RFC3339 (`new Date(v).toISOString()`), never locale strings — the Go API 400s otherwise.
- Include filters are single-select; excludes are multi-value, serialized as comma-separated URL params `exclude_kinds` / `exclude_namespaces`.
- Filter state lives ONLY in the URL query string (shareable links); no localStorage.
- Frontend commands run from `web/` (`npm test -- <file>` filters to one file); Go commands from the repo root.
- TDD every task: write the failing test, see it fail, implement, see it pass, commit.
- Existing behaviors that must not regress: live-feed 5s poll dedup (`useLiveFeed`), keyset pagination, keyboard-activatable feed rows, `useFilters` stable identity memoization.

## File Structure

```
internal/storage/query.go        # Filter gains Q/ExcludeKinds/ExcludeNamespaces; namespaces facet query; Facets struct
internal/storage/read.go        # Facets() fetches namespaces
internal/dashboardapi/api.go    # parseFilter reads q + exclude params
web/vite.config.ts              # + tailwindcss plugin
web/src/index.css               # NEW: tailwind import + dark body
web/src/main.tsx                # + import './index.css'
web/src/App.tsx                 # shell div + Header; later: page routes
web/src/components/Header.tsx   # NEW: sticky wordmark + live dot
web/src/types.ts                # Filters + Facets extensions
web/src/useFilters.ts           # + q/exclude keys, setFilters, clearFilters, csv list helpers
web/src/timeRange.ts            # NEW: presets, pickBucket, bucketRange (pure)
web/src/time.ts                 # NEW: timeAgo (pure)
web/src/unifiedDiff.ts          # NEW: buildUnifiedDiff (pure)
web/src/components/FilterBar.tsx        # v2: search/selects/time range/ignore
web/src/components/FilterChips.tsx      # NEW: chips + clear all
web/src/components/DashboardPage.tsx    # v2 layout
web/src/components/ActivityHistogram.tsx# v2: auto bucket + bar-click zoom
web/src/components/EventList.tsx        # v2: styled table
web/src/components/OperationBadge.tsx   # NEW
web/src/components/DiffView.tsx         # NEW: unified diff renderer + fold expanders
web/src/components/EventDetail.tsx      # rewritten as EventDetailPage
DELETED: web/src/components/Drawer.tsx, Drawer.test.tsx, web/src/diff.ts, web/src/diff.test.ts
```

---

### Task 1: Go storage — `q` search and exclude conditions

**Files:**
- Modify: `internal/storage/query.go` (Filter struct ~line 14, `filterConds` ~line 161)
- Test: `internal/storage/query_test.go`

**Interfaces:**
- Consumes: existing `condBuilder`, `buildListQuery`, `buildActivityQuery`.
- Produces: `storage.Filter` fields `Q string`, `ExcludeKinds []string`, `ExcludeNamespaces []string` (Task 2's `parseFilter` sets them). SQL: `(name ILIKE ? OR user_name ILIKE ?)`, `kind NOT IN (?)`, `namespace NOT IN (?)` — clickhouse-go expands a slice arg bound to `IN (?)`.

- [ ] **Step 1: Write the failing test** — append to `internal/storage/query_test.go`:

```go
func TestBuildListQuerySearchAndExcludes(t *testing.T) {
	f := Filter{
		Q:                 "ali",
		ExcludeKinds:      []string{"Lease", "Endpoints"},
		ExcludeNamespaces: []string{"kube-system"},
	}
	q, args := buildListQuery(f, nil, nil, 50)

	for _, want := range []string{
		"(name ILIKE ? OR user_name ILIKE ?)",
		"kind NOT IN (?)",
		"namespace NOT IN (?)",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("query missing %q\n%s", want, q)
		}
	}
	// args order: %ali%, %ali%, exclude kinds slice, exclude namespaces slice, limit
	if len(args) != 5 {
		t.Fatalf("want 5 args, got %d: %v", len(args), args)
	}
	if args[0] != "%ali%" || args[1] != "%ali%" {
		t.Fatalf("q args wrong: %v", args)
	}
	kinds, ok := args[2].([]string)
	if !ok || len(kinds) != 2 || kinds[0] != "Lease" {
		t.Fatalf("exclude kinds arg wrong: %v", args[2])
	}
}

func TestBuildActivityQueryAppliesExcludes(t *testing.T) {
	q, args, err := buildActivityQuery(Filter{ExcludeNamespaces: []string{"kube-system"}}, "hour")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q, "namespace NOT IN (?)") {
		t.Errorf("activity query missing exclude:\n%s", q)
	}
	if len(args) != 1 {
		t.Fatalf("want 1 arg, got %v", args)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/storage/ -run 'TestBuildListQuerySearchAndExcludes|TestBuildActivityQueryAppliesExcludes' -v`
Expected: FAIL — `unknown field Q in struct literal`.

- [ ] **Step 3: Implement.** In `internal/storage/query.go`, extend `Filter`:

```go
// Filter holds the structured, indexable search dimensions for the dashboard.
type Filter struct {
	Cluster   string
	Namespace string
	Kind      string
	Name      string // substring, case-insensitive
	User      string // substring, case-insensitive
	Q         string // substring, case-insensitive; matches name OR user_name
	Operation string
	From      time.Time // zero => unbounded
	To        time.Time // zero => unbounded

	ExcludeKinds      []string // exact kinds to exclude (ignore filter)
	ExcludeNamespaces []string // exact namespaces to exclude (ignore filter)
}
```

In `filterConds`, after the `f.User` block and before the `f.From` block, add:

```go
	if f.Q != "" {
		p := "%" + f.Q + "%"
		b.conds = append(b.conds, "(name ILIKE ? OR user_name ILIKE ?)")
		b.args = append(b.args, p, p)
	}
	if len(f.ExcludeKinds) > 0 {
		b.add("kind NOT IN (?)", f.ExcludeKinds)
	}
	if len(f.ExcludeNamespaces) > 0 {
		b.add("namespace NOT IN (?)", f.ExcludeNamespaces)
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/storage/ -v`
Expected: PASS (all storage tests, including pre-existing ones).

- [ ] **Step 5: Commit**

```bash
git add internal/storage/query.go internal/storage/query_test.go
git commit -m "feat(storage): add q search and kind/namespace exclude filters"
```

---

### Task 2: Go API — parse `q`/excludes, add namespaces facet

**Files:**
- Modify: `internal/storage/query.go` (facet query consts ~line 124, `Facets` struct ~line 101)
- Modify: `internal/storage/read.go` (`Facets()` ~line 119)
- Modify: `internal/dashboardapi/api.go` (`parseFilter` ~line 143)
- Test: `internal/dashboardapi/api_test.go`

**Interfaces:**
- Consumes: Task 1's `Filter.Q/ExcludeKinds/ExcludeNamespaces`.
- Produces: query params `q`, `exclude_kinds`, `exclude_namespaces` (comma-separated) on `/api/events` and `/api/activity`; `GET /api/facets` response gains `"namespaces": [...]`.

- [ ] **Step 1: Write the failing tests** — append to `internal/dashboardapi/api_test.go`, and change `fakeStore.Facets` to return namespaces:

```go
func (f *fakeStore) Facets(_ context.Context) (storage.Facets, error) {
	return storage.Facets{Clusters: []string{"c1"}, Namespaces: []string{"default", "kube-system"}}, nil
}
```

```go
func TestListEventsParsesSearchAndExcludes(t *testing.T) {
	fs := &fakeStore{}
	h := NewRouter(fs, "")
	rec := do(t, h, "/api/events?q=ali&exclude_kinds=Lease,Endpoints&exclude_namespaces=kube-system")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	f := fs.lastP.Filter
	if f.Q != "ali" {
		t.Errorf("q not parsed: %+v", f)
	}
	if len(f.ExcludeKinds) != 2 || f.ExcludeKinds[0] != "Lease" || f.ExcludeKinds[1] != "Endpoints" {
		t.Errorf("exclude_kinds not parsed: %+v", f.ExcludeKinds)
	}
	if len(f.ExcludeNamespaces) != 1 || f.ExcludeNamespaces[0] != "kube-system" {
		t.Errorf("exclude_namespaces not parsed: %+v", f.ExcludeNamespaces)
	}
}

func TestFacetsIncludesNamespaces(t *testing.T) {
	rec := do(t, NewRouter(&fakeStore{}, ""), "/api/facets")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var fc storage.Facets
	if err := json.Unmarshal(rec.Body.Bytes(), &fc); err != nil {
		t.Fatal(err)
	}
	if len(fc.Namespaces) != 2 || fc.Namespaces[0] != "default" {
		t.Fatalf("namespaces missing from facets: %+v", fc)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/dashboardapi/ -v`
Expected: FAIL — `unknown field Namespaces in struct literal`.

- [ ] **Step 3: Implement.**

`internal/storage/query.go` — extend `Facets` and add the query const:

```go
// Facets are distinct low-cardinality filter values for the UI dropdowns.
type Facets struct {
	Clusters   []string `json:"clusters"`
	Namespaces []string `json:"namespaces"`
	Kinds      []string `json:"kinds"`
	Operations []string `json:"operations"`
}
```

```go
const (
	facetClustersQuery   = "SELECT DISTINCT cluster FROM change_events ORDER BY cluster"
	facetNamespacesQuery = "SELECT DISTINCT namespace FROM change_events ORDER BY namespace"
	facetKindsQuery      = "SELECT DISTINCT kind FROM change_events ORDER BY kind"
	facetOperationsQuery = "SELECT DISTINCT operation FROM change_events ORDER BY operation"
)
```

`internal/storage/read.go` — replace the body of `Facets()`:

```go
// Facets returns distinct low-cardinality filter values for the UI.
func (s *Store) Facets(ctx context.Context) (Facets, error) {
	clusters, err := s.distinct(ctx, facetClustersQuery)
	if err != nil {
		return Facets{}, err
	}
	namespaces, err := s.distinct(ctx, facetNamespacesQuery)
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
	return Facets{Clusters: clusters, Namespaces: namespaces, Kinds: kinds, Operations: ops}, nil
}
```

`internal/dashboardapi/api.go` — add `"strings"` to imports and extend `parseFilter` (after the existing field assignments, before the `from` block):

```go
	f.Q = q.Get("q")
	if v := q.Get("exclude_kinds"); v != "" {
		f.ExcludeKinds = strings.Split(v, ",")
	}
	if v := q.Get("exclude_namespaces"); v != "" {
		f.ExcludeNamespaces = strings.Split(v, ",")
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... `
Expected: PASS across all packages.

- [ ] **Step 5: Commit**

```bash
git add internal/storage/query.go internal/storage/read.go internal/dashboardapi/api.go internal/dashboardapi/api_test.go
git commit -m "feat(api): q search + exclude params, namespaces facet"
```

---

### Task 3: Tailwind setup + app shell (Header)

**Files:**
- Modify: `web/package.json` (via npm install), `web/vite.config.ts`, `web/src/main.tsx`, `web/src/App.tsx`, `web/src/components/DashboardPage.tsx`, `web/src/App.test.tsx`
- Create: `web/src/index.css`, `web/src/components/Header.tsx`

**Interfaces:**
- Consumes: nothing new.
- Produces: Tailwind utilities available in every component; `<Header />` rendered above routes. Later tasks style with Tailwind classes only.

- [ ] **Step 1: Install dependencies**

```bash
cd web
npm install diff
npm install -D tailwindcss @tailwindcss/vite
```

If the build in Step 5 fails with TS7016 (no declaration file for 'diff'), also run `npm install -D @types/diff` (recent `diff` versions bundle types; older ones need the stub).

- [ ] **Step 2: Write the failing test** — replace `web/src/App.test.tsx`:

```tsx
import { afterEach, test, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from './App'

afterEach(() => vi.unstubAllGlobals())

test('renders the header wordmark linking home', () => {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({ ok: true, status: 200, json: () => Promise.resolve({}) }),
  )
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/']}>
        <App />
      </MemoryRouter>
    </QueryClientProvider>,
  )
  expect(screen.getByRole('link', { name: /kubewatch/i })).toBeInTheDocument()
})
```

- [ ] **Step 3: Run test to verify it fails**

Run: `npm test -- src/App.test.tsx`
Expected: FAIL — no link named /kubewatch/i.

- [ ] **Step 4: Implement.**

`web/src/index.css` (new):

```css
@import "tailwindcss";

body {
  @apply bg-zinc-950 text-zinc-200 antialiased;
}
```

`web/vite.config.ts`:

```ts
/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    proxy: { '/api': 'http://localhost:8081' },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/vitest.setup.ts'],
  },
})
```

`web/src/main.tsx` — add `import './index.css'` after the react-dom import.

`web/src/components/Header.tsx` (new):

```tsx
import { Link } from 'react-router-dom'

export function Header() {
  return (
    <header className="sticky top-0 z-10 border-b border-zinc-800 bg-zinc-950/90 backdrop-blur">
      <div className="mx-auto flex max-w-6xl items-center gap-3 px-4 py-3">
        <Link to="/" className="text-base font-bold tracking-tight text-zinc-100">
          KubeWatch
        </Link>
        <span className="flex items-center gap-1.5 rounded-full border border-zinc-800 px-2 py-0.5 text-xs text-zinc-400">
          <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-emerald-500" aria-hidden />
          live
        </span>
      </div>
    </header>
  )
}
```

`web/src/App.tsx` — add the shell and Header (routes unchanged for now):

```tsx
import { Routes, Route, useNavigate, useParams, useLocation } from 'react-router-dom'
import { DashboardPage } from './components/DashboardPage'
import { Drawer } from './components/Drawer'
import { EventDetail } from './components/EventDetail'
import { Header } from './components/Header'

function EventDrawer() {
  const { id } = useParams()
  const navigate = useNavigate()
  const location = useLocation()
  if (!id) return null
  return (
    <Drawer onClose={() => navigate(`/${location.search}`)}>
      <EventDetail id={id} />
    </Drawer>
  )
}

export function App() {
  return (
    <div className="min-h-screen">
      <Header />
      <DashboardPage />
      <Routes>
        <Route path="/events/:id" element={<EventDrawer />} />
        <Route path="*" element={null} />
      </Routes>
    </div>
  )
}
```

`web/src/components/DashboardPage.tsx` — remove the `<h1>KubeWatch Dashboard</h1>` line (the wordmark replaces it); leave the rest untouched.

- [ ] **Step 5: Run tests and build**

Run: `npm test && npm run build`
Expected: all tests PASS; `vite build` succeeds and emits CSS containing zinc background rules.

- [ ] **Step 6: Commit**

```bash
git add web/package.json web/package-lock.json web/vite.config.ts web/src/index.css web/src/main.tsx web/src/App.tsx web/src/App.test.tsx web/src/components/Header.tsx web/src/components/DashboardPage.tsx
git commit -m "feat(web): tailwind v4 dark theme, app shell with header"
```

---

### Task 4: Filter state extensions (`q`, excludes, batch set, clear)

**Files:**
- Modify: `web/src/types.ts`, `web/src/useFilters.ts`
- Test: `web/src/useFilters.test.tsx`

**Interfaces:**
- Consumes: react-router `useSearchParams` (existing pattern).
- Produces (used by Tasks 5–8):
  - `Filters` gains `q?`, `exclude_kinds?`, `exclude_namespaces?` (csv strings); `Facets` gains `namespaces: string[]`.
  - `useFilters()` returns `{ filters, setFilter, setFilters, clearFilters }` where `setFilters(patch: Partial<Record<keyof Filters, string>>)` writes several keys in ONE history replace (empty string deletes a key) and `clearFilters()` deletes all filter keys.
  - Pure csv helpers exported from `useFilters.ts`: `splitList(csv?: string): string[]`, `addToList(csv: string | undefined, value: string): string`, `removeFromList(csv: string | undefined, value: string): string`.

- [ ] **Step 1: Write the failing tests** — append to `web/src/useFilters.test.tsx`:

```tsx
import { splitList, addToList, removeFromList } from './useFilters'

test('setFilters writes several keys in one update; empty deletes', () => {
  const { result } = renderHook(() => useFilters(), { wrapper })
  act(() => {
    result.current.setFilters({ from: '2026-07-07T00:00:00.000Z', to: '2026-07-07T01:00:00.000Z' })
  })
  expect(result.current.filters.from).toBe('2026-07-07T00:00:00.000Z')
  expect(result.current.filters.to).toBe('2026-07-07T01:00:00.000Z')
  act(() => {
    result.current.setFilters({ from: '', to: '' })
  })
  expect(result.current.filters.from).toBeUndefined()
  expect(result.current.filters.to).toBeUndefined()
})

test('clearFilters removes every filter key', () => {
  const { result } = renderHook(() => useFilters(), { wrapper }) // starts with cluster=c1
  act(() => {
    result.current.setFilter('exclude_kinds', 'Lease,Endpoints')
  })
  act(() => {
    result.current.clearFilters()
  })
  expect(result.current.filters).toEqual({})
})

test('csv list helpers add, dedupe, and remove', () => {
  expect(splitList(undefined)).toEqual([])
  expect(splitList('a,b')).toEqual(['a', 'b'])
  expect(addToList(undefined, 'Lease')).toBe('Lease')
  expect(addToList('Lease', 'Endpoints')).toBe('Lease,Endpoints')
  expect(addToList('Lease', 'Lease')).toBe('Lease')
  expect(removeFromList('Lease,Endpoints', 'Lease')).toBe('Endpoints')
  expect(removeFromList('Lease', 'Lease')).toBe('')
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `npm test -- src/useFilters.test.tsx`
Expected: FAIL — `splitList` not exported / `setFilters` undefined.

- [ ] **Step 3: Implement.**

`web/src/types.ts` — replace the `Filters` and `Facets` interfaces:

```ts
export interface Facets {
  clusters: string[]
  namespaces: string[]
  kinds: string[]
  operations: string[]
}

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
}
```

`web/src/useFilters.ts` — full replacement:

```ts
import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import type { Filters } from './types'

const FILTER_KEYS: (keyof Filters)[] = [
  'q', 'cluster', 'namespace', 'kind', 'name', 'user', 'operation',
  'from', 'to', 'exclude_kinds', 'exclude_namespaces',
]

// csv helpers for the multi-value exclude params.
export function splitList(csv?: string): string[] {
  return csv ? csv.split(',').filter(Boolean) : []
}

export function addToList(csv: string | undefined, value: string): string {
  const items = splitList(csv)
  if (!items.includes(value)) items.push(value)
  return items.join(',')
}

export function removeFromList(csv: string | undefined, value: string): string {
  return splitList(csv).filter((v) => v !== value).join(',')
}

export function useFilters() {
  const [params, setParams] = useSearchParams()

  // A change-detection key over just the filter params, so the memoized filters
  // object keeps a stable identity across re-renders when they are unchanged.
  const filterKey = FILTER_KEYS.map((k) => params.get(k) ?? '').join(' ')

  const filters = useMemo<Filters>(() => {
    const f: Filters = {}
    for (const key of FILTER_KEYS) {
      const v = params.get(key)
      if (v) f[key] = v
    }
    return f
    // filterKey fully captures the params we read; params identity is not stable.
  }, [filterKey]) // eslint-disable-line react-hooks/exhaustive-deps

  const setFilters = useCallback(
    (patch: Partial<Record<keyof Filters, string>>) => {
      setParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          for (const [key, value] of Object.entries(patch)) {
            if (value) next.set(key, value)
            else next.delete(key)
          }
          return next
        },
        { replace: true },
      )
    },
    [setParams],
  )

  const setFilter = useCallback(
    (key: keyof Filters, value: string) => setFilters({ [key]: value }),
    [setFilters],
  )

  const clearFilters = useCallback(() => {
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        for (const key of FILTER_KEYS) next.delete(key)
        return next
      },
      { replace: true },
    )
  }, [setParams])

  return { filters, setFilter, setFilters, clearFilters }
}
```

Note: the `filterKey` join separator changes from `''` to `' '` so adjacent values can't collide (`a,b`+`c` vs `a`+`b,c`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `npm test -- src/useFilters.test.tsx`
Expected: PASS, including the two pre-existing stable-identity tests.

- [ ] **Step 5: Commit**

```bash
git add web/src/types.ts web/src/useFilters.ts web/src/useFilters.test.tsx
git commit -m "feat(web): filter state gains q/excludes, batch set and clear"
```

---

### Task 5: Time-range helpers (pure)

**Files:**
- Create: `web/src/timeRange.ts`
- Test: `web/src/timeRange.test.ts`

**Interfaces:**
- Produces (used by Tasks 6 and 7):

```ts
export const PRESETS: Record<'15m' | '1h' | '6h' | '24h' | '7d', number> // duration ms
export type Preset = keyof typeof PRESETS
export function presetFrom(p: Preset, now?: Date): string // RFC3339 from
export type BucketUnit = 'minute' | 'hour' | 'day'
export function pickBucket(from?: string, to?: string, now?: Date): BucketUnit
export function bucketRange(bucketStartIso: string, unit: BucketUnit): { from: string; to: string }
```

- [ ] **Step 1: Write the failing tests** — `web/src/timeRange.test.ts`:

```ts
import { test, expect } from 'vitest'
import { presetFrom, pickBucket, bucketRange } from './timeRange'

const now = new Date('2026-07-07T12:00:00.000Z')

test('presetFrom returns RFC3339 now-minus-duration', () => {
  expect(presetFrom('1h', now)).toBe('2026-07-07T11:00:00.000Z')
  expect(presetFrom('7d', now)).toBe('2026-06-30T12:00:00.000Z')
})

test('pickBucket: no from => day; <=2h => minute; <=3d => hour; else day', () => {
  expect(pickBucket(undefined, undefined, now)).toBe('day')
  expect(pickBucket('2026-07-07T11:00:00.000Z', undefined, now)).toBe('minute')
  expect(pickBucket('2026-07-06T12:00:00.000Z', undefined, now)).toBe('hour')
  expect(pickBucket('2026-06-01T00:00:00.000Z', undefined, now)).toBe('day')
  expect(pickBucket('2026-07-01T00:00:00.000Z', '2026-07-01T01:00:00.000Z', now)).toBe('minute')
})

test('bucketRange spans exactly one bucket', () => {
  expect(bucketRange('2026-07-07T11:00:00.000Z', 'hour')).toEqual({
    from: '2026-07-07T11:00:00.000Z',
    to: '2026-07-07T12:00:00.000Z',
  })
  expect(bucketRange('2026-07-07T11:00:00.000Z', 'minute').to).toBe('2026-07-07T11:01:00.000Z')
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npm test -- src/timeRange.test.ts`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement** — `web/src/timeRange.ts`:

```ts
export const PRESETS = {
  '15m': 15 * 60_000,
  '1h': 3_600_000,
  '6h': 6 * 3_600_000,
  '24h': 24 * 3_600_000,
  '7d': 7 * 86_400_000,
} as const

export type Preset = keyof typeof PRESETS

// presetFrom returns the RFC3339 `from` bound for a live preset window.
// `to` is intentionally left unset by callers so the feed stays live.
export function presetFrom(p: Preset, now: Date = new Date()): string {
  return new Date(now.getTime() - PRESETS[p]).toISOString()
}

export type BucketUnit = 'minute' | 'hour' | 'day'

const BUCKET_MS: Record<BucketUnit, number> = {
  minute: 60_000,
  hour: 3_600_000,
  day: 86_400_000,
}

// pickBucket chooses the histogram granularity for the active range:
// <=2h => minute, <=3d => hour, else (or unbounded) => day.
export function pickBucket(from?: string, to?: string, now: Date = new Date()): BucketUnit {
  if (!from) return 'day'
  const span = (to ? new Date(to) : now).getTime() - new Date(from).getTime()
  if (span <= 2 * 3_600_000) return 'minute'
  if (span <= 3 * 86_400_000) return 'hour'
  return 'day'
}

// bucketRange is the [from, to] window of one histogram bucket (for bar-click zoom).
export function bucketRange(bucketStartIso: string, unit: BucketUnit): { from: string; to: string } {
  const start = new Date(bucketStartIso)
  return {
    from: start.toISOString(),
    to: new Date(start.getTime() + BUCKET_MS[unit]).toISOString(),
  }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `npm test -- src/timeRange.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/timeRange.ts web/src/timeRange.test.ts
git commit -m "feat(web): pure time-range preset and bucket helpers"
```

---

### Task 6: FilterBar v2 (search, selects, time range, ignore)

**Files:**
- Modify: `web/src/components/FilterBar.tsx`
- Test: `web/src/components/FilterBar.test.tsx`

**Interfaces:**
- Consumes: `useFilters` (Task 4: `setFilter`, `setFilters`, `addToList`), `presetFrom`/`Preset` (Task 5), `useFacets` (namespaces now present per Task 2).
- Produces: `<FilterBar />` (no props). Accessible names used by tests: inputs `search`, `from`, `to`; selects `cluster`, `namespace`, `kind`, `operation`, `time range`, `ignore`; button `Apply`.

- [ ] **Step 1: Write the failing tests** — replace `web/src/components/FilterBar.test.tsx`:

```tsx
import { afterEach, test, expect, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, useSearchParams } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { FilterBar } from './FilterBar'

afterEach(() => vi.unstubAllGlobals())

function mockFacets() {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: () =>
        Promise.resolve({
          clusters: ['c1', 'c2'],
          namespaces: ['default', 'kube-system'],
          kinds: ['Deployment', 'Lease'],
          operations: ['UPDATE'],
        }),
    }),
  )
}

function Harness() {
  const [params] = useSearchParams()
  return (
    <>
      <FilterBar />
      <span data-testid="qs">{decodeURIComponent(params.toString())}</span>
    </>
  )
}

function renderBar(initial = '/') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initial]}>
        <Harness />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

const qs = () => screen.getByTestId('qs').textContent ?? ''

test('search box debounces into the q param', async () => {
  mockFacets()
  renderBar()
  await userEvent.type(screen.getByLabelText('search'), 'ali')
  await waitFor(() => expect(qs()).toContain('q=ali'))
})

test('selecting a namespace writes it to the URL', async () => {
  mockFacets()
  renderBar()
  await screen.findByRole('option', { name: 'kube-system' })
  await userEvent.selectOptions(screen.getByLabelText('namespace'), 'kube-system')
  expect(qs()).toContain('namespace=kube-system')
})

test('picking a preset writes an RFC3339 from and no to', async () => {
  mockFacets()
  renderBar()
  await userEvent.selectOptions(screen.getByLabelText('time range'), '1h')
  expect(qs()).toMatch(/from=\d{4}-\d{2}-\d{2}T[\d:.]+Z/)
  expect(qs()).not.toContain('to=')
})

test('custom range applies both bounds as RFC3339', async () => {
  mockFacets()
  renderBar()
  await userEvent.selectOptions(screen.getByLabelText('time range'), 'custom')
  await userEvent.type(screen.getByLabelText('from'), '2026-07-07T10:00')
  await userEvent.type(screen.getByLabelText('to'), '2026-07-07T11:00')
  await userEvent.click(screen.getByRole('button', { name: 'Apply' }))
  expect(qs()).toMatch(/from=[^&]+Z/)
  expect(qs()).toMatch(/to=[^&]+Z/)
})

test('ignore select accumulates comma-separated excludes', async () => {
  mockFacets()
  renderBar()
  await screen.findByRole('option', { name: 'Lease' })
  await userEvent.selectOptions(screen.getByLabelText('ignore'), 'kind:Lease')
  expect(qs()).toContain('exclude_kinds=Lease')
  await userEvent.selectOptions(screen.getByLabelText('ignore'), 'ns:kube-system')
  expect(qs()).toContain('exclude_namespaces=kube-system')
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `npm test -- src/components/FilterBar.test.tsx`
Expected: FAIL — no element labelled `search`.

- [ ] **Step 3: Implement** — replace `web/src/components/FilterBar.tsx`:

```tsx
import { useEffect, useState } from 'react'
import { useFacets } from '../api/hooks'
import { useFilters, addToList } from '../useFilters'
import { presetFrom, PRESETS, type Preset } from '../timeRange'

const fieldCls =
  'rounded-md border border-zinc-700 bg-zinc-900 px-2 py-1.5 text-sm text-zinc-200 ' +
  'placeholder:text-zinc-500 focus:border-sky-600 focus:outline-none'

export function FilterBar() {
  const { filters, setFilter, setFilters } = useFilters()
  const facets = useFacets()

  const clusters = facets.data?.clusters ?? []
  const namespaces = facets.data?.namespaces ?? []
  const kinds = facets.data?.kinds ?? []
  const operations = facets.data?.operations ?? []

  // Search box: local value debounced 300ms into the `q` URL param.
  const [search, setSearch] = useState(filters.q ?? '')
  useEffect(() => {
    setSearch(filters.q ?? '')
  }, [filters.q])
  useEffect(() => {
    const t = setTimeout(() => {
      if (search !== (filters.q ?? '')) setFilter('q', search)
    }, 300)
    return () => clearTimeout(t)
  }, [search, filters.q, setFilter])

  // Time range: preset select; presets set only `from` (live window),
  // custom applies both bounds. Values are always RFC3339 (the API 400s otherwise).
  const [range, setRange] = useState(filters.from || filters.to ? 'custom' : '')
  const [fromLocal, setFromLocal] = useState('')
  const [toLocal, setToLocal] = useState('')
  const onRange = (v: string) => {
    setRange(v)
    if (v === '') setFilters({ from: '', to: '' })
    else if (v !== 'custom') setFilters({ from: presetFrom(v as Preset), to: '' })
  }
  const applyCustom = () => {
    const f = new Date(fromLocal)
    const t = new Date(toLocal)
    if (isNaN(f.getTime()) || isNaN(t.getTime())) return
    setFilters({ from: f.toISOString(), to: t.toISOString() })
  }

  const onIgnore = (v: string) => {
    if (!v) return
    const sep = v.indexOf(':')
    const dim = v.slice(0, sep)
    const val = v.slice(sep + 1)
    if (dim === 'kind') setFilter('exclude_kinds', addToList(filters.exclude_kinds, val))
    else setFilter('exclude_namespaces', addToList(filters.exclude_namespaces, val))
  }

  const select = (
    label: string,
    key: 'cluster' | 'namespace' | 'kind' | 'operation',
    values: string[],
  ) => (
    <select
      aria-label={label}
      className={fieldCls}
      value={filters[key] ?? ''}
      onChange={(e) => setFilter(key, e.target.value)}
    >
      <option value="">{label}: all</option>
      {values.map((v) => (
        <option key={v} value={v}>
          {v}
        </option>
      ))}
    </select>
  )

  return (
    <div className="flex flex-wrap items-center gap-2">
      <input
        aria-label="search"
        className={`${fieldCls} w-56`}
        placeholder="Search name or user…"
        value={search}
        onChange={(e) => setSearch(e.target.value)}
      />
      {select('cluster', 'cluster', clusters)}
      {select('namespace', 'namespace', namespaces)}
      {select('kind', 'kind', kinds)}
      {select('operation', 'operation', operations)}

      <select aria-label="time range" className={fieldCls} value={range} onChange={(e) => onRange(e.target.value)}>
        <option value="">Time: all</option>
        {(Object.keys(PRESETS) as Preset[]).map((p) => (
          <option key={p} value={p}>
            Last {p}
          </option>
        ))}
        <option value="custom">Custom…</option>
      </select>
      {range === 'custom' && (
        <span className="flex items-center gap-1">
          <input aria-label="from" type="datetime-local" className={fieldCls} value={fromLocal} onChange={(e) => setFromLocal(e.target.value)} />
          <span className="text-zinc-500">–</span>
          <input aria-label="to" type="datetime-local" className={fieldCls} value={toLocal} onChange={(e) => setToLocal(e.target.value)} />
          <button className="rounded-md bg-sky-700 px-2 py-1.5 text-sm text-white hover:bg-sky-600" onClick={applyCustom}>
            Apply
          </button>
        </span>
      )}

      <select aria-label="ignore" className={fieldCls} value="" onChange={(e) => onIgnore(e.target.value)}>
        <option value="">Ignore…</option>
        <optgroup label="Kinds">
          {kinds.map((k) => (
            <option key={k} value={`kind:${k}`}>
              {k}
            </option>
          ))}
        </optgroup>
        <optgroup label="Namespaces">
          {namespaces.map((n) => (
            <option key={n} value={`ns:${n}`}>
              {n}
            </option>
          ))}
        </optgroup>
      </select>
    </div>
  )
}
```

Note: the old `name`/`user` inputs are replaced by the single `q` search box; the API still accepts `name`/`user` for deep links.

- [ ] **Step 4: Run tests to verify they pass**

Run: `npm test -- src/components/FilterBar.test.tsx`
Expected: PASS (5 tests).

- [ ] **Step 5: Commit**

```bash
git add web/src/components/FilterBar.tsx web/src/components/FilterBar.test.tsx
git commit -m "feat(web): FilterBar v2 with search, namespace, time range, ignore"
```

---

### Task 7: FilterChips + DashboardPage v2 layout

**Files:**
- Create: `web/src/components/FilterChips.tsx`, `web/src/components/FilterChips.test.tsx`
- Modify: `web/src/components/DashboardPage.tsx`

**Interfaces:**
- Consumes: `useFilters` + `splitList`/`removeFromList` (Task 4).
- Produces: `<FilterChips />` (no props) — one removable chip per active filter, exclude chips labelled `not kind: X` / `not ns: X`, a `Clear all` button; renders nothing when no filters are active.

- [ ] **Step 1: Write the failing tests** — `web/src/components/FilterChips.test.tsx`:

```tsx
import { test, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, useSearchParams } from 'react-router-dom'
import { FilterChips } from './FilterChips'

function Harness() {
  const [params] = useSearchParams()
  return (
    <>
      <FilterChips />
      <span data-testid="qs">{decodeURIComponent(params.toString())}</span>
    </>
  )
}

function renderChips(initial: string) {
  render(
    <MemoryRouter initialEntries={[initial]}>
      <Harness />
    </MemoryRouter>,
  )
}

test('renders nothing when no filters are active', () => {
  renderChips('/')
  expect(screen.queryByRole('button', { name: /clear all/i })).not.toBeInTheDocument()
})

test('shows a chip per filter and removes one on click', async () => {
  renderChips('/?cluster=c1&exclude_kinds=Lease,Endpoints')
  expect(screen.getByText('cluster: c1')).toBeInTheDocument()
  expect(screen.getByText('not kind: Lease')).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'remove not kind: Lease' }))
  expect(screen.getByTestId('qs').textContent).toContain('exclude_kinds=Endpoints')
  expect(screen.getByTestId('qs').textContent).not.toContain('Lease')
})

test('clear all removes every filter', async () => {
  renderChips('/?cluster=c1&q=ali&exclude_namespaces=kube-system')
  await userEvent.click(screen.getByRole('button', { name: /clear all/i }))
  expect(screen.getByTestId('qs').textContent).toBe('')
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `npm test -- src/components/FilterChips.test.tsx`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement.**

`web/src/components/FilterChips.tsx` (new):

```tsx
import { useFilters, splitList, removeFromList } from '../useFilters'
import type { Filters } from '../types'

const SIMPLE_KEYS: (keyof Filters)[] = [
  'q', 'cluster', 'namespace', 'kind', 'name', 'user', 'operation', 'from', 'to',
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
  for (const v of splitList(filters.exclude_kinds)) {
    chips.push({
      label: `not kind: ${v}`,
      onRemove: () => setFilter('exclude_kinds', removeFromList(filters.exclude_kinds, v)),
    })
  }
  for (const v of splitList(filters.exclude_namespaces)) {
    chips.push({
      label: `not ns: ${v}`,
      onRemove: () => setFilter('exclude_namespaces', removeFromList(filters.exclude_namespaces, v)),
    })
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

`web/src/components/DashboardPage.tsx` — full replacement:

```tsx
import { useLocation, useNavigate } from 'react-router-dom'
import { useFilters } from '../useFilters'
import { FilterBar } from './FilterBar'
import { FilterChips } from './FilterChips'
import { ActivityHistogram } from './ActivityHistogram'
import { EventList } from './EventList'

export function DashboardPage() {
  const { filters } = useFilters()
  const navigate = useNavigate()
  const location = useLocation()

  return (
    <main className="mx-auto max-w-6xl space-y-4 px-4 py-6">
      <FilterBar />
      <FilterChips />
      <section className="rounded-lg border border-zinc-800 bg-zinc-900/60 p-4">
        <ActivityHistogram />
      </section>
      <section className="overflow-hidden rounded-lg border border-zinc-800 bg-zinc-900/60">
        <EventList filters={filters} onSelect={(id) => navigate(`/events/${id}${location.search}`)} />
      </section>
    </main>
  )
}
```

- [ ] **Step 4: Run the full web suite** (DashboardPage is exercised by App tests)

Run: `npm test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/FilterChips.tsx web/src/components/FilterChips.test.tsx web/src/components/DashboardPage.tsx
git commit -m "feat(web): filter chips with clear-all, dashboard v2 layout"
```

---

### Task 8: ActivityHistogram v2 (auto bucket + bar-click zoom)

**Files:**
- Modify: `web/src/components/ActivityHistogram.tsx`, `web/src/components/ActivityHistogram.test.tsx`

**Interfaces:**
- Consumes: `pickBucket`/`bucketRange` (Task 5), `useFilters().setFilters` (Task 4), `useActivity` (existing).
- Produces: histogram that re-buckets with the active range and zooms `from`/`to` on bar click.

- [ ] **Step 1: Write/extend the failing test.** Read the existing `ActivityHistogram.test.tsx` first and keep its passing assertions. Add:

```tsx
// Recharts does not lay out in jsdom, so bar-click zoom is covered by the pure
// bucketRange test (timeRange.test.ts). Here we assert bucket auto-selection
// reaches the API query string.
test('uses minute buckets for a <=2h range', async () => {
  const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, json: () => Promise.resolve([]) })
  vi.stubGlobal('fetch', fetchMock)
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const from = new Date(Date.now() - 30 * 60_000).toISOString()
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[`/?from=${encodeURIComponent(from)}`]}>
        <ActivityHistogram />
      </MemoryRouter>
    </QueryClientProvider>,
  )
  await waitFor(() => {
    const url = fetchMock.mock.calls.map((c) => String(c[0])).find((u) => u.includes('/api/activity'))
    expect(url).toContain('bucket=minute')
  })
})
```

(Adjust imports at the top of the test file to include `vi`, `waitFor`, `QueryClient`, `QueryClientProvider`, `MemoryRouter` as needed to match the file's existing style.)

- [ ] **Step 2: Run test to verify it fails**

Run: `npm test -- src/components/ActivityHistogram.test.tsx`
Expected: FAIL — url contains `bucket=hour` (hard-coded).

- [ ] **Step 3: Implement** — replace `web/src/components/ActivityHistogram.tsx`:

```tsx
import { Bar, BarChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { useActivity } from '../api/hooks'
import { useFilters } from '../useFilters'
import { pickBucket, bucketRange } from '../timeRange'

export function ActivityHistogram() {
  const { filters, setFilters } = useFilters()
  const bucket = pickBucket(filters.from, filters.to)
  const activity = useActivity(filters, bucket)

  if (activity.isLoading) return <div className="py-8 text-center text-sm text-zinc-500">Loading activity…</div>
  if (activity.isError) return <div className="py-8 text-center text-sm text-zinc-400">Failed to load activity.</div>

  const data = (activity.data ?? []).map((b) => ({
    iso: b.bucket_start,
    label: new Date(b.bucket_start).toLocaleString(),
    count: b.count,
  }))
  if (data.length === 0) return <div className="py-8 text-center text-sm text-zinc-500">No activity in this window.</div>

  // Bar click zooms the time range to that bucket.
  const onBarClick = (entry: unknown) => {
    const e = entry as { iso?: string; payload?: { iso?: string } }
    const iso = e?.iso ?? e?.payload?.iso
    if (iso) setFilters(bucketRange(iso, bucket))
  }

  return (
    <div aria-label="activity histogram" className="h-40 w-full">
      <ResponsiveContainer>
        <BarChart data={data}>
          <XAxis dataKey="label" hide />
          <YAxis allowDecimals={false} width={30} stroke="#52525b" tick={{ fill: '#a1a1aa', fontSize: 11 }} />
          <Tooltip
            cursor={{ fill: 'rgba(255,255,255,0.05)' }}
            contentStyle={{ background: '#18181b', border: '1px solid #3f3f46', borderRadius: 6, color: '#e4e4e7' }}
          />
          <Bar dataKey="count" fill="#0284c7" radius={[2, 2, 0, 0]} onClick={onBarClick} cursor="pointer" />
        </BarChart>
      </ResponsiveContainer>
    </div>
  )
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `npm test -- src/components/ActivityHistogram.test.tsx`
Expected: PASS. If a pre-existing assertion targeted the old `hist-loading`/`hist-empty` class names, update it to match the new text-based assertions.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/ActivityHistogram.tsx web/src/components/ActivityHistogram.test.tsx
git commit -m "feat(web): histogram auto-buckets by range and zooms on bar click"
```

---

### Task 9: EventList v2 (styled table, badges, relative time)

**Files:**
- Create: `web/src/components/OperationBadge.tsx`, `web/src/time.ts`, `web/src/time.test.ts`, `web/src/components/EventList.test.tsx`
- Modify: `web/src/components/EventList.tsx`, `web/src/App.routing.test.tsx` (row-locator only)

**Interfaces:**
- Consumes: `useEventsFeed`/`useLiveFeed` (existing, unchanged).
- Produces: `OperationBadge({ op: string })` and `timeAgo(iso: string, now?: Date): string` — Task 11's detail page reuses `OperationBadge`. The resource cell renders `{namespace}/{name}` in ONE text node (tests locate rows by e.g. `default/web`).

- [ ] **Step 1: Write the failing tests.**

`web/src/time.test.ts`:

```ts
import { test, expect } from 'vitest'
import { timeAgo } from './time'

const now = new Date('2026-07-07T12:00:00Z')

test('timeAgo formats seconds/minutes/hours/days', () => {
  expect(timeAgo('2026-07-07T11:59:30Z', now)).toBe('30s ago')
  expect(timeAgo('2026-07-07T11:45:00Z', now)).toBe('15m ago')
  expect(timeAgo('2026-07-07T09:00:00Z', now)).toBe('3h ago')
  expect(timeAgo('2026-07-01T12:00:00Z', now)).toBe('6d ago')
  expect(timeAgo('2026-07-07T12:00:05Z', now)).toBe('0s ago') // clock skew clamps to 0
})
```

`web/src/components/EventList.test.tsx`:

```tsx
import { afterEach, test, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
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
    <QueryClientProvider client={qc}>
      <EventList filters={{}} onSelect={onSelect} />
    </QueryClientProvider>,
  )
  return onSelect
}

test('renders operation badge, resource, dry-run marker', async () => {
  renderList()
  expect(await screen.findByText('DELETE')).toBeInTheDocument()
  expect(screen.getByText('default/web')).toBeInTheDocument()
  expect(screen.getByText('dry-run')).toBeInTheDocument()
  expect(screen.getByText('alice')).toBeInTheDocument()
})

test('row activates with keyboard', async () => {
  const onSelect = renderList()
  const btn = await screen.findByRole('button', { name: /default\/web/ })
  btn.focus()
  await userEvent.keyboard('{Enter}')
  expect(onSelect).toHaveBeenCalledWith('1')
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `npm test -- src/time.test.ts src/components/EventList.test.tsx`
Expected: FAIL — `time.ts` missing; `default/web` not found (old markup renders `default/Deployment/web`).

- [ ] **Step 3: Implement.**

`web/src/time.ts` (new):

```ts
// timeAgo renders a compact relative timestamp for feed rows.
export function timeAgo(iso: string, now: Date = new Date()): string {
  const s = Math.max(0, Math.floor((now.getTime() - new Date(iso).getTime()) / 1000))
  if (s < 60) return `${s}s ago`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  return `${Math.floor(h / 24)}d ago`
}
```

`web/src/components/OperationBadge.tsx` (new):

```tsx
const STYLES: Record<string, string> = {
  CREATE: 'bg-emerald-950 text-emerald-300 ring-emerald-800',
  UPDATE: 'bg-amber-950 text-amber-300 ring-amber-800',
  DELETE: 'bg-red-950 text-red-300 ring-red-800',
}

export function OperationBadge({ op }: { op: string }) {
  const cls = STYLES[op] ?? 'bg-zinc-800 text-zinc-300 ring-zinc-700'
  return (
    <span className={`inline-block rounded px-1.5 py-0.5 text-xs font-semibold ring-1 ${cls}`}>
      {op}
    </span>
  )
}
```

`web/src/components/EventList.tsx` — full replacement:

```tsx
import { useEventsFeed } from '../api/hooks'
import { useLiveFeed } from '../useLiveFeed'
import { timeAgo } from '../time'
import { OperationBadge } from './OperationBadge'
import type { Filters, Row } from '../types'

function summarizeDiff(diff: string): string {
  try {
    const changes = JSON.parse(diff) as unknown[]
    if (!Array.isArray(changes) || changes.length === 0) return ''
    return changes.length === 1 ? '1 change' : `${changes.length} changes`
  } catch {
    return ''
  }
}

const thCls = 'px-3 py-2 text-left text-xs font-semibold uppercase tracking-wide text-zinc-500'

export function EventList({ filters, onSelect }: { filters: Filters; onSelect: (id: string) => void }) {
  const feed = useEventsFeed(filters)
  useLiveFeed(filters)

  if (feed.isLoading) return <div className="py-10 text-center text-sm text-zinc-500">Loading changes…</div>
  if (feed.isError) {
    return (
      <div className="py-10 text-center text-sm text-zinc-400">
        Failed to load changes.{' '}
        <button className="text-sky-400 underline" onClick={() => feed.refetch()}>
          Retry
        </button>
      </div>
    )
  }

  const rows: Row[] = feed.data?.pages.flatMap((p) => p.events) ?? []
  if (rows.length === 0) {
    return <div className="py-10 text-center text-sm text-zinc-500">No changes match these filters.</div>
  }

  return (
    <div>
      <table className="w-full text-sm">
        <thead className="border-b border-zinc-800">
          <tr>
            <th className={thCls}>Time</th>
            <th className={thCls}>Operation</th>
            <th className={thCls}>Resource</th>
            <th className={thCls}>User</th>
            <th className={thCls}>Cluster</th>
            <th className={thCls}>Changes</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr
              key={r.event_id}
              onClick={() => onSelect(r.event_id)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault()
                  onSelect(r.event_id)
                }
              }}
              tabIndex={0}
              role="button"
              className="cursor-pointer border-b border-zinc-800/60 hover:bg-zinc-800/40 focus:bg-zinc-800/40 focus:outline-none"
            >
              <td className="whitespace-nowrap px-3 py-2 text-zinc-400" title={new Date(r.event_time).toISOString()}>
                {timeAgo(r.event_time)}
              </td>
              <td className="whitespace-nowrap px-3 py-2">
                <OperationBadge op={r.operation} />
                {r.dry_run && (
                  <span className="ml-1.5 rounded border border-zinc-600 px-1 py-0.5 text-xs text-zinc-400">dry-run</span>
                )}
              </td>
              <td className="px-3 py-2 font-mono text-xs">
                <span className="text-zinc-500">{r.kind}</span>{' '}
                <span className="text-zinc-100">{r.namespace ? `${r.namespace}/${r.name}` : r.name}</span>
              </td>
              <td className="whitespace-nowrap px-3 py-2 text-zinc-300">{r.user_name}</td>
              <td className="whitespace-nowrap px-3 py-2 text-zinc-400">{r.cluster}</td>
              <td className="whitespace-nowrap px-3 py-2 text-zinc-500">{summarizeDiff(r.diff)}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="flex items-center justify-between px-3 py-2 text-xs text-zinc-500">
        <span>{rows.length} events loaded</span>
        {feed.hasNextPage && (
          <button
            className="rounded border border-zinc-700 px-2 py-1 text-zinc-300 hover:bg-zinc-800"
            onClick={() => feed.fetchNextPage()}
            disabled={feed.isFetchingNextPage}
          >
            {feed.isFetchingNextPage ? 'Loading…' : 'Load older'}
          </button>
        )}
      </div>
    </div>
  )
}
```

`web/src/App.routing.test.tsx` — the row locator changes: replace `await screen.findByText('default/Deployment/web')` with `await screen.findByText('default/web')` (leave the rest of the test as-is for now; Task 11 rewrites its detail assertion).

- [ ] **Step 4: Run the web suite**

Run: `npm test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/time.ts web/src/time.test.ts web/src/components/OperationBadge.tsx web/src/components/EventList.tsx web/src/components/EventList.test.tsx web/src/App.routing.test.tsx
git commit -m "feat(web): styled event table with badges and relative time"
```

---

### Task 10: Unified diff builder + DiffView renderer

**Files:**
- Create: `web/src/unifiedDiff.ts`, `web/src/unifiedDiff.test.ts`, `web/src/components/DiffView.tsx`, `web/src/components/DiffView.test.tsx`

**Interfaces:**
- Consumes: `diffLines` from the `diff` package.
- Produces (used by Task 11):

```ts
export type LineType = 'add' | 'del' | 'ctx'
export interface DiffLine { type: LineType; text: string }
export type Segment =
  | { kind: 'lines'; lines: DiffLine[] }
  | { kind: 'fold'; lines: DiffLine[] } // hidden ctx lines behind an expander
export function buildUnifiedDiff(oldJson: string, newJson: string): Segment[]
```

and `<DiffView oldJson={string} newJson={string} />`.

- [ ] **Step 1: Write the failing tests.**

`web/src/unifiedDiff.test.ts`:

```ts
import { test, expect } from 'vitest'
import { buildUnifiedDiff, type Segment } from './unifiedDiff'

function flat(segments: Segment[]): string[] {
  return segments.flatMap((s) => s.lines.map((l) => `${s.kind === 'fold' ? 'F' : l.type[0]}:${l.text}`))
}

test('replace produces del+add between context', () => {
  const segs = buildUnifiedDiff('{"a":1}', '{"a":2}')
  expect(flat(segs)).toEqual(['c:{', 'd:  "a": 1', 'a:  "a": 2', 'c:}'])
})

test('CREATE (empty old) is all additions', () => {
  const segs = buildUnifiedDiff('', '{"a":1}')
  const lines = flat(segs)
  expect(lines.length).toBeGreaterThan(0)
  expect(lines.every((l) => l.startsWith('a:'))).toBe(true)
})

test('DELETE (empty new) is all removals', () => {
  const segs = buildUnifiedDiff('{"a":1}', '')
  expect(flat(segs).every((l) => l.startsWith('d:'))).toBe(true)
})

test('long unchanged runs fold to 3 lines of context', () => {
  const items = Array.from({ length: 30 }, (_, i) => i)
  const oldObj = JSON.stringify({ items, x: 1 })
  const newObj = JSON.stringify({ items, x: 2 })
  const segs = buildUnifiedDiff(oldObj, newObj)
  const fold = segs.find((s) => s.kind === 'fold')
  expect(fold).toBeDefined()
  expect(fold!.lines.length).toBeGreaterThan(0)
  expect(fold!.lines.every((l) => l.type === 'ctx')).toBe(true)
})

test('malformed JSON falls back to raw text diff', () => {
  const segs = buildUnifiedDiff('not json {', 'not json }')
  const lines = flat(segs)
  expect(lines).toContain('d:not json {')
  expect(lines).toContain('a:not json }')
})
```

`web/src/components/DiffView.test.tsx`:

```tsx
import { test, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { DiffView } from './DiffView'

const items = Array.from({ length: 30 }, (_, i) => i)
const oldObj = JSON.stringify({ items, x: 1 })
const newObj = JSON.stringify({ items, x: 2 })

test('renders changed lines and a fold expander; expanding reveals lines', async () => {
  render(<DiffView oldJson={oldObj} newJson={newObj} />)
  expect(screen.getByText(/"x": 1/)).toBeInTheDocument()
  expect(screen.getByText(/"x": 2/)).toBeInTheDocument()
  const expander = screen.getByRole('button', { name: /unchanged lines/ })
  const before = document.querySelectorAll('[data-line]').length
  await userEvent.click(expander)
  expect(document.querySelectorAll('[data-line]').length).toBeGreaterThan(before)
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `npm test -- src/unifiedDiff.test.ts src/components/DiffView.test.tsx`
Expected: FAIL — modules not found.

- [ ] **Step 3: Implement.**

`web/src/unifiedDiff.ts` (new):

```ts
import { diffLines } from 'diff'

export type LineType = 'add' | 'del' | 'ctx'

export interface DiffLine {
  type: LineType
  text: string
}

export type Segment =
  | { kind: 'lines'; lines: DiffLine[] }
  | { kind: 'fold'; lines: DiffLine[] }

const CONTEXT = 3 // visible ctx lines kept adjacent to a change
const FOLD_THRESHOLD = 6 // ctx runs longer than this get folded

function pretty(json: string): string {
  if (!json) return ''
  try {
    return JSON.stringify(JSON.parse(json), null, 2)
  } catch {
    return json // malformed payload: diff the raw text
  }
}

function toLines(value: string, type: LineType): DiffLine[] {
  const body = value.endsWith('\n') ? value.slice(0, -1) : value
  if (body === '') return []
  return body.split('\n').map((text) => ({ type, text }))
}

function pushLines(segments: Segment[], lines: DiffLine[]): void {
  if (lines.length === 0) return
  const last = segments[segments.length - 1]
  if (last && last.kind === 'lines') last.lines.push(...lines)
  else segments.push({ kind: 'lines', lines: [...lines] })
}

// buildUnifiedDiff pretty-prints both JSON payloads, line-diffs them, and folds
// long unchanged runs behind expander segments (3 lines of context kept at each
// edge that touches a change; document edges keep none).
export function buildUnifiedDiff(oldJson: string, newJson: string): Segment[] {
  const parts = diffLines(pretty(oldJson), pretty(newJson))
  const all: DiffLine[] = []
  for (const p of parts) {
    all.push(...toLines(p.value, p.added ? 'add' : p.removed ? 'del' : 'ctx'))
  }

  const segments: Segment[] = []
  let i = 0
  while (i < all.length) {
    const type = all[i].type
    let j = i
    while (j < all.length && all[j].type === type) j++
    const run = all.slice(i, j)
    if (type !== 'ctx' || run.length <= FOLD_THRESHOLD) {
      pushLines(segments, run)
    } else {
      const head = i === 0 ? 0 : CONTEXT
      const tail = j === all.length ? 0 : CONTEXT
      pushLines(segments, run.slice(0, head))
      segments.push({ kind: 'fold', lines: run.slice(head, run.length - tail) })
      pushLines(segments, run.slice(run.length - tail))
    }
    i = j
  }
  return segments
}
```

`web/src/components/DiffView.tsx` (new):

```tsx
import { useMemo, useState } from 'react'
import { buildUnifiedDiff } from '../unifiedDiff'

const LINE_CLS = {
  add: 'bg-emerald-950/50 text-emerald-200',
  del: 'bg-red-950/50 text-red-300',
  ctx: 'text-zinc-400',
} as const

const MARKER = { add: '+', del: '-', ctx: ' ' } as const

export function DiffView({ oldJson, newJson }: { oldJson: string; newJson: string }) {
  const segments = useMemo(() => buildUnifiedDiff(oldJson, newJson), [oldJson, newJson])
  const [expanded, setExpanded] = useState<ReadonlySet<number>>(new Set())
  const expand = (i: number) => setExpanded((prev) => new Set(prev).add(i))

  if (segments.length === 0) {
    return <div className="py-6 text-center text-sm text-zinc-500">No content to diff.</div>
  }

  return (
    <div className="overflow-x-auto rounded-lg border border-zinc-800 bg-zinc-950 py-1 font-mono text-xs leading-5">
      {segments.map((s, i) =>
        s.kind === 'fold' && !expanded.has(i) ? (
          <button
            key={i}
            onClick={() => expand(i)}
            className="block w-full bg-zinc-900/80 px-3 py-1 text-center text-zinc-500 hover:text-zinc-300"
          >
            ⋯ {s.lines.length} unchanged lines
          </button>
        ) : (
          <div key={i}>
            {s.lines.map((l, j) => (
              <div key={j} data-line className={`whitespace-pre px-3 ${LINE_CLS[l.type]}`}>
                {MARKER[l.type]} {l.text}
              </div>
            ))}
          </div>
        ),
      )}
    </div>
  )
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `npm test -- src/unifiedDiff.test.ts src/components/DiffView.test.tsx`
Expected: PASS (6 tests).

- [ ] **Step 5: Commit**

```bash
git add web/src/unifiedDiff.ts web/src/unifiedDiff.test.ts web/src/components/DiffView.tsx web/src/components/DiffView.test.tsx
git commit -m "feat(web): unified diff builder with context folding and renderer"
```

---

### Task 11: Event detail page (replaces drawer)

**Files:**
- Modify: `web/src/components/EventDetail.tsx` (rewrite as `EventDetailPage`), `web/src/App.tsx`, `web/src/components/EventDetail.test.tsx`, `web/src/App.routing.test.tsx`
- Delete: `web/src/components/Drawer.tsx`, `web/src/components/Drawer.test.tsx`, `web/src/diff.ts`, `web/src/diff.test.ts`

**Interfaces:**
- Consumes: `useEvent` (existing), `DiffView` (Task 10), `OperationBadge` (Task 9).
- Produces: `EventDetailPage` (no props; reads `:id` via `useParams`) exported from `web/src/components/EventDetail.tsx`. Route contract: `/events/:id` renders ONLY the detail page; every other path renders the dashboard; breadcrumb link returns to `/` + preserved query string.

- [ ] **Step 1: Write the failing tests.**

Replace `web/src/components/EventDetail.test.tsx`:

```tsx
import { afterEach, test, expect, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { EventDetailPage } from './EventDetail'

afterEach(() => vi.unstubAllGlobals())

const detail = {
  event_id: '1', event_time: '2026-07-01T10:00:00Z', ingested_at: '2026-07-01T10:00:01Z',
  cluster: 'c1', source: 'webhook', operation: 'UPDATE', api_group: 'apps', api_version: 'v1',
  kind: 'Deployment', namespace: 'default', name: 'web', resource_uid: 'u', sub_resource: '',
  user_name: 'alice', user_groups: ['system:masters'], dry_run: false,
  diff: '[{"path":"spec.replicas","op":"replace","old":2,"new":3}]',
  old_object: '{"spec":{"replicas":2}}', new_object: '{"spec":{"replicas":3}}',
  user_uid: 'uid', user_agent: 'kubectl/v1.30',
}

function renderPage() {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, json: () => Promise.resolve(detail) }))
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/events/1?cluster=c1']}>
        <Routes>
          <Route path="/events/:id" element={<EventDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

test('renders metadata and a unified diff', async () => {
  renderPage()
  await waitFor(() => expect(screen.getByText('alice')).toBeInTheDocument())
  expect(screen.getByText('UPDATE')).toBeInTheDocument()
  expect(screen.getByText(/default\/web/)).toBeInTheDocument()
  expect(screen.getByText(/"replicas": 2/)).toBeInTheDocument() // del line
  expect(screen.getByText(/"replicas": 3/)).toBeInTheDocument() // add line
})

test('breadcrumb preserves the query string', async () => {
  renderPage()
  await screen.findByText('alice')
  const back = screen.getByRole('link', { name: /events/i })
  expect(back).toHaveAttribute('href', '/?cluster=c1')
})
```

Replace `web/src/App.routing.test.tsx` (same fetch stub, new assertions — the detail is now a full page):

```tsx
import { afterEach, test, expect, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from './App'

afterEach(() => vi.unstubAllGlobals())

function stubApi() {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string) => {
      let body: unknown = {}
      if (url.includes('/api/facets')) {
        body = { clusters: ['c1'], namespaces: ['default'], kinds: ['Deployment'], operations: ['UPDATE'] }
      } else if (url.includes('/api/activity')) body = []
      else if (/\/api\/events\/[^?]/.test(url)) {
        body = {
          event_id: '1', event_time: '2026-07-01T10:00:00Z', ingested_at: '2026-07-01T10:00:01Z',
          cluster: 'c1', source: 'webhook', operation: 'UPDATE', api_group: 'apps', api_version: 'v1',
          kind: 'Deployment', namespace: 'default', name: 'web', resource_uid: 'u', sub_resource: '',
          user_name: 'alice', user_groups: [], dry_run: false,
          diff: '[{"path":"spec.replicas","op":"replace","old":2,"new":3}]',
          old_object: '{"spec":{"replicas":2}}', new_object: '{"spec":{"replicas":3}}',
          user_uid: '', user_agent: '',
        }
      } else {
        body = {
          events: [{
            event_id: '1', event_time: '2026-07-01T10:00:00Z', ingested_at: '2026-07-01T10:00:01Z',
            cluster: 'c1', source: 'webhook', operation: 'UPDATE', api_group: 'apps', api_version: 'v1',
            kind: 'Deployment', namespace: 'default', name: 'web', resource_uid: 'u', sub_resource: '',
            user_name: 'alice', user_groups: [], dry_run: false, diff: '[]',
          }],
          next_cursor: '',
        }
      }
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) })
    }),
  )
}

test('clicking a feed row opens the full detail page with a unified diff', async () => {
  stubApi()
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/']}>
        <App />
      </MemoryRouter>
    </QueryClientProvider>,
  )

  const row = await screen.findByText('default/web')
  await userEvent.click(row)
  await waitFor(() => expect(screen.getByText(/"replicas": 3/)).toBeInTheDocument())
  // dashboard content is replaced by the page
  expect(screen.queryByLabelText('search')).not.toBeInTheDocument()
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `npm test -- src/components/EventDetail.test.tsx src/App.routing.test.tsx`
Expected: FAIL — `EventDetailPage` not exported.

- [ ] **Step 3: Implement.**

`web/src/components/EventDetail.tsx` — full replacement:

```tsx
import { Link, useLocation, useParams } from 'react-router-dom'
import { useEvent } from '../api/hooks'
import { OperationBadge } from './OperationBadge'
import { DiffView } from './DiffView'

function Meta({ label, value }: { label: string; value: string }) {
  if (!value) return null
  return (
    <div>
      <dt className="text-xs uppercase tracking-wide text-zinc-500">{label}</dt>
      <dd className="truncate text-zinc-200" title={value}>{value}</dd>
    </div>
  )
}

function CopyButton({ label, text }: { label: string; text: string }) {
  return (
    <button
      className="rounded border border-zinc-700 px-2 py-1 text-xs text-zinc-300 hover:bg-zinc-800"
      onClick={() => navigator.clipboard?.writeText(text)}
    >
      {label}
    </button>
  )
}

export function EventDetailPage() {
  const { id } = useParams()
  const location = useLocation()
  const { data, isLoading, isError, refetch } = useEvent(id)

  return (
    <main className="mx-auto max-w-6xl px-4 py-6">
      <Link to={`/${location.search}`} className="text-sm text-sky-400 hover:text-sky-300">
        ← Events
      </Link>

      {isLoading && <div className="py-10 text-center text-sm text-zinc-500">Loading…</div>}
      {!isLoading && (isError || !data) && (
        <div className="py-10 text-center text-sm text-zinc-400">
          Failed to load event.{' '}
          <button className="text-sky-400 underline" onClick={() => refetch()}>
            Retry
          </button>
        </div>
      )}

      {data && (
        <>
          <header className="mt-4 flex flex-wrap items-center gap-3">
            <OperationBadge op={data.operation} />
            <h1 className="font-mono text-lg text-zinc-100">
              <span className="text-zinc-500">{data.kind}</span>{' '}
              {data.namespace ? `${data.namespace}/${data.name}` : data.name}
            </h1>
            {data.dry_run && (
              <span className="rounded border border-zinc-600 px-1.5 py-0.5 text-xs text-zinc-400">dry-run</span>
            )}
          </header>

          <dl className="mt-4 grid grid-cols-2 gap-x-8 gap-y-3 rounded-lg border border-zinc-800 bg-zinc-900/60 p-4 text-sm sm:grid-cols-3">
            <Meta label="cluster" value={data.cluster} />
            <Meta label="user" value={data.user_name} />
            <Meta label="groups" value={data.user_groups.join(', ')} />
            <Meta label="time" value={new Date(data.event_time).toLocaleString()} />
            <Meta label="api" value={`${data.api_group || 'core'}/${data.api_version}`} />
            <Meta label="sub-resource" value={data.sub_resource} />
            <Meta label="user agent" value={data.user_agent} />
            <Meta label="source" value={data.source} />
            <Meta label="event id" value={data.event_id} />
          </dl>

          <div className="mt-5 flex items-center gap-2">
            <h2 className="text-sm font-semibold text-zinc-300">Change</h2>
            <span className="flex-1" />
            <CopyButton label="Copy before" text={data.old_object} />
            <CopyButton label="Copy after" text={data.new_object} />
          </div>
          <div className="mt-2">
            <DiffView oldJson={data.old_object} newJson={data.new_object} />
          </div>
        </>
      )}
    </main>
  )
}
```

`web/src/App.tsx` — full replacement:

```tsx
import { Routes, Route } from 'react-router-dom'
import { DashboardPage } from './components/DashboardPage'
import { EventDetailPage } from './components/EventDetail'
import { Header } from './components/Header'

export function App() {
  return (
    <div className="min-h-screen">
      <Header />
      <Routes>
        <Route path="/events/:id" element={<EventDetailPage />} />
        <Route path="*" element={<DashboardPage />} />
      </Routes>
    </div>
  )
}
```

Delete the now-unused files:

```bash
git rm web/src/components/Drawer.tsx web/src/components/Drawer.test.tsx web/src/diff.ts web/src/diff.test.ts
```

Also remove the `DiffChange` interface from `web/src/types.ts` (its only consumers were `diff.ts` and the old EventDetail).

- [ ] **Step 4: Run the full suite and build**

Run: `npm test && npm run build`
Expected: all tests PASS, build clean, no TS errors about deleted modules.

- [ ] **Step 5: Commit**

```bash
git add -A web/src
git commit -m "feat(web): full event detail page with unified diff, drop drawer"
```

---

### Task 12: Final verification

**Files:** none (verification only)

- [ ] **Step 1: Full frontend gate**

Run: `cd web && npm test && npm run build`
Expected: every suite PASS; production build emits hashed assets.

- [ ] **Step 2: Full backend gate**

Run: `cd /Users/gauravrautela/GolandProjects/KubeWatch && go vet ./... && go test ./...`
Expected: PASS across all packages.

- [ ] **Step 3: Manual smoke (if the hub stack is reachable)**

Follow the repo's `verifying-services-end-to-end` practice: run the dashboard API (`cmd/dashboard`) against ClickHouse, `npm run dev` in `web/`, and verify in a browser: facets populate all selects; picking `Last 1h` filters the feed and re-buckets the histogram; the ignore select adds `not kind:` chips and removes those rows; clicking a feed row opens `/events/:id` with a red/green unified diff; the breadcrumb returns to the filtered list. If infra is unavailable, record this as an open merge gate in `.superpowers/sdd/spa-progress.md` alongside the existing ones.

- [ ] **Step 4: Final review**

Use superpowers:requesting-code-review over the whole branch range before merge.

---

## Self-Review (completed at plan time)

- **Spec coverage:** theme/shell → T3; q + excludes + namespaces facet → T1–T2; search/selects/time range/ignore → T6; chips + clear all → T7; histogram auto-bucket + bar-click zoom → T8; styled list/badges/load-more → T9; unified diff + folding → T10; full detail page + drawer removal → T11; testing gates → every task + T12. RFC3339 constraint encoded in T5/T6 code and Global Constraints.
- **Placeholder scan:** none — every code step carries complete code.
- **Type consistency:** `Segment`/`DiffLine`/`buildUnifiedDiff` (T10) match T11's `DiffView` usage; `setFilters`/`clearFilters`/`splitList`/`addToList`/`removeFromList` (T4) match T6–T8 imports; `presetFrom`/`pickBucket`/`bucketRange` (T5) match T6/T8; `Facets.Namespaces` (T2) matches `facets.data?.namespaces` (T6); `OperationBadge` (T9) matches T11.
