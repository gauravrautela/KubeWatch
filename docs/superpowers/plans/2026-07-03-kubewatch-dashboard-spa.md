# KubeWatch Dashboard SPA (Plan 2b of 2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the React + TypeScript SPA for the KubeWatch dashboard — live change feed, activity histogram, structured search/filter, structured-diff viewer with raw toggle, and per-resource timelines — consuming the Plan 2a JSON API, served in production via `SPA_DIR`.

**Architecture:** A Vite + React + TypeScript app in a new `web/` directory, fully decoupled from the Go code. URL query string is the single source of truth for filters and the selected event. TanStack Query owns data fetching/caching; the live feed reconciles keyset "load older" (infinite query) with `since`-polling "new rows" (prepend into the same cache). Frontend-only — no Go changes.

**Tech Stack:** Node 22 / npm 10, React 18, TypeScript 5, Vite 5, react-router-dom 6, @tanstack/react-query 5, recharts 2. Tests: Vitest 2 + @testing-library/react 16 + @testing-library/user-event 14 + jsdom.

**Scope note:** Plan 2b of 2. Consumes the Plan 2a API (already built). Produces a working, tested SPA. Live end-to-end against a running API is a manual pre-merge step (needs the Go dashboard + ClickHouse).

## Global Constraints

- All frontend code lives under `web/` — a separate npm project, NOT part of the Go module.
- API base path is `/api` (Vite dev-proxies it to `http://localhost:8081`; in prod the Go binary serves both).
- The API JSON shapes are fixed by Plan 2a — mirror them exactly in `web/src/types.ts`. Do NOT change any Go code.
- URL query string is the single source of truth for filters (`cluster, namespace, kind, name, user, operation, from, to`) and the selected event (`/events/:id`).
- The `diff` field on an event is a JSON **string** (a serialized array of `{path, op, old?, new?}`); parse it client-side.
- Cursors are opaque base64url-no-pad of `${event_time_unix_nano}:${event_id}` — matching Plan 2a's `EncodeCursor`.
- Test framework: Vitest + React Testing Library only.
- TypeScript `strict` mode on.

---

### Task 1: Scaffold the web/ project

**Files:**
- Create: `web/package.json`, `web/vite.config.ts`, `web/tsconfig.json`, `web/index.html`, `web/src/main.tsx`, `web/src/App.tsx`, `web/src/vitest.setup.ts`, `web/src/App.test.tsx`
- Modify: `.gitignore` (root)

**Interfaces:**
- Consumes: nothing
- Produces: `App` (React component) from `web/src/App.tsx`; a working `npm test` / `npm run build`.

- [ ] **Step 1: Create package.json**

Create `web/package.json`:
```json
{
  "name": "kubewatch-web",
  "private": true,
  "version": "0.1.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc && vite build",
    "preview": "vite preview",
    "test": "vitest run",
    "test:watch": "vitest"
  },
  "dependencies": {
    "@tanstack/react-query": "^5.59.0",
    "react": "^18.3.1",
    "react-dom": "^18.3.1",
    "react-router-dom": "^6.27.0",
    "recharts": "^2.13.0"
  },
  "devDependencies": {
    "@testing-library/jest-dom": "^6.6.0",
    "@testing-library/react": "^16.0.1",
    "@testing-library/user-event": "^14.5.2",
    "@types/react": "^18.3.11",
    "@types/react-dom": "^18.3.1",
    "@vitejs/plugin-react": "^4.3.3",
    "jsdom": "^25.0.1",
    "typescript": "^5.6.3",
    "vite": "^5.4.9",
    "vitest": "^2.1.3"
  }
}
```

- [ ] **Step 2: Create the Vite + Vitest config**

Create `web/vite.config.ts`:
```ts
/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
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

- [ ] **Step 3: Create tsconfig.json**

Create `web/tsconfig.json`:
```json
{
  "compilerOptions": {
    "target": "ES2020",
    "useDefineForClassFields": true,
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "resolveJsonModule": true,
    "isolatedModules": true,
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "types": ["vitest/globals", "@testing-library/jest-dom"]
  },
  "include": ["src"]
}
```

- [ ] **Step 4: Create index.html, entry, and App**

Create `web/index.html`:
```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>KubeWatch</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

Create `web/src/main.tsx`:
```tsx
import React from 'react'
import ReactDOM from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import { App } from './App'

const queryClient = new QueryClient()

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </QueryClientProvider>
  </React.StrictMode>,
)
```

Create `web/src/App.tsx` (minimal now; expanded in Task 8):
```tsx
export function App() {
  return <div>KubeWatch Dashboard</div>
}
```

Create `web/src/vitest.setup.ts`:
```ts
import '@testing-library/jest-dom'
```

- [ ] **Step 5: Write the smoke test**

Create `web/src/App.test.tsx`:
```tsx
import { render, screen } from '@testing-library/react'
import { test, expect } from 'vitest'
import { App } from './App'

test('renders the dashboard title', () => {
  render(<App />)
  expect(screen.getByText('KubeWatch Dashboard')).toBeInTheDocument()
})
```

- [ ] **Step 6: Update root .gitignore**

Append to `.gitignore`:
```gitignore
web/node_modules/
web/dist/
```

- [ ] **Step 7: Install, test, build**

Run:
```bash
cd web && npm install && npm test && npm run build
```
Expected: `npm test` → 1 passed; `npm run build` produces `web/dist/index.html`.

- [ ] **Step 8: Commit**

```bash
git add web/package.json web/package-lock.json web/vite.config.ts web/tsconfig.json web/index.html web/src/ .gitignore
git commit -m "chore: scaffold web SPA (vite + react + ts + vitest)"
```

