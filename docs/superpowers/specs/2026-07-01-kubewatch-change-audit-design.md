# KubeWatch — Kubernetes Change Audit System

**Status:** Design approved
**Date:** 2026-07-01
**Author:** gaurav.rautela

## Summary

KubeWatch is a hub-and-spoke system that audits **all changes to Kubernetes
clusters**. For every change it records **what changed** (a before→after diff),
**who changed it** (the authenticated user/service account), and **where/when**
(cluster, namespace, resource, timestamp). A central backend collects events from
lightweight agents running in each monitored cluster and serves a web dashboard
for live feeds, diffs, search, and timelines.

## Goals

- Capture create/update/delete changes across many clusters with attribution.
- Show a structured before→after diff for every change.
- Provide a dashboard: live change feed, diff viewer, search/filter, and timelines.
- Hub-and-spoke topology: one central backend, N monitored (spoke) clusters.
- Work on managed Kubernetes (e.g. Alibaba Cloud ACK) with **no control-plane access**.

## Non-Goals (v1)

- Blocking or mutating changes — KubeWatch only observes; it never denies a request.
- Historical backfill / current-state baseline (arrives with reconcile, later).
- Alerting/notifications on specific changes (later).
- Durable message queue and mTLS (later; see Deferred).

## Capture Mechanism

