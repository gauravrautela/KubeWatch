# Incident Lens — Design

**Date:** 2026-07-08
**Status:** Approved

## Problem

During an incident, responders spend the first 15–30 minutes finding *what
changed* — digging through kubectl, CI history, and Slack. KubeWatch already
captures every CREATE/UPDATE/DELETE with actor identity and a structured diff,
but the dashboard requires knowing what to filter for. Responders typically
know only the cluster and the approximate incident start time.

## Goal

A dedicated **Incident Lens** page: the responder enters a cluster and an
approximate incident time, and gets a ranked, noise-suppressed list of suspect
changes with drill-down to the full diff. Find only — rollback assistance,
alert integration, and sharing are explicitly out of scope for v1.

### Non-goals (v1)

- Revert-manifest generation / rollback assistance.
- Push integrations (Alertmanager, PagerDuty, Slack).
- Shareable frozen permalinks of an incident view.
- Collecting native Kubernetes Events (OOMKilled etc.) — webhook change data only.
- LLM-based ranking or narrative explanations. Scoring is deterministic and
  explainable.

## Architecture overview

Three additions, all hub-side (no agent changes):

1. **Ingest-time classification** — when the hub computes the diff
   (`internal/ingest/handler.go`), it also derives `change_class` and
   `actor_type`, stored as new columns on `change_events`.
2. **Change-frequency stats** — a small aggregate table fed by a materialized
   view, with 30-day TTL, powering rarity and churn signals beyond the raw
   TTL.
3. **Query-time scoring** — a new `GET /api/incident` endpoint on the
   dashboard API ranks the window's events; a new `/incident` SPA page renders
   the result.

## Data model

### New columns on `change_events`

Added via `ALTER TABLE change_events ADD COLUMN IF NOT EXISTS ...` in the
existing `Store.Migrate` path (additive, zero-downtime):

- `change_class Array(LowCardinality(String))` — an event can carry several
  classes (e.g. an update that bumps the image and changes env). Values:
  `rbac`, `network`, `config-data`, `image`, `env`, `resources`, `scale`,
  `metadata-only`, `status-only`, `other`.
- `actor_type Enum8('unknown' = 0, 'human' = 1, 'serviceaccount' = 2,
  'system' = 3) DEFAULT 'unknown'` — derived from `user_name`/`user_groups`:
  `system:serviceaccount:*` → `serviceaccount`; any other `system:*` →
  `system`; otherwise `human`. Pre-existing rows read as `unknown` (neutral
  weight), never as `human`.

### Raw TTL change

`change_events` TTL goes from **1 day to 7 days** so incident drill-down
(diffs, old/new objects) survives long enough for post-incident reviews and
late-discovered incidents. Applied with `ALTER TABLE ... MODIFY TTL` for
existing installs and updated in the `CREATE TABLE` DDL.

### New aggregate table `resource_change_stats`

SummingMergeTree fed by a materialized view on `change_events` inserts:

- Key: `(cluster, namespace, kind, name, day)`; value: `change_count`.
- Rows contain no object bodies — tiny. **TTL 30 days.**
- Powers two query-time signals: rarity (days since previous change) and
  churn (average changes per day).

### Day-1 behavior

Pre-existing rows have empty `change_class` and default `actor_type`; the
stats table starts empty. Scoring treats missing data as neutral, so the page
works immediately and rarity/churn sharpen over the first weeks. Unclassified
rows age out within the 7-day TTL. No backfill.

## Classification

New `internal/classify` package. Input: kind, sub-resource, operation, and the
parsed diff (`internal/diff` Change list). Rules, in order:

1. **Kind-based:** `Role`, `ClusterRole`, `RoleBinding`, `ClusterRoleBinding`,
   `ServiceAccount` → `rbac`. `NetworkPolicy`, `Ingress` → `network`.
   `ConfigMap`/`Secret` with changes under `data`/`stringData`/`binaryData` →
   `config-data`.
2. **Path-based (workloads):** the diff compares arrays as whole values, so
   any container change appears as a single `replace` at
   `spec.template.spec.containers` (or `spec.containers` for bare Pods). The
   classifier diffs the old/new container arrays element-wise, matched by
   container name, and emits `image`, `env`, and/or `resources` accordingly.
   `spec.replicas` change or sub-resource `scale` → `scale`. `Service` spec
   changes → `network`.
3. **Noise classes:** all diff paths under `status.` or within
   `metadata.resourceVersion`, `metadata.generation`, `metadata.managedFields`
   → `status-only`. Only label/annotation paths → `metadata-only`.