---

### Task 2: API types, cursor codec, and fetch client

**Files:**
- Create: `web/src/types.ts`, `web/src/api/cursor.ts`, `web/src/api/client.ts`
- Test: `web/src/api/cursor.test.ts`, `web/src/api/client.test.ts`

**Interfaces:**
- Consumes: nothing (mirrors the Plan 2a API JSON)
- Produces:
  - `types.ts`: `Row`, `Detail`, `Page`, `Bucket`, `Facets`, `Filters`, `DiffChange`, `Operation`
  - `cursor.ts`: `encodeCursor(row: Pick<Row,'event_time'|'event_id'>): string`
  - `client.ts`: `ApiError`, `FeedOpts`, `fetchEvents(filters, opts?)`, `fetchEvent(id)`, `fetchActivity(filters, bucket)`, `fetchFacets()`

- [ ] **Step 1: Create the types**

Create `web/src/types.ts`:
```ts
export type Operation = 'CREATE' | 'UPDATE' | 'DELETE'

export interface Row {
  event_id: string
  event_time: string
  ingested_at: string
  cluster: string
  source: string
  operation: Operation
  api_group: string
  api_version: string
  kind: string
  namespace: string
  name: string
  resource_uid: string
  sub_resource: string
  user_name: string
  user_groups: string[]
  dry_run: boolean
  diff: string
}

export interface Detail extends Row {
  old_object: string
  new_object: string
  user_uid: string
  user_agent: string
}

export interface Page {
  events: Row[]
  next_cursor: string
}

export interface Bucket {
  bucket_start: string
  count: number
}

export interface Facets {
  clusters: string[]
  kinds: string[]
  operations: string[]
}

export interface Filters {
  cluster?: string
  namespace?: string
  kind?: string
  name?: string
  user?: string
  operation?: string
  from?: string
  to?: string
}

export interface DiffChange {
  path: string
  op: 'add' | 'remove' | 'replace'
  old?: unknown
  new?: unknown
}
```

- [ ] **Step 2: Write the failing cursor test**

Create `web/src/api/cursor.test.ts`:
```ts
import { test, expect } from 'vitest'
import { encodeCursor } from './cursor'

// Decodes the base64url-no-pad cursor back to "ns:uuid" for verification.
function decode(cur: string): string {
  const b64 = cur.replace(/-/g, '+').replace(/_/g, '/')
  return atob(b64)
}

test('encodeCursor renders base64url-no-pad of "<unixNanos>:<uuid>"', () => {
  const cur = encodeCursor({ event_time: '2026-07-01T10:00:00.000Z', event_id: 'abc-123' })
  expect(cur).not.toMatch(/[+/=]/) // url-safe, no padding
  const ms = new Date('2026-07-01T10:00:00.000Z').getTime()
  expect(decode(cur)).toBe(`${BigInt(ms) * 1_000_000n}:abc-123`)
})
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd web && npx vitest run src/api/cursor.test.ts`
Expected: FAIL — cannot resolve `./cursor`.

- [ ] **Step 4: Implement the cursor codec**

Create `web/src/api/cursor.ts`:
```ts
import type { Row } from '../types'

// encodeCursor reproduces the server's EncodeCursor: base64url-no-pad of
// "<event_time as unix nanoseconds>:<event_id>". event_time is DateTime64(3)
// (millisecond precision), which a JS Date represents exactly.
export function encodeCursor(row: Pick<Row, 'event_time' | 'event_id'>): string {
  const ms = new Date(row.event_time).getTime()
  const ns = BigInt(ms) * 1_000_000n
  const raw = `${ns}:${row.event_id}`
  return btoa(raw).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}
```

- [ ] **Step 5: Run cursor test to verify it passes**

Run: `cd web && npx vitest run src/api/cursor.test.ts`
Expected: PASS

- [ ] **Step 6: Write the failing client test**

Create `web/src/api/client.test.ts`:
```ts
import { afterEach, test, expect, vi } from 'vitest'
import { ApiError, fetchActivity, fetchEvents } from './client'

afterEach(() => vi.unstubAllGlobals())

function mockFetch(body: unknown, ok = true, status = 200) {
  const f = vi.fn().mockResolvedValue({ ok, status, json: () => Promise.resolve(body) })
  vi.stubGlobal('fetch', f)
  return f
}

test('fetchEvents builds URL with filters, cursor, and limit', async () => {
  const f = mockFetch({ events: [], next_cursor: '' })
  await fetchEvents({ cluster: 'c1', name: 'web' }, { cursor: 'abc', limit: 50 })
  const url = f.mock.calls[0][0] as string
  expect(url).toContain('/api/events?')
  expect(url).toContain('cluster=c1')
  expect(url).toContain('name=web')
  expect(url).toContain('cursor=abc')
  expect(url).toContain('limit=50')
})

test('fetchEvents omits empty filter values', async () => {
  const f = mockFetch({ events: [], next_cursor: '' })
  await fetchEvents({ cluster: '', name: 'web' })
  const url = f.mock.calls[0][0] as string
  expect(url).not.toContain('cluster=')
  expect(url).toContain('name=web')
})

test('fetchActivity sets the bucket param', async () => {
  const f = mockFetch([])
  await fetchActivity({}, 'hour')
  expect(f.mock.calls[0][0]).toContain('bucket=hour')
})

test('non-2xx throws ApiError carrying the server error message', async () => {
  mockFetch({ error: 'invalid cursor' }, false, 400)
  await expect(fetchEvents({})).rejects.toMatchObject({ status: 400, message: 'invalid cursor' })
  mockFetch({ error: 'x' }, false, 400)
  const err = await fetchEvents({}).catch((e) => e)
  expect(err).toBeInstanceOf(ApiError)
})
```

