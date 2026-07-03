# KubeWatch Dashboard (Read Path) — Design

**Status:** Design approved
**Date:** 2026-07-03
**Author:** gaurav.rautela
**Relationship:** Plan 2 of 2. Plan 1 (capture & storage pipeline) built the write
path — agents, hub ingest, and the ClickHouse `change_events` table. This spec
covers the read path that queries that table. See
`docs/superpowers/specs/2026-07-01-kubewatch-change-audit-design.md`.

## Summary

A read-only dashboard for the KubeWatch audit trail: a **Go JSON API** over the
existing ClickHouse `change_events` table, plus a **React/TypeScript SPA**. It is a
new, independent binary (`cmd/dashboard`) that shares the `internal/storage`
ClickHouse layer and the `internal/event` model with the hub but never touches the
write path. The service is **stateless** — every request is a ClickHouse query.

## Goals

- Serve all dashboard views defined in the original design:
  - **A. Live change feed** — real-time stream of who/what/where/when.
  - **C. Diff viewer** — full before→after for any change.
  - **D. Search & filter** — structured filters, cluster a first-class dimension.
  - **B. Per-resource history** — one object's full change timeline.
  - **Activity histogram** — change volume over time.
  - **Per-resource timeline** — one object's chronological change track.
- Keep the API a stable contract and the SPA a thin, replaceable consumer.
- Simple but extendable: no schema changes; new endpoints/features are additive.

## Non-Goals (v1)

- Full-text search over object contents (`old_object`/`new_object`/`diff`). Structured
  filters only; a token/ngram index + query param can be added later.
- Push transport (SSE/WebSocket). v1 uses client polling; the API contract does not
  change if SSE is added later.
- Built-in authentication. The dashboard runs behind an authenticating ingress/SSO
  proxy (see Security).
- Writes of any kind. Read-only.

## Architecture

```
  Browser (React SPA)
      │  JSON over HTTPS; polls GET /api/events?since=<cursor> every ~5s
      ▼
  Dashboard API (Go, cmd/dashboard)  ──queries──►  ClickHouse (change_events)
      └── also serves the built SPA static assets
```

Language: **Go** for the API (matches the hub), **React + TypeScript** (Vite) for the SPA.

### Components

1. **`cmd/dashboard`** (Go binary) — wires config, opens ClickHouse, serves the API
   and the built SPA static assets, graceful shutdown (same pattern as the hub).
2. **`internal/dashboardapi`** (Go) — HTTP handlers, request parsing/validation,
   cursor encode/decode, JSON responses. Depends on the storage read methods.
3. **`internal/storage`** read methods (Go) — new read-only methods on the existing
   ClickHouse store: `ListEvents`, `GetEvent`, `Activity`, `Facets`. Owns SQL.
4. **React SPA** — the views below, consuming the API.

## API

All endpoints return JSON. Filters are shared across `GET /api/events` and
`GET /api/activity`.

| Endpoint | Purpose | Powers |
|---|---|---|
| `GET /api/events` | list/search/feed: structured filters + keyset pagination + `since` cursor. Returns lightweight rows (metadata + `diff`, NOT full objects). | Feed (A), Search (D), Per-resource history (B), Per-resource timeline |
| `GET /api/events/{event_id}` | full detail of one change: all metadata + full `old_object` + `new_object` + `diff`. | Diff viewer (C) |
| `GET /api/activity` | time-bucketed change counts (`bucket`, `from`, `to`, + filters). | Activity histogram |
| `GET /api/facets` | distinct values for low-cardinality filters (clusters, kinds, operations). | Filter dropdowns |
| `GET /healthz` | liveness. | ops |

### Filters (query params)

`cluster`, `namespace`, `kind`, `name` (substring, case-insensitive), `user`
(substring, case-insensitive), `operation`, `from` (RFC3339), `to` (RFC3339).
Absent params are unconstrained. `GET /api/events` also accepts `cursor`, `since`,
and `limit`.

### Pagination & live polling

- **Keyset (cursor) pagination** on `(event_time, event_id)` — stable and fast on an
  append-only table; no `OFFSET`. Default order `event_time DESC, event_id DESC`.
- `cursor=<opaque>` fetches the next older page: `WHERE (event_time, event_id) <
  (cursor_time, cursor_id)`.
- `since=<opaque>` fetches newer rows for live polling: `WHERE (event_time,
  event_id) > (cursor_time, cursor_id)`.
- Cursor is a base64-encoded `(event_time, event_id)` pair. Undecodable → 400.
- `limit` is capped server-side (max 200/page); requests above the cap are clamped.

### Response shapes

