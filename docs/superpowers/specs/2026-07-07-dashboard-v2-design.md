# KubeWatch Dashboard v2 — Design

Date: 2026-07-07
Status: Approved

## Goal

Turn the functional-but-unstyled SPA (`web/`) into a mature ops dashboard:
a dark-console visual design, complete filtering (search, namespace, time
range, filter chips), and a full detail page presenting each change as a
unified diff.

## Decisions (user-confirmed)

- **Diff presentation:** unified diff only (git-diff style, `-`/`+` lines).
  No split view, no field-change table.
- **Detail layout:** full page at `/events/:id`, replacing the drawer.
- **Filters:** time-range picker, namespace filter, active-filter chips with
  clear-all, and a free-text search box. All include-filter controls are
  single-select.
- **Ignore (exclude) filter:** noise can be filtered OUT — exclude specific
  kinds (e.g. Lease, Endpoints) and namespaces (e.g. kube-system) from the
  feed and histogram. Excludes are multi-value.
- **Visual style:** dark ops-console (Grafana/Datadog register).
- **Approach:** Tailwind CSS for styling; `diff` (jsdiff) for line diffs;
  keep React 18 + TanStack Query + react-router + Recharts.

## New dependencies

- `tailwindcss` v4 + `@tailwindcss/vite` (dev): utility styling, CSS-first
  theme config, no PostCSS config file needed with Vite.
- `diff` (jsdiff, runtime): line-level diff of pretty-printed JSON.

## Backend changes (Go)

1. **Namespaces facet** — `storage.Facets` gains `Namespaces []string`
   (`SELECT DISTINCT namespace FROM change_events ORDER BY namespace`);
   surfaced through `GET /api/facets` as `namespaces`.
2. **`q` search param** — `storage.Filter` gains `Q string`; the WHERE
   builder adds `(name ILIKE ? OR user_name ILIKE ?)` with `%q%` bound
   twice. `dashboardapi.parseFilter` reads `q`. Existing `name`/`user`
   params remain for deep links.
3. **Exclude params** — `storage.Filter` gains `ExcludeKinds []string` and
   `ExcludeNamespaces []string`; the WHERE builder adds `kind NOT IN (?)` /
   `namespace NOT IN (?)` when non-empty. `parseFilter` reads them from
   comma-separated `exclude_kinds` / `exclude_namespaces` query params.
   Excludes apply to `/api/events` and `/api/activity` alike (they share
   `filterConds`).

All three changes get unit tests alongside the existing table-driven ones
(`query_test.go`, `read_test.go`, `api_test.go`).

## Frontend changes

### App shell & theme

- Tailwind theme tokens for a dark console: zinc/slate surface scale,
  operation accents (CREATE green, UPDATE amber, DELETE red), monospace
  (`ui-monospace` stack) for resource names and diff text.
- Sticky header: "KubeWatch" wordmark, live-feed indicator (pulsing dot,
  "live" while the 5s poll is active), and the loaded-events count.
- Content in a centered max-width container. The app ships a single
  dark theme (no light mode, no theme toggle).

### FilterBar v2

One control row plus a chips row; all state remains URL query params via
the existing `useFilters` hook (extended with `q`):

- **Search box** — debounced 300 ms, sets `q` (matches name OR user).
- **Selects** — cluster, namespace, kind, operation, populated from
  `/api/facets`; empty option = "all".
- **Time range** — preset select: 15m, 1h, 6h, 24h, 7d, All time, Custom.
  Presets serialize to absolute RFC3339 `from`/`to` at selection time.
  Custom shows two `datetime-local` inputs; values are converted with
  `new Date(v).toISOString()` (never locale strings — the API 400s on
  non-RFC3339).
- **Ignore control** — an "Ignore…" dropdown listing facet kinds and
  namespaces; picking a value adds it to `exclude_kinds` /
  `exclude_namespaces` (comma-separated in the URL). Multiple values can
  be excluded. Excluding and including the same dimension is allowed;
  the backend just ANDs the conditions.
- **Chips row** — one removable chip per active filter and a
  "Clear all" button; hidden when no filters are active. Exclude chips
  are visually distinct (styled `not: kube-system`) and removable
  individually.

### ActivityHistogram v2

- Bucket auto-selection from the active range: ≤2h → minute, ≤3d → hour,
  else day.
- Bar click sets `from`/`to` to that bucket's bounds (ISO strings from
  `bucket_start`), zooming the range — closes the deferred item from the
  SPA whole-branch review.
- Styled to the theme; tooltip with exact bucket time and count.

### EventList v2

- Table: relative timestamp (`title` = exact ISO), operation badge,
  `Kind namespace/name` in monospace, user, cluster.
- Dry-run events show an outline "dry-run" badge.
- Row click navigates to `/events/:id` preserving the current query
  string; rows remain keyboard-activatable (existing behavior).
- "Load more" button drives the existing cursor pagination.
- Styled empty / loading / error states.

### Detail page (replaces Drawer)

- `/events/:id` renders `EventDetailPage` as a full page (the
  `DashboardPage` no longer renders beneath it). Breadcrumb
  "← Events" returns to `/` with the preserved query string.
- Header: operation badge, `Kind namespace/name`, dry-run badge.
- Metadata grid: cluster, source, user, user groups, user agent,
  api group/version, sub-resource, event time (exact), event id.
- **Unified diff**: pure function `buildUnifiedDiff(oldJson, newJson)`
  in `web/src/unifiedDiff.ts` — pretty-prints both objects
  (2-space JSON), runs jsdiff `diffLines`, and folds unchanged runs
  longer than 6 lines down to 3 lines of context plus a
  "⋯ N unchanged lines" expander row. Renderer paints `-` lines
  red-tinted, `+` lines green-tinted, gutter markers in a monospace
  block with horizontal scroll containment.
- CREATE (empty `old_object`) renders all-added; DELETE all-removed.
- "Copy JSON" buttons for raw before/after.
- `Drawer.tsx` and its tests are deleted; `App.tsx` routes
  `/events/:id` to the page component.

## Error handling

- Unparseable `old_object`/`new_object` JSON: fall back to raw string
  lines in the diff (jsdiff still works on text).
- Facets/list/detail query errors keep the existing retry affordances,
  restyled.
- Invalid custom date input: ignore until both bounds parse.

## Testing

- Update existing suites for the new DOM: `FilterBar.test.tsx`,
  `App.routing.test.tsx`, `EventDetail.test.tsx`, `useFilters.test.tsx`.
- New: `unifiedDiff.test.ts` (pure hunk builder: replace/add/remove,
  context folding, CREATE/DELETE, malformed JSON), time-range
  serialization tests (presets and custom produce RFC3339), chips
  render/remove/clear-all tests, histogram bucket auto-selection test.
- Go: `q` condition building, NOT IN exclude conditions, namespaces
  facet, `parseFilter` for `q` and comma-separated excludes.
- Gate: `npm run test && npm run build` and `go test ./...` green;
  manual smoke against the dev proxy if the hub stack is reachable.

## Out of scope

- Light theme / theme switching.
- Multi-select include filters (excludes are multi-value; includes stay
  single-select).
- Persisted/server-side saved ignore lists (excludes live in the URL only).
- Server-side diff rendering; auth; RBAC on the dashboard.