- [ ] **Step 7: Run to verify it fails**

Run: `cd web && npx vitest run src/api/client.test.ts`
Expected: FAIL — cannot resolve `./client`.

- [ ] **Step 8: Implement the client**

Create `web/src/api/client.ts`:
```ts
import type { Bucket, Detail, Facets, Filters, Page } from '../types'

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

function filtersToParams(f: Filters): URLSearchParams {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries(f)) {
    if (v) p.set(k, v)
  }
  return p
}

async function getJSON<T>(url: string): Promise<T> {
  const resp = await fetch(url)
  if (!resp.ok) {
    let msg = `request failed (${resp.status})`
    try {
      const body = await resp.json()
      if (body && typeof body.error === 'string') msg = body.error
    } catch {
      // non-JSON error body; keep default message
    }
    throw new ApiError(resp.status, msg)
  }
  return (await resp.json()) as T
}

export interface FeedOpts {
  cursor?: string
  since?: string
  limit?: number
}

export function fetchEvents(filters: Filters, opts: FeedOpts = {}): Promise<Page> {
  const p = filtersToParams(filters)
  if (opts.cursor) p.set('cursor', opts.cursor)
  if (opts.since) p.set('since', opts.since)
  if (opts.limit) p.set('limit', String(opts.limit))
  return getJSON<Page>(`/api/events?${p.toString()}`)
}

export function fetchEvent(id: string): Promise<Detail> {
  return getJSON<Detail>(`/api/events/${encodeURIComponent(id)}`)
}

export function fetchActivity(filters: Filters, bucket: string): Promise<Bucket[]> {
  const p = filtersToParams(filters)
  p.set('bucket', bucket)
  return getJSON<Bucket[]>(`/api/activity?${p.toString()}`)
}

export function fetchFacets(): Promise<Facets> {
  return getJSON<Facets>('/api/facets')
}
```

- [ ] **Step 9: Run all Task-2 tests to verify they pass**

Run: `cd web && npx vitest run src/api/`
Expected: PASS (cursor + client tests)

- [ ] **Step 10: Commit**

```bash
git add web/src/types.ts web/src/api/cursor.ts web/src/api/cursor.test.ts web/src/api/client.ts web/src/api/client.test.ts
git commit -m "feat(web): add API types, cursor codec, and fetch client"
```

---

### Task 3: TanStack Query hooks

**Files:**
- Create: `web/src/api/hooks.ts`
- Test: `web/src/api/hooks.test.tsx`

**Interfaces:**
- Consumes: `client.ts` (`fetchEvents`/`fetchEvent`/`fetchActivity`/`fetchFacets`), `types.ts`
- Produces: `useEventsFeed(filters)`, `useEvent(id?)`, `useActivity(filters, bucket)`, `useFacets()`, and `eventsKey(filters)` (the shared query key for the feed, used by the live poller in Task 5).

- [ ] **Step 1: Write the failing test**

Create `web/src/api/hooks.test.tsx`:
```tsx
import { afterEach, test, expect, vi } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useFacets, useEventsFeed, eventsKey } from './hooks'

afterEach(() => vi.unstubAllGlobals())

function wrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  )
}

function mockFetch(body: unknown) {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, json: () => Promise.resolve(body) }))
}

test('eventsKey is stable for equal filters', () => {
  expect(eventsKey({ cluster: 'c1' })).toEqual(eventsKey({ cluster: 'c1' }))
})

test('useFacets fetches facets', async () => {
  mockFetch({ clusters: ['c1'], kinds: [], operations: [] })
  const { result } = renderHook(() => useFacets(), { wrapper: wrapper() })
  await waitFor(() => expect(result.current.isSuccess).toBe(true))
  expect(result.current.data?.clusters).toEqual(['c1'])
})

test('useEventsFeed loads the first page', async () => {
  mockFetch({ events: [{ event_id: '1' }], next_cursor: '' })
  const { result } = renderHook(() => useEventsFeed({}), { wrapper: wrapper() })
  await waitFor(() => expect(result.current.isSuccess).toBe(true))
  expect(result.current.data?.pages[0].events[0].event_id).toBe('1')
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npx vitest run src/api/hooks.test.tsx`
Expected: FAIL — cannot resolve `./hooks`.

- [ ] **Step 3: Implement the hooks**

Create `web/src/api/hooks.ts`:
```ts
import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { fetchActivity, fetchEvent, fetchEvents, fetchFacets } from './client'
import type { Filters } from '../types'

const FEED_LIMIT = 50

// eventsKey is the shared query key for the feed. The live poller (Task 5) uses
// it to read and mutate the same cache entry.
export function eventsKey(filters: Filters) {
  return ['events', filters] as const
}

export function useEventsFeed(filters: Filters) {
  return useInfiniteQuery({
    queryKey: eventsKey(filters),
    queryFn: ({ pageParam }) =>
      fetchEvents(filters, { cursor: pageParam || undefined, limit: FEED_LIMIT }),
    initialPageParam: '',
    getNextPageParam: (lastPage) => lastPage.next_cursor || undefined,
  })
}

export function useEvent(id: string | undefined) {
  return useQuery({
    queryKey: ['event', id],
    queryFn: () => fetchEvent(id!),
    enabled: !!id,
  })
}

export function useActivity(filters: Filters, bucket: string) {
  return useQuery({
    queryKey: ['activity', filters, bucket],
    queryFn: () => fetchActivity(filters, bucket),
  })
}

export function useFacets() {
  return useQuery({ queryKey: ['facets'], queryFn: fetchFacets })
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd web && npx vitest run src/api/hooks.test.tsx`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/src/api/hooks.ts web/src/api/hooks.test.tsx
git commit -m "feat(web): add TanStack Query hooks for events, activity, facets"
```

---

### Task 4: URL filter state and FilterBar

**Files:**
- Create: `web/src/useFilters.ts`, `web/src/components/FilterBar.tsx`
- Test: `web/src/components/FilterBar.test.tsx`

**Interfaces:**
- Consumes: `useFacets` (Task 3), `Filters` (Task 2), `react-router-dom` `useSearchParams`
- Produces:
  - `useFilters(): { filters: Filters; setFilter(key: keyof Filters, value: string): void }` — reads/writes the URL query string
  - `FilterBar` component

- [ ] **Step 1: Write the failing test**

Create `web/src/components/FilterBar.test.tsx`:
```tsx
import { afterEach, test, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
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
      json: () => Promise.resolve({ clusters: ['c1', 'c2'], kinds: ['Deployment'], operations: ['UPDATE'] }),
    }),
  )
}

