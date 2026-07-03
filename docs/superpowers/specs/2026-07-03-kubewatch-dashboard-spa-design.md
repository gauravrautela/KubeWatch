# KubeWatch Dashboard SPA (Frontend) — Design

**Status:** Design approved
**Date:** 2026-07-03
**Author:** gaurav.rautela
**Relationship:** Plan 2b of 2. Consumes the read-path JSON API from Plan 2a
(`docs/superpowers/specs/2026-07-03-kubewatch-dashboard-design.md` and
`docs/superpowers/plans/2026-07-03-kubewatch-dashboard-api.md`). Frontend only —
no Go changes.

## Summary

A React + TypeScript single-page app (Vite) that presents the KubeWatch audit
trail: a live change feed, an activity histogram, structured search/filter, a
diff viewer, and per-resource timelines. It consumes the existing dashboard JSON
API (`/api/events`, `/api/events/{id}`, `/api/activity`, `/api/facets`) and is
served in production by the Go `kubewatch-dashboard` binary via `SPA_DIR`.

## Goals

- Implement all defined views: live feed (A), diff viewer (C), search & filter
  (D) with cluster first-class, per-resource history (B), activity histogram, and
  per-resource timeline.
- URL query string is the single source of truth for filters and selected event —
  every view is deep-linkable and shareable.
- Live feed updates via `since`-polling with gap-free forward paging.
- Frontend-only: served by the Go binary through `SPA_DIR`; no Go changes.

## Non-Goals (v1)

- Server changes of any kind (the API contract is fixed by Plan 2a).
- `go:embed` single-binary packaging (a documented future upgrade; v1 uses `SPA_DIR`).
- Line-level text diffing in the raw view (object-level pretty-print only in v1).
- Authentication UI (the deployment sits behind an authenticating proxy).
- Full-text search UI (the API does structured filters only in v1).

## Architecture

The SPA lives in a new `web/` directory, isolated from the Go code.

```
web/
  index.html
  package.json          react, react-dom, react-router-dom, @tanstack/react-query, recharts
  vite.config.ts        dev server :5173; proxy /api -> http://localhost:8081
  tsconfig.json
  src/
    main.tsx            entry: QueryClientProvider + RouterProvider
    types.ts            TS mirror of API JSON shapes (Row, Detail, Page, Bucket, Facets)
    api/
      client.ts         typed fetch client: URL + query-param construction, error throwing
      hooks.ts          TanStack Query hooks: useEventsFeed, useEvent, useActivity, useFacets
    components/
      DashboardPage.tsx
      FilterBar.tsx
      ActivityHistogram.tsx
      EventList.tsx
      EventDetail.tsx
      ResourceTimeline.tsx
      Drawer.tsx
    dist/               build output (gitignored)
```

**Dev workflow:** `npm run dev` runs Vite on `:5173`; its proxy forwards `/api/*`
to the Go dashboard on `:8081` — both run side-by-side with hot reload, no CORS.

**Production:** `npm run build` emits `web/dist`; run the Go binary with
`SPA_DIR=web/dist` (Plan 2a's `spaFileServer` serves it with index.html fallback).

**Types:** a hand-written `types.ts` mirrors the Go JSON shapes — small and stable,
no codegen.

## State Architecture

- **URL query string is the single source of truth** for filters (cluster,
  namespace, kind, name, user, operation, from, to) and the selected event
  (`/events/:id` route). `react-router-dom`'s `useSearchParams` reads/writes them.
- Every data hook derives its **TanStack Query key** from the URL params, so a URL
  change automatically drives refetch/caching. Every view is shareable via URL.
- The API client (`client.ts`) is the single place that knows endpoint URLs and
  builds query strings from a `Filters` object.

## Components

```
App (RouterProvider + QueryClientProvider)
└─ DashboardPage                     reads filters from URL
   ├─ FilterBar                      cluster/kind/operation dropdowns (useFacets) + name/user inputs + time range; writes URL
   ├─ ActivityHistogram              useActivity(filters, bucket); Recharts bars; click bar -> sets from/to in URL
   ├─ EventList                      live feed (see below)
   └─ Drawer (route /events/:id)     EventDetail (diff viewer) OR ResourceTimeline; overlaid; closes back to feed
```

### EventList — live feed reconciliation

`EventList` merges two directions into one list backed by a single TanStack Query cache:

- **History (older direction):** `useInfiniteQuery` keyed on the filters. Initial
  fetch = newest page (no cursor). "Load older" uses each page's `next_cursor`
  (DESC paging, per Plan 2a).
- **Live (newer direction):** a poller (`refetchInterval` ~5s) calling
  `/api/events?since=<newestCursor>`. It tracks the newest event's cursor; the
  gap-free ASC `since` response (Plan 2a fix) is **prepended** to the first page
  via `queryClient.setQueryData`, and the newest cursor advances using the
  response's `next_cursor` (which points at the newest row for a `since` query).
  An `event_id` seen-set guards against a boundary duplicate.
- **On filter change:** the query key changes -> everything refetches fresh and the
  live cursor resets. No stale merging.

This keeps one cache as the source of truth (no parallel local list to desync).

## Diff Viewer (EventDetail)

Opened in the drawer via `useEvent(id)` (detail endpoint returns full objects +
structured diff).

- **Default — structured view:** renders the precomputed `diff` array as a compact
  list of changed paths; each row shows `op` (add/remove/replace, color-coded) and
  `old -> new` values (JSON-stringified scalars/objects).
- **Toggle — raw side-by-side:** a "View raw" switch renders `old_object` and
  `new_object` as pretty-printed JSON in two panes. v1 highlights at the object
  level (pretty-print both, no line-diff library); a line-level diff is a future
  upgrade.
- **Metadata header:** user, cluster, namespace, kind/name, operation, timestamps
  (event_time + ingested_at), and a dry-run badge.

## ResourceTimeline

Same drawer, opened from a resource. A filtered feed
(cluster+namespace+kind+name) rendered as a vertical chronological track; each
entry expands to its structured diff.

## Error, Loading & Empty States

Every hook surfaces TanStack Query's `isLoading`/`isError`. Components render
skeletons while loading, an inline error with a retry button on failure, and
explicit empty states ("No changes match these filters"). The API client throws a
typed error on any non-2xx response, surfacing the API's `{"error": ...}` message.

## Testing Strategy

Vitest + React Testing Library:

- **`client.ts`** — URL/query-param construction from a `Filters` object, cursor
  threading (`cursor`/`since`), and typed error throwing on non-2xx (mocked `fetch`).
- **`FilterBar`** — URL param read/write round-trips.
- **`EventList`** — renders a page; "load older" appends the next page; a simulated
  `since` poll **prepends** new rows without duplicates (the core reconciliation).
- **`EventDetail`** — structured diff renders each change; the raw toggle switches
  panes.
- Hooks are tested via a `QueryClientProvider` wrapper with mocked `fetch`.

## Deferred (documented, not built in v1)

- `go:embed` single-binary packaging of `web/dist`.
- Line-level text diffing in the raw view.
- Full-text search UI (awaits the API's deferred content search).
- Saved searches, alerting UI, auth UI.