Changes are captured in each spoke cluster via a **ValidatingWebhookConfiguration**
(validating, not mutating — we observe, we don't change). The admission
`AdmissionReview` payload uniquely provides everything needed in one place:

- `request.userInfo` → **who** (username, groups, UID, service account)
- `request.oldObject` → **before**
- `request.object` → **after** (→ diff derives from these two)
- `request.operation` → CREATE / UPDATE / DELETE
- `request.userAgent`, resource identity, timestamp

This works on managed control planes (ACK included) because the webhook is a normal
in-cluster workload — no API-server flags, no SLS, no master-node access.

**Known v1 limitation — completeness:** the webhook uses `failurePolicy: Ignore`
(fail-open) so it can never block cluster operations. Consequently, if an agent is
down, changes during that window are **missed** with no backfill. This is an
accepted v1 limitation, closed later by periodic reconcile. All drops are logged
and metered — never silent at the metrics level.

**Source-agnostic path:** the agent's internal event pipeline is source-agnostic.
The webhook is source #1; **periodic reconcile** (informer/watch with resync) plugs
in later as source #2 without reworking the pipeline, closing the gap above and
providing a current-state baseline.

## Architecture

```
   ┌─────────────── Spoke cluster (×N) ───────────────┐
   │  kube-apiserver ──admission(validating)──► Agent │
   │                                             │     │
   └─────────────────────────────────────────────┼─────┘
                                                  │ HTTPS POST (batched, token auth)
                                                  ▼
   ┌──────────────────── Hub cluster ──────────────────────┐
   │   Ingest API ──buffer/batch──► ClickHouse             │
   │        ▲                          ▲                    │
   │        │                          │                    │
   │   Dashboard API ─────────────── queries               │
   │        ▲                                               │
   │        └── Web dashboard (feed / diff / search / timeline)
   └────────────────────────────────────────────────────────┘
```

Language: **Go** for the agent, ingest API, and dashboard API.

### Components

1. **Agent** (per spoke cluster, Go)
   - Hosts the validating webhook endpoint (`failurePolicy: Ignore`, ~1–2s timeout).
   - Maps `AdmissionReview` → normalized change event, **admits immediately**
     (always `allowed: true`), enqueues into a bounded in-memory buffer.
   - Flushes buffered events as **batched HTTPS POSTs** to the hub with its cluster token.
   - Source-agnostic pipeline (reconcile pluggable later).

2. **Ingest API** (hub, Go)
   - Authenticates agents (per-cluster token), tags events with `cluster` identity.
   - Computes the structured `diff` at ingest.
   - Buffers and **batch-inserts** into ClickHouse.

3. **ClickHouse** (hub) — single source of truth for change events.

4. **Dashboard API** (hub, Go) — read API serving feed, diffs, search, timelines.

5. **Web dashboard** — UI for the views below.

## Data Model (ClickHouse)

One immutable **change event** row per captured change.

| Field | Type | Purpose |
|---|---|---|
| `event_id` | UUID | unique event id (generated at agent; idempotency) |
| `event_time` | DateTime64 | when the change happened |
| `ingested_at` | DateTime64 | when hub received it (lag visibility) |
| `cluster` | LowCardinality(String) | originating cluster (from token) — primary filter |
| `source` | Enum('webhook','reconcile') | source-agnostic; reconcile added later |
| `operation` | Enum('CREATE','UPDATE','DELETE') | verb |
| `api_group` | LowCardinality(String) | resource group |
| `api_version` | LowCardinality(String) | resource version |
| `kind` | LowCardinality(String) | resource kind |
| `namespace` | String | object namespace |
| `name` | String | object name |
| `resource_uid` | String | stable object UID |
| `sub_resource` | LowCardinality(String) | e.g. `status`, `scale` |
| `user_name` | String | who |
| `user_groups` | Array(String) | groups |
| `user_uid` | String | user uid |
| `user_agent` | String | client agent |
| `dry_run` | UInt8 | filter out dry-run noise |
| `old_object` | String (JSON) | before |
| `new_object` | String (JSON) | after |
| `diff` | String (JSON) | precomputed structured diff |

**Engine & layout:**
- `MergeTree` (append-only; audit rows never mutate).
- `PARTITION BY toYYYYMM(event_time)` — cheap retention drops.
- `ORDER BY (cluster, namespace, kind, name, event_time)` — fast per-resource
  history and cluster filtering.
- **TTL** on `event_time` for configurable retention (default 90 days).
- **Bloom-filter skip index** on `user_name` — keeps "changes by user X" fast
  despite the resource-centric primary sort.

**Diff strategy:** the agent captures `old_object`/`new_object`; the **hub computes
a structured JSON diff at ingest** (changed paths → before/after values) stored in
`diff`. Dashboard reads never diff at query time; raw objects remain for deep inspection.

## Dashboard Views

- **A. Live change feed** — real-time stream of who/what/where/when; filterable by
  cluster, namespace, kind, user, operation, time.
- **C. Diff viewer** — unified/side-by-side before→after for any change.
- **D. Search & filter** — query across all dimensions (user, resource, time window,
  operation), with **cluster as a first-class filter**.
- **B. Per-resource history** — filtered view: full change timeline of one object.
- **Timelines:**
  - **Activity histogram** — change volume over a time window (per minute/hour),
    filterable, click-to-drill into a window's events.
  - **Per-resource timeline** — a single object's chronological change track
    (v1 → v2 → v3), each entry expandable to its diff.

## Data Flow

1. Write hits a spoke's `kube-apiserver` → validating webhook fires → agent receives
   `AdmissionReview`.
2. Agent normalizes the event, **admits immediately** (never blocks), enqueues in buffer.
3. Agent flushes a **batched HTTPS POST** to the hub with its cluster token.
4. Ingest authenticates, tags `cluster`, computes `diff`, buffers, **batch-inserts**
   into ClickHouse.
5. Dashboard API queries ClickHouse to serve all views.

## Reliability & Error Handling

- **Webhook never blocks:** `failurePolicy: Ignore`, tight timeout, admit-then-record.
- **Agent buffering:** bounded in-memory queue with retry + backoff on POST failure.
  On extended hub outage the buffer may overflow; it drops oldest with a logged,
  **metered** counter (honest, visible gap — no silent loss). Closed later by reconcile.
- **Ingest batching:** size/time-based flush; retry batch on ClickHouse insert failure;
  expose health metric.
- **Idempotency:** `event_id` generated at the agent so retried batches don't
  double-insert (dedup on read, or `ReplacingMergeTree` if needed).
- **Observability:** per-hop counters (received / forwarded / dropped / inserted) and
  end-to-end lag (`ingested_at - event_time`).

## Security / Authentication

Agents authenticate to the hub with a **per-cluster bearer token** (stored as a
Secret in each spoke, sent on every request; hub validates and derives the `cluster`
identity from it). Rotation is straightforward. **This is the v1 default and is
revisitable** — mTLS can be layered on later if the security posture requires it.

## Testing Strategy

- **Agent:** unit tests mapping sample `AdmissionReview` payloads → normalized events
  (CREATE/UPDATE/DELETE, sub-resources, dry-run); buffer/retry/overflow behavior.
- **Diff engine:** table-driven unit tests over object pairs → expected structured diffs.
- **Ingest:** auth, batching, ClickHouse insert (integration test against a real
  ClickHouse container).
- **Dashboard API:** query tests against seeded ClickHouse data for each view/filter.
- **End-to-end:** kind cluster + agent + hub + ClickHouse; apply manifests; assert
  events land with correct attribution and diffs.

## Deferred (documented, not built in v1)

- Periodic reconcile (informer/watch resync) — closes downtime gaps, provides baseline.
- Durable message queue (Kafka/NATS) between agents and hub — for high throughput/burst.
- mTLS between agents and hub.
- Alerting/notifications on specific changes.