function Harness() {
  const [params] = useSearchParams()
  return (
    <>
      <FilterBar />
      <span data-testid="qs">{params.toString()}</span>
    </>
  )
}

function renderBar() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/']}>
        <Harness />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

test('typing a name writes it to the URL query string', async () => {
  mockFacets()
  renderBar()
  const input = screen.getByLabelText('name')
  await userEvent.type(input, 'web')
  expect(screen.getByTestId('qs').textContent).toContain('name=web')
})

test('selecting a cluster writes it to the URL', async () => {
  mockFacets()
  renderBar()
  await screen.findByRole('option', { name: 'c1' })
  await userEvent.selectOptions(screen.getByLabelText('cluster'), 'c1')
  expect(screen.getByTestId('qs').textContent).toContain('cluster=c1')
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npx vitest run src/components/FilterBar.test.tsx`
Expected: FAIL — cannot resolve `./FilterBar`.

- [ ] **Step 3: Implement useFilters**

Create `web/src/useFilters.ts`:
```ts
import { useSearchParams } from 'react-router-dom'
import type { Filters } from './types'

const FILTER_KEYS: (keyof Filters)[] = [
  'cluster', 'namespace', 'kind', 'name', 'user', 'operation', 'from', 'to',
]

export function useFilters() {
  const [params, setParams] = useSearchParams()

  const filters: Filters = {}
  for (const key of FILTER_KEYS) {
    const v = params.get(key)
    if (v) filters[key] = v
  }

  function setFilter(key: keyof Filters, value: string) {
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        if (value) next.set(key, value)
        else next.delete(key)
        return next
      },
      { replace: true },
    )
  }

  return { filters, setFilter }
}
```

- [ ] **Step 4: Implement FilterBar**

Create `web/src/components/FilterBar.tsx`:
```tsx
import { useFacets } from '../api/hooks'
import { useFilters } from '../useFilters'

export function FilterBar() {
  const { filters, setFilter } = useFilters()
  const facets = useFacets()

  const clusters = facets.data?.clusters ?? []
  const kinds = facets.data?.kinds ?? []
  const operations = facets.data?.operations ?? []

  return (
    <div className="filter-bar">
      <label>
        cluster
        <select
          aria-label="cluster"
          value={filters.cluster ?? ''}
          onChange={(e) => setFilter('cluster', e.target.value)}
        >
          <option value="">all</option>
          {clusters.map((c) => (
            <option key={c} value={c}>{c}</option>
          ))}
        </select>
      </label>

      <label>
        kind
        <select
          aria-label="kind"
          value={filters.kind ?? ''}
          onChange={(e) => setFilter('kind', e.target.value)}
        >
          <option value="">all</option>
          {kinds.map((k) => (
            <option key={k} value={k}>{k}</option>
          ))}
        </select>
      </label>

      <label>
        operation
        <select
          aria-label="operation"
          value={filters.operation ?? ''}
          onChange={(e) => setFilter('operation', e.target.value)}
        >
          <option value="">all</option>
          {operations.map((o) => (
            <option key={o} value={o}>{o}</option>
          ))}
        </select>
      </label>

      <label>
        name
        <input
          aria-label="name"
          value={filters.name ?? ''}
          onChange={(e) => setFilter('name', e.target.value)}
        />
      </label>

      <label>
        user
        <input
          aria-label="user"
          value={filters.user ?? ''}
          onChange={(e) => setFilter('user', e.target.value)}
        />
      </label>
    </div>
  )
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `cd web && npx vitest run src/components/FilterBar.test.tsx`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add web/src/useFilters.ts web/src/components/FilterBar.tsx web/src/components/FilterBar.test.tsx
git commit -m "feat(web): add URL filter state and FilterBar"
```

---

### Task 5: EventList — live feed with since-polling reconciliation

**Files:**
- Create: `web/src/components/EventList.tsx`, `web/src/useLiveFeed.ts`
- Test: `web/src/useLiveFeed.test.tsx`

**Interfaces:**
- Consumes: `useEventsFeed`, `eventsKey` (Task 3), `encodeCursor` (Task 2), `fetchEvents` (Task 2), `Filters`/`Row`/`Page` (Task 2), TanStack `useQueryClient`
- Produces:
  - `useLiveFeed(filters: Filters): void` — the polling side effect that prepends new rows into the feed cache
  - `EventList({ filters, onSelect }: { filters: Filters; onSelect(id: string): void })`

- [ ] **Step 1: Write the failing test for the reconciliation**

Create `web/src/useLiveFeed.test.tsx`:
```tsx
import { afterEach, beforeEach, test, expect, vi } from 'vitest'
import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { InfiniteData } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useLiveFeed } from './useLiveFeed'
import { eventsKey } from './api/hooks'
import type { Page, Row } from './types'