- **List row:** `event_id, event_time, ingested_at, cluster, source, operation,
  api_group, api_version, kind, namespace, name, resource_uid, sub_resource,
  user_name, user_groups, dry_run, diff`. Includes a `next_cursor` at the envelope
  level. Excludes `old_object`/`new_object` to keep payloads small.
- **Detail:** the full row including `old_object`, `new_object`, `diff`, and
  `user_uid`/`user_agent`.
- **Activity:** array of `{ bucket_start, count }`.
- **Facets:** `{ clusters: [...], kinds: [...], operations: [...] }`.

## SPA

**Stack:** React + TypeScript, built with Vite.

- **Data layer — TanStack Query:** `since`-based polling for the live feed, keyset
  infinite pagination ("load older"), response caching. Localizes any future
  transport change (e.g. SSE) to the query hooks.
- **Charting — Recharts:** the activity histogram (time-bucketed bars; click a bar to
  set the time filter).
- **Routing — React Router**, with **filters encoded in the URL query string** so
  every filtered view is a shareable, bookmarkable link and back/forward works.

### Components

| Component | Role |
|---|---|
| `FilterBar` | cluster/kind/operation dropdowns (from `/api/facets`) + name/user text inputs + time-range picker; reads/writes URL params. |
| `EventList` | live feed: keyset-paginated infinite list; prepends new rows from `since` polling; each row shows who/what/where/when + compact diff summary. |
| `ActivityHistogram` | Recharts time-bucketed bars from `/api/activity`; click-to-drill sets the time filter. |
| `EventDetail` | drawer opened from a feed row → fetches `/api/events/{id}` → full before/after + diff viewer. |
| `ResourceTimeline` | per-resource view (filtered to one object) as a vertical chronological track; each entry expands to its diff. |

**Data flow:** URL filters → typed API client → TanStack Query → render. The feed
poll runs on a ~5s interval, fetching only rows newer than the newest cursor and
prepending them.

## Query Mechanics (backend)

- Read methods live in `internal/storage` (owns the connection and schema):
  `ListEvents(ctx, filter) ([]Row, nextCursor, error)`, `GetEvent(ctx, id)
  (Detail, error)`, `Activity(ctx, filter, bucket) ([]Bucket, error)`,
  `Facets(ctx) (Facets, error)`.
- A `Filter` struct is translated into **parameter-bound** ClickHouse queries — never
  string-concatenated — so user input can never be injected.
- `ListEvents` uses the primary sort key for fast keyset scans; `name`/`user`
  substring uses case-insensitive matching (won't use the bloom index — acceptable).
- `Activity` uses `toStartOfInterval(event_time, INTERVAL <bucket>)` + `count()`
  `GROUP BY` with the same filters.
- `Facets` uses `DISTINCT` only on `LowCardinality` columns (cluster, kind,
  operation) — cheap. High-cardinality dims (name, user) are free-text inputs, never
  faceted.

## Reliability & Error Handling

- **Per-request context timeout** on every ClickHouse query; a slow/huge query cannot
  hang the server.
- Invalid params or an undecodable cursor → **400** with a clear JSON error.
- ClickHouse failures → **500**, logged, generic message to the client.
- Empty result → **200** with `[]`, never an error.
- `limit` capped server-side (max 200/page).

## Security / Authentication

v1 has **no built-in auth**. The dashboard is read-only and deployed **behind an
authenticating ingress/SSO proxy** — the same boundary posture as the hub's required
TLS front. Because audit data is sensitive, this proxy is a documented **deployment
invariant**, not optional. Built-in auth (SSO/OIDC) is deferred and revisitable.

## Testing Strategy

- **Query builder** — table-driven unit tests: `Filter` → expected SQL + bound args
  (no DB).
- **Handlers** — tests against a fake read-store: param parsing, JSON shape, cursor
  round-trip, 400/500 paths, `limit` clamping.
- **Storage read methods** — integration tests against real ClickHouse (skip without
  `CLICKHOUSE_DSN`): seed rows, assert filtering, keyset pagination, `since`, and
  activity bucketing.
- **SPA** — Vitest + React Testing Library on the key pieces: `FilterBar` (URL sync),
  `EventList` (polling prepend + pagination), `EventDetail` (diff render), and the
  typed API client.

## Deferred (documented, not built in v1)

- Full-text search over object contents (needs a ClickHouse token/ngram index).
- SSE/WebSocket push transport (API contract already accommodates it).
- Built-in authentication (SSO/OIDC).
- `ReplacingMergeTree`/`event_id`-in-sort-key dedup (a write-path migration; note that
  the current `ORDER BY` does not include `event_id`).
- Saved searches / alerting on specific change patterns.