4. **Fallback:** anything else → `other`.

DELETE and CREATE operations classify by kind (rules 1 and 2 where the single
object body allows) and are never `status-only`.

## Scoring (query time)

Per event, deterministic and explainable:

```
score = 100 × recency × W_class × W_actor × W_rarity   (capped at 100)
```

Each factor emits a human-readable reason chip shown in the UI.

- **Recency:** exponential decay with a **30-minute half-life** measured
  backwards from the incident time. Events *after* the incident time decay
  with a 4× faster half-life (kept for clock skew, heavily discounted).
- **Class weight (max across the event's classes):**
  `image` 1.5 · `rbac`/`network`/`config-data` 1.4 · `env`/`resources` 1.2 ·
  `scale` 1.0 · `other` 0.8 · `metadata-only` 0.4 · `status-only` 0.1.
  `image` is highest because bad deploys are the most common incident cause
  for this team.
- **Actor weight:** `human` 1.5 · `serviceaccount` 1.0 · `unknown` 1.0 ·
  `system` 0.6.
- **Rarity (from `resource_change_stats`, 30-day window):** first change in
  ≥14 days → 1.5 · normal → 1.0 · churner (>50 changes/day average) → **0.2**.
  Missing stats → 1.0 (neutral). This is the primary defense against
  resources that change every few minutes drowning out the culprit.

All weights are named constants in a single file for easy tuning.

**Grouping:** results are grouped by resource identity (cluster, namespace,
kind, name). A group's rank is its best-scoring event; the group shows the
event count in the window and the latest event time, collapsing redeploy
loops into one row.

## API

`GET /api/incident` on the existing dashboard API (`internal/dashboardapi`):

**Params:**

- `cluster` (required)
- `at` (RFC3339, default: now)
- `lookback` (duration, default `1h`, max `24h`)
- `limit` (ranked resource groups, default 50, max 100)

Missing/invalid params → `400` with a JSON error message.

**Response:**

```json
{
  "incident": {"cluster": "...", "at": "...", "lookback": "1h"},
  "suspects": [
    {
      "namespace": "payments", "kind": "Deployment", "name": "checkout",
      "score": 87,
      "reasons": ["image change", "human via kubectl", "first change in 12d"],
      "event_count": 3,
      "latest_event_time": "...",
      "events": [
        {"event_id": "...", "event_time": "...", "operation": "UPDATE",
         "classes": ["image", "env"], "actor_type": "human",
         "user_name": "gaurav.rautela"}
      ]
    }
  ]
}
```

Event IDs link to the existing `GET /api/events/{id}` for full diff
drill-down.

**Query shape:** one ClickHouse query fetches the window's events *without*
the heavy `old_object`/`new_object` columns; a second fetches 30-day stats
for the touched resources. Scoring and grouping happen in Go — window sizes
(one cluster, ≤24h) keep this small.

## UI — new Incident Lens page

New `/incident` route in the SPA (`web/src`), linked from the main nav.

- **Top bar:** cluster dropdown (populated from `/api/facets`), incident time
  picker (default "now"), lookback selector (15m / 1h / 6h / 24h).
- **Results:** ranked table — score badge, resource identity, reason chips,
  "N changes in window", latest event time. Expanding a row lists its events;
  clicking an event opens the existing event-detail view with the full diff.
- **States:** empty window → hint to widen the lookback; stats not yet
  accumulated → scores render without rarity chips; API errors → inline error
  with retry.
- Reuses the existing table, chip, and time-formatting components/patterns
  from dashboard v2.

## Error handling

- API validates params and returns structured `400`s; ClickHouse errors →
  `500` with a generic message (details logged server-side).
- Classifier failures (unparseable diff/objects) fall back to `other` and
  never block ingest — classification errors are logged, the event is stored
  regardless.
- Scoring tolerates missing stats, empty classes (treated as `other`), and
  default actor_type (treated as neutral weight 1.0).

## Testing

- **Classifier:** table-driven unit tests with realistic diffs, including the
  containers-array element-wise case, ConfigMap data changes, RBAC kinds,
  status-only noise, and CREATE/DELETE handling.
- **Scoring:** unit tests for decay, weight application, churn dampening,
  grouping, and cap behavior.
- **API:** handler tests against a fake store — param validation, 400s,
  response shape, ordering.
- **Storage:** migration test (new columns + stats table DDL), stats query
  tests, following existing ClickHouse test patterns.
- **SPA:** vitest component tests for the incident page (input controls,
  ranked rendering, expansion, empty/error states), matching existing tests.