beforeEach(() => vi.useFakeTimers())
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

function row(id: string, time: string): Row {
  return {
    event_id: id, event_time: time, ingested_at: time, cluster: 'c1', source: 'webhook',
    operation: 'UPDATE', api_group: 'apps', api_version: 'v1', kind: 'Deployment',
    namespace: 'default', name: 'web', resource_uid: 'u', sub_resource: '',
    user_name: 'alice', user_groups: [], dry_run: false, diff: '[]',
  }
}

test('useLiveFeed prepends new since-rows without duplicating existing ones', async () => {
  const qc = new QueryClient()
  const filters = {}
  // seed the feed cache with one existing row
  qc.setQueryData<InfiniteData<Page>>(eventsKey(filters), {
    pageParams: [''],
    pages: [{ events: [row('old', '2026-07-01T10:00:00.000Z')], next_cursor: '' }],
  })

  // the since-poll returns the existing row (boundary dup) + one new row, ASC order
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: () =>
        Promise.resolve({
          events: [row('old', '2026-07-01T10:00:00.000Z'), row('new', '2026-07-01T10:00:05.000Z')],
          next_cursor: '',
        } satisfies Page),
    }),
  )

  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  )
  renderHook(() => useLiveFeed(filters), { wrapper })

  await vi.advanceTimersByTimeAsync(5000)

  const data = qc.getQueryData<InfiniteData<Page>>(eventsKey(filters))!
  const ids = data.pages[0].events.map((e) => e.event_id)
  expect(ids).toEqual(['new', 'old']) // new prepended, no duplicate 'old'
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npx vitest run src/useLiveFeed.test.tsx`
Expected: FAIL — cannot resolve `./useLiveFeed`.

- [ ] **Step 3: Implement useLiveFeed**

Create `web/src/useLiveFeed.ts`:
```ts
import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import type { InfiniteData } from '@tanstack/react-query'
import { eventsKey } from './api/hooks'
import { encodeCursor } from './api/cursor'
import { fetchEvents } from './api/client'
import type { Filters, Page } from './types'

const POLL_MS = 5000

