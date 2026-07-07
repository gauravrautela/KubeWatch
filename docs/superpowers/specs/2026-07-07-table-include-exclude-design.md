# Table Include/Exclude Filter Buttons — Design

**Date:** 2026-07-07
**Status:** Approved

## Goal

Let users filter the dashboard event table in one click, Kibana-style: hovering a
value in a table cell reveals ⊕ (include) and ⊖ (exclude) buttons that apply the
corresponding filter immediately.

## Scope

Filterable fields, each with both include and exclude:

- kind, namespace, name (the Resource column — each segment is an independent
  hover target)
- user (User column)
- cluster (Cluster column)
- operation (Operation column)

Backend today supports excludes only for kinds and namespaces, so four new
exclude params are added server-side (see Backend).

## Interaction

- Hovering a filterable value shows two small icon buttons immediately after it:
  **⊕ include** and **⊖ exclude**. Icons are hidden otherwise (`group-hover`
  pattern); the table stays visually clean.
- Clicking either button applies the filter instantly by updating the URL search
  params (the existing `useFilters` model); the events feed, histogram, and
  facets refetch automatically.
- Button clicks call `stopPropagation` so they do not trigger the row's
  click-to-open-detail behavior.
- **Include** sets the single-value param (e.g. `kind=Deployment`), identical to
  choosing it in the FilterBar dropdown. Including a different value replaces
  the previous one. Note: for the name and user fields the include param is a substring match (matching existing FilterBar semantics) while the exclude is an exact match — this asymmetry is intentional.
- **Exclude** appends to the CSV multi-value param (e.g.
  `exclude_kinds=Lease,Endpoints`), identical to the existing "Ignore…"
  dropdown behavior.
- Mutual exclusion per field: excluding a value clears a matching include for
  that field, and including a value removes it from that field's exclude list,
  so contradictory states like `kind=Deployment&exclude_kinds=Deployment` never
  occur.
- Active filters continue to appear as removable chips in the existing
  FilterChips row; excludes render in the red "not …" style. No new state UI.

## Backend (Go API)

In `internal/dashboardapi/api.go`, add four CSV exclude params following the
exact pattern of `exclude_kinds` / `exclude_namespaces`:

- `exclude_users` → `user_name NOT IN (...)`
- `exclude_clusters` → `cluster NOT IN (...)`
- `exclude_names` → `name NOT IN (...)`
- `exclude_operations` → `operation NOT IN (...)`

Applied everywhere the existing excludes are applied (events list, activity histogram, and the live feed, which share the same query builder). Facets stay unfiltered by design so dropdowns always show all values.

## Frontend components

- **New:** `web/src/components/CellFilter.tsx` — wraps a value; renders the
  value plus hover ⊕/⊖ buttons. Props: `field` (filter key), `value`, children
  (the rendered value). Uses `useFilters` to apply include/exclude with the
  mutual-exclusion rule above. Buttons get `aria-label`s like
  `include kind Deployment` / `exclude kind Deployment`.
- **`web/src/components/EventList.tsx`** — wrap the six cell values in
  `CellFilter` (kind, namespace, name inside the Resource cell; user, cluster,
  operation in their columns).
- **`web/src/types.ts`** — add `exclude_users`, `exclude_clusters`,
  `exclude_names`, `exclude_operations` to `Filters`.
- **`web/src/useFilters.ts`** — add the four keys to `FILTER_KEYS`.
- **`web/src/components/FilterChips.tsx`** — generalize the per-key exclude
  loops into one list covering all six exclude params, with labels like
  `not user: alice`.

## Field → param mapping

| Field     | Include param | Exclude param        |
|-----------|---------------|----------------------|
| kind      | `kind`        | `exclude_kinds`      |
| namespace | `namespace`   | `exclude_namespaces` |
| name      | `name`        | `exclude_names`      |
| user      | `user`        | `exclude_users`      |
| cluster   | `cluster`     | `exclude_clusters`   |
| operation | `operation`   | `exclude_operations` |

## Testing

- **Go** (`internal/dashboardapi/api_test.go`): table-driven cases for the four
  new params mirroring the existing exclude tests — filtering effect on the
  events list, plus sanitization of empty CSV entries.
- **Frontend:**
  - `CellFilter.test.tsx`: include click sets the param; exclude click appends
    to the CSV; excluding clears a matching include and vice versa; button
    click does not fire the row's onClick.
  - `FilterChips.test.tsx`: chips render and remove correctly for the new
    exclude params.
  - `EventList.test.tsx`: cells render values wrapped with filter buttons.

## Out of scope

- Multi-value includes (e.g. `kind IN (a,b)`) — includes stay single-value.
- Include/exclude on the event detail page.
- Time or diff-count cell filtering.