// useLiveFeed polls for events newer than the newest row currently in the feed
// cache and prepends any new ones (newest-first). A since-response is ASC and may
// re-include the boundary row, so we dedupe against ids already present.
export function useLiveFeed(filters: Filters): void {
  const queryClient = useQueryClient()

  useEffect(() => {
    const key = eventsKey(filters)

    const tick = async () => {
      const data = queryClient.getQueryData<InfiniteData<Page>>(key)
      const newest = data?.pages?.[0]?.events?.[0]
      if (!newest) return

      let page: Page
      try {
        page = await fetchEvents(filters, { since: encodeCursor(newest) })
      } catch {
        return
      }
      if (page.events.length === 0) return

      const existing = new Set(data!.pages.flatMap((p) => p.events.map((e) => e.event_id)))
      const fresh = page.events.filter((e) => !existing.has(e.event_id))
      if (fresh.length === 0) return

      // since-response is ASC (oldest-new first); reverse for newest-first display
      const prepend = [...fresh].reverse()
      queryClient.setQueryData<InfiniteData<Page>>(key, (old) => {
        if (!old) return old
        const pages = old.pages.slice()
        pages[0] = { ...pages[0], events: [...prepend, ...pages[0].events] }
        return { ...old, pages }
      })
    }

    const id = setInterval(tick, POLL_MS)
    return () => clearInterval(id)
  }, [filters, queryClient])
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd web && npx vitest run src/useLiveFeed.test.tsx`
Expected: PASS

- [ ] **Step 5: Implement EventList**

Create `web/src/components/EventList.tsx`:
```tsx
import { useEventsFeed } from '../api/hooks'
import { useLiveFeed } from '../useLiveFeed'
import type { Filters, Row } from '../types'

function summarizeDiff(diff: string): string {
  try {
    const changes = JSON.parse(diff) as unknown[]
    return changes.length === 1 ? '1 change' : `${changes.length} changes`
  } catch {
    return ''
  }
}

export function EventList({ filters, onSelect }: { filters: Filters; onSelect: (id: string) => void }) {
  const feed = useEventsFeed(filters)
  useLiveFeed(filters)

  if (feed.isLoading) return <div className="feed-loading">Loading changes…</div>
  if (feed.isError) {
    return (
      <div className="feed-error">
        Failed to load changes. <button onClick={() => feed.refetch()}>Retry</button>
      </div>
    )
  }

  const rows: Row[] = feed.data?.pages.flatMap((p) => p.events) ?? []
  if (rows.length === 0) return <div className="feed-empty">No changes match these filters.</div>

  return (
    <div className="event-list">
      <table>
        <tbody>
          {rows.map((r) => (
            <tr key={r.event_id} onClick={() => onSelect(r.event_id)} className="event-row">
              <td>{new Date(r.event_time).toLocaleString()}</td>
              <td>{r.operation}</td>
              <td>{r.cluster}</td>
              <td>{r.namespace}/{r.kind}/{r.name}</td>
              <td>{r.user_name}</td>
              <td>{summarizeDiff(r.diff)}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {feed.hasNextPage && (
        <button onClick={() => feed.fetchNextPage()} disabled={feed.isFetchingNextPage}>
          {feed.isFetchingNextPage ? 'Loading…' : 'Load older'}
        </button>
      )}
    </div>
  )
}
```

- [ ] **Step 6: Run the full web suite to confirm no regressions**

Run: `cd web && npm test`
Expected: PASS (all tests so far)

- [ ] **Step 7: Commit**

```bash
git add web/src/useLiveFeed.ts web/src/useLiveFeed.test.tsx web/src/components/EventList.tsx
git commit -m "feat(web): add EventList with since-polling live feed reconciliation"
```

---

### Task 6: ActivityHistogram

**Files:**
- Create: `web/src/components/ActivityHistogram.tsx`
- Test: `web/src/components/ActivityHistogram.test.tsx`

**Interfaces:**
- Consumes: `useActivity` (Task 3), `useFilters` (Task 4), `Bucket` (Task 2), `recharts`
- Produces: `ActivityHistogram` component

- [ ] **Step 1: Write the failing test**

Recharts renders an SVG that jsdom sizes to 0 (so bars may not paint); assert on the accessible surrounding structure and the loading/empty states, which are deterministic. Create `web/src/components/ActivityHistogram.test.tsx`:
```tsx
import { afterEach, test, expect, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ActivityHistogram } from './ActivityHistogram'

afterEach(() => vi.unstubAllGlobals())

function renderHist(body: unknown) {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, json: () => Promise.resolve(body) }))
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <ActivityHistogram />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

test('shows an empty state when there is no activity', async () => {
  renderHist([])
  await waitFor(() => expect(screen.getByText(/no activity/i)).toBeInTheDocument())
})

test('renders the chart region when buckets exist', async () => {
  renderHist([{ bucket_start: '2026-07-01T10:00:00Z', count: 5 }])
  await waitFor(() => expect(screen.getByLabelText('activity histogram')).toBeInTheDocument())
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npx vitest run src/components/ActivityHistogram.test.tsx`
Expected: FAIL — cannot resolve `./ActivityHistogram`.

- [ ] **Step 3: Implement ActivityHistogram**

Create `web/src/components/ActivityHistogram.tsx`:
```tsx
import { Bar, BarChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { useActivity } from '../api/hooks'
import { useFilters } from '../useFilters'

const BUCKET = 'hour'

export function ActivityHistogram() {
  const { filters } = useFilters()
  const activity = useActivity(filters, BUCKET)

  if (activity.isLoading) return <div className="hist-loading">Loading activity…</div>
  if (activity.isError) return <div className="hist-error">Failed to load activity.</div>

  const data = (activity.data ?? []).map((b) => ({
    label: new Date(b.bucket_start).toLocaleString(),
    count: b.count,
  }))
  if (data.length === 0) return <div className="hist-empty">No activity in this window.</div>

  return (
    <div aria-label="activity histogram" className="activity-histogram" style={{ width: '100%', height: 160 }}>
      <ResponsiveContainer>
        <BarChart data={data}>
          <XAxis dataKey="label" hide />
          <YAxis allowDecimals={false} width={30} />
          <Tooltip />
          <Bar dataKey="count" fill="#4f46e5" />
        </BarChart>
      </ResponsiveContainer>
    </div>
  )
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd web && npx vitest run src/components/ActivityHistogram.test.tsx`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/src/components/ActivityHistogram.tsx web/src/components/ActivityHistogram.test.tsx
git commit -m "feat(web): add ActivityHistogram (recharts)"
```

---

### Task 7: EventDetail — diff viewer (structured + raw toggle)

**Files:**
- Create: `web/src/components/EventDetail.tsx`, `web/src/diff.ts`
- Test: `web/src/components/EventDetail.test.tsx`, `web/src/diff.test.ts`

**Interfaces:**
- Consumes: `useEvent` (Task 3), `DiffChange`/`Detail` (Task 2)
- Produces:
  - `diff.ts`: `parseDiff(diff: string): DiffChange[]`, `formatValue(v: unknown): string`
  - `EventDetail({ id }: { id: string })` component

- [ ] **Step 1: Write the failing diff-util test**

Create `web/src/diff.test.ts`:
```ts
import { test, expect } from 'vitest'
import { parseDiff, formatValue } from './diff'

test('parseDiff parses a valid diff array', () => {
  const changes = parseDiff('[{"path":"spec.replicas","op":"replace","old":2,"new":3}]')
  expect(changes).toHaveLength(1)
  expect(changes[0]).toMatchObject({ path: 'spec.replicas', op: 'replace', old: 2, new: 3 })
})

test('parseDiff returns [] for empty or invalid input', () => {
  expect(parseDiff('')).toEqual([])
  expect(parseDiff('not json')).toEqual([])
})

test('formatValue renders scalars and objects readably', () => {
  expect(formatValue(3)).toBe('3')
  expect(formatValue('x')).toBe('"x"')
  expect(formatValue(undefined)).toBe('∅')
  expect(formatValue({ a: 1 })).toBe('{"a":1}')
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npx vitest run src/diff.test.ts`
Expected: FAIL — cannot resolve `./diff`.

- [ ] **Step 3: Implement the diff utils**

Create `web/src/diff.ts`:
```ts
import type { DiffChange } from './types'

export function parseDiff(diff: string): DiffChange[] {
  if (!diff) return []
  try {
    const parsed = JSON.parse(diff)
    return Array.isArray(parsed) ? (parsed as DiffChange[]) : []
  } catch {
    return []
  }
}

export function formatValue(v: unknown): string {
  if (v === undefined) return '∅'
  if (v === null) return 'null'
  if (typeof v === 'string') return `"${v}"`
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd web && npx vitest run src/diff.test.ts`
Expected: PASS

- [ ] **Step 5: Write the failing EventDetail test**

Create `web/src/components/EventDetail.test.tsx`:
```tsx
import { afterEach, test, expect, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { EventDetail } from './EventDetail'

afterEach(() => vi.unstubAllGlobals())

const detail = {
  event_id: '1', event_time: '2026-07-01T10:00:00Z', ingested_at: '2026-07-01T10:00:01Z',
  cluster: 'c1', source: 'webhook', operation: 'UPDATE', api_group: 'apps', api_version: 'v1',
  kind: 'Deployment', namespace: 'default', name: 'web', resource_uid: 'u', sub_resource: '',
  user_name: 'alice', user_groups: [], dry_run: false,
  diff: '[{"path":"spec.replicas","op":"replace","old":2,"new":3}]',
  old_object: '{"spec":{"replicas":2}}', new_object: '{"spec":{"replicas":3}}',
  user_uid: 'uid', user_agent: '',
}

function renderDetail() {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, json: () => Promise.resolve(detail) }))
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={qc}>
      <EventDetail id="1" />
    </QueryClientProvider>,
  )
}

test('renders the structured diff by default', async () => {
  renderDetail()
  await waitFor(() => expect(screen.getByText('spec.replicas')).toBeInTheDocument())
  expect(screen.getByText(/replace/i)).toBeInTheDocument()
  expect(screen.getByText('alice')).toBeInTheDocument()
})

test('View raw toggles to side-by-side objects', async () => {
  renderDetail()
  await screen.findByText('spec.replicas')
  await userEvent.click(screen.getByRole('button', { name: /view raw/i }))
  expect(screen.getByLabelText('before')).toHaveTextContent('"replicas": 2')
  expect(screen.getByLabelText('after')).toHaveTextContent('"replicas": 3')
})
```

- [ ] **Step 6: Run to verify it fails**

Run: `cd web && npx vitest run src/components/EventDetail.test.tsx`
Expected: FAIL — cannot resolve `./EventDetail`.

- [ ] **Step 7: Implement EventDetail**

Create `web/src/components/EventDetail.tsx`:
```tsx
import { useState } from 'react'
import { useEvent } from '../api/hooks'
import { parseDiff, formatValue } from '../diff'

function pretty(json: string): string {
  try {
    return JSON.stringify(JSON.parse(json), null, 2)
  } catch {
    return json
  }
}

export function EventDetail({ id }: { id: string }) {
  const [raw, setRaw] = useState(false)
  const { data, isLoading, isError, refetch } = useEvent(id)

  if (isLoading) return <div className="detail-loading">Loading…</div>
  if (isError || !data) {
    return (
      <div className="detail-error">
        Failed to load event. <button onClick={() => refetch()}>Retry</button>
      </div>
    )
  }

  const changes = parseDiff(data.diff)

  return (
    <div className="event-detail">
      <header className="detail-meta">
        <div><strong>{data.operation}</strong> {data.namespace}/{data.kind}/{data.name}</div>
        <div>by <strong>{data.user_name}</strong> on {data.cluster}</div>
        <div>{new Date(data.event_time).toLocaleString()}</div>
        {data.dry_run && <span className="badge">dry-run</span>}
      </header>

      <button onClick={() => setRaw((v) => !v)}>{raw ? 'View structured' : 'View raw'}</button>

      {raw ? (
        <div className="raw-diff">
          <pre aria-label="before">{pretty(data.old_object)}</pre>
          <pre aria-label="after">{pretty(data.new_object)}</pre>
        </div>
      ) : (
        <ul className="structured-diff">
          {changes.length === 0 && <li>No field-level changes.</li>}
          {changes.map((c) => (
            <li key={c.path} className={`diff-${c.op}`}>
              <code>{c.path}</code> <span className="op">{c.op}</span>{' '}
              {formatValue(c.old)} → {formatValue(c.new)}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
```

- [ ] **Step 8: Run to verify it passes**

Run: `cd web && npx vitest run src/components/EventDetail.test.tsx`
Expected: PASS (both tests)

- [ ] **Step 9: Commit**

```bash
git add web/src/diff.ts web/src/diff.test.ts web/src/components/EventDetail.tsx web/src/components/EventDetail.test.tsx
git commit -m "feat(web): add EventDetail diff viewer (structured + raw toggle)"
```

---

### Task 8: Assemble the app — Drawer, routing, DashboardPage, ResourceTimeline

**Files:**
- Create: `web/src/components/Drawer.tsx`, `web/src/components/ResourceTimeline.tsx`, `web/src/components/DashboardPage.tsx`
- Modify: `web/src/App.tsx`
- Test: `web/src/App.routing.test.tsx`

**Interfaces:**
- Consumes: `FilterBar`, `ActivityHistogram`, `EventList`, `EventDetail` (Tasks 4–7), `useFilters` (Task 4), `useEventsFeed` (Task 3), `react-router-dom` (`Routes`, `Route`, `useNavigate`, `useParams`)
- Produces: the wired `App` with routes `/` (dashboard) and `/events/:id` (drawer overlay).

- [ ] **Step 1: Write the failing routing test**

Create `web/src/App.routing.test.tsx`:
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
      if (url.includes('/api/facets')) body = { clusters: ['c1'], kinds: ['Deployment'], operations: ['UPDATE'] }
      else if (url.includes('/api/activity')) body = []
      else if (/\/api\/events\/[^?]/.test(url)) {
        body = {
          event_id: '1', event_time: '2026-07-01T10:00:00Z', ingested_at: '2026-07-01T10:00:01Z',
          cluster: 'c1', source: 'webhook', operation: 'UPDATE', api_group: 'apps', api_version: 'v1',
          kind: 'Deployment', namespace: 'default', name: 'web', resource_uid: 'u', sub_resource: '',
          user_name: 'alice', user_groups: [], dry_run: false,
          diff: '[{"path":"spec.replicas","op":"replace","old":2,"new":3}]',
          old_object: '{}', new_object: '{}', user_uid: '', user_agent: '',
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

test('clicking a feed row opens the detail drawer at /events/:id', async () => {
  stubApi()
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/']}>
        <App />
      </MemoryRouter>
    </QueryClientProvider>,
  )

  const row = await screen.findByText('default/Deployment/web')
  await userEvent.click(row)
  await waitFor(() => expect(screen.getByText('spec.replicas')).toBeInTheDocument())
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npx vitest run src/App.routing.test.tsx`
Expected: FAIL — `App` still renders the placeholder.

- [ ] **Step 3: Implement Drawer**

Create `web/src/components/Drawer.tsx`:
```tsx
import type { ReactNode } from 'react'

export function Drawer({ onClose, children }: { onClose: () => void; children: ReactNode }) {
  return (
    <div className="drawer-overlay" onClick={onClose}>
      <aside className="drawer" role="dialog" aria-modal="true" onClick={(e) => e.stopPropagation()}>
        <button className="drawer-close" aria-label="close" onClick={onClose}>×</button>
        {children}
      </aside>
    </div>
  )
}
```

- [ ] **Step 4: Implement ResourceTimeline**

Create `web/src/components/ResourceTimeline.tsx`:
```tsx
import { useEventsFeed } from '../api/hooks'
import { parseDiff, formatValue } from '../diff'
import type { Filters } from '../types'

// ResourceTimeline shows one object's change history as a chronological track.
export function ResourceTimeline({ filters }: { filters: Filters }) {
  const feed = useEventsFeed(filters)

  if (feed.isLoading) return <div>Loading timeline…</div>
  if (feed.isError) return <div>Failed to load timeline.</div>

  const rows = feed.data?.pages.flatMap((p) => p.events) ?? []
  if (rows.length === 0) return <div>No history for this resource.</div>

  return (
    <ol className="resource-timeline">
      {rows.map((r) => {
        const changes = parseDiff(r.diff)
        return (
          <li key={r.event_id} className="timeline-entry">
            <div className="timeline-when">{new Date(r.event_time).toLocaleString()} — {r.operation} by {r.user_name}</div>
            <ul>
              {changes.map((c) => (
                <li key={c.path}>
                  <code>{c.path}</code>: {formatValue(c.old)} → {formatValue(c.new)}
                </li>
              ))}
            </ul>
          </li>
        )
      })}
    </ol>
  )
}
```

- [ ] **Step 5: Implement DashboardPage**

Create `web/src/components/DashboardPage.tsx`:
```tsx
import { useNavigate } from 'react-router-dom'
import { useFilters } from '../useFilters'
import { FilterBar } from './FilterBar'
import { ActivityHistogram } from './ActivityHistogram'
import { EventList } from './EventList'

export function DashboardPage() {
  const { filters } = useFilters()
  const navigate = useNavigate()

  return (
    <div className="dashboard">
      <h1>KubeWatch Dashboard</h1>
      <FilterBar />
      <ActivityHistogram />
      <EventList filters={filters} onSelect={(id) => navigate(`/events/${id}${location.search}`)} />
    </div>
  )
}
```

- [ ] **Step 6: Wire App with routes**

Replace `web/src/App.tsx`:
```tsx
import { Routes, Route, useNavigate, useParams, useLocation } from 'react-router-dom'
import { DashboardPage } from './components/DashboardPage'
import { Drawer } from './components/Drawer'
import { EventDetail } from './components/EventDetail'

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
    <>
      <DashboardPage />
      <Routes>
        <Route path="/events/:id" element={<EventDrawer />} />
        <Route path="*" element={null} />
      </Routes>
    </>
  )
}
```

Note: `DashboardPage` renders on every route so the feed stays mounted behind the drawer; the `Routes` block only decides whether the drawer overlay is shown. The smoke test from Task 1 (`renders the dashboard title`) still passes because `DashboardPage` renders the `<h1>KubeWatch Dashboard</h1>`.

- [ ] **Step 7: Run the routing test and the full suite**

Run:
```bash
cd web && npx vitest run src/App.routing.test.tsx && npm test
```
Expected: routing test passes; full suite green.

- [ ] **Step 8: Typecheck and build**

Run: `cd web && npm run build`
Expected: `tsc` reports no type errors; `web/dist` is produced.

- [ ] **Step 9: Commit**

```bash
git add web/src/components/Drawer.tsx web/src/components/ResourceTimeline.tsx web/src/components/DashboardPage.tsx web/src/App.tsx web/src/App.routing.test.tsx
git commit -m "feat(web): assemble dashboard — routing, drawer, timeline, page wiring"
```

- [ ] **Step 10: Manual end-to-end check (optional, needs the running API)**

With a local ClickHouse + the Go dashboard running (`SPA_DIR` unset, API on `:8081`):
```bash
cd web && npm run dev
# open http://localhost:5173 — feed loads, filters update the URL, clicking a row
# opens the diff drawer, the histogram renders, "Load older" pages.
```
Then a production check: `npm run build` and run the Go binary with `SPA_DIR=web/dist`, browse `http://localhost:8081`.

---

## Deferred (documented, not built in v1)

- `go:embed` single-binary packaging of `web/dist`.
- Line-level text diffing in the raw view (v1 is object-level pretty-print).
- Full-text search UI (awaits the API's deferred content search).
- Virtualized feed list for very large result sets; saved searches; auth UI.
- The `ResourceTimeline` is implemented and reachable by navigating to a filtered feed; wiring a dedicated "resource" entry point (click a resource name → timeline drawer) is a small follow-up once product decides the exact affordance.
