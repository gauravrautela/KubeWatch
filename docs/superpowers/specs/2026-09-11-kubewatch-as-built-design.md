# KubeWatch — As-Built Design

**Date:** 2026-09-11
**Status:** Approved
**Relationship:** Describes `main` @ `b8ac95e` as built, checked against the code.
The dated specs remain the record of original intent:
`docs/superpowers/specs/2026-07-01-kubewatch-change-audit-design.md`,
`docs/superpowers/specs/2026-07-03-kubewatch-dashboard-design.md`,
`docs/superpowers/specs/2026-07-03-kubewatch-dashboard-spa-design.md`,
`docs/superpowers/specs/2026-07-06-docker-ci-pipeline-design.md`,
`docs/superpowers/specs/2026-07-07-dashboard-v2-design.md`,
`docs/superpowers/specs/2026-07-07-table-include-exclude-design.md`,
`docs/superpowers/specs/2026-07-08-incident-lens-design.md`.

## 0. Purpose

This document describes what KubeWatch actually does on `main` @ `b8ac95e`: its architecture, components, data flow, key decisions and failure modes, each checked against the code rather than taken from the dated specs, which have drifted from it in places. It proposes no change; the gaps it records in §8 are not addressed here.

## 1. Target runtime and versions

| Layer | Target | Source of truth |
|---|---|---|
| Go services (agent, hub, dashboard) | **Go 1.26** (`go 1.26.2`), stdlib `net/http` with 1.22+ method/pattern routing | `go.mod`; built on `golang:1.26`, run on `gcr.io/distroless/static:nonroot`, cross-built with `--platform=$BUILDPLATFORM` |
| Kubernetes API | **`admission.k8s.io/v1`**, `k8s.io/api` / `apimachinery` **v0.36.2** | `go.mod`, `deploy/validatingwebhookconfiguration.yaml` |
| Platform | Any conformant cluster, **including managed control planes (ACK)**: no apiserver flags, no audit-log access needed | design constraint from the 2026-07-01 spec |
| Storage | **ClickHouse 24.8** (single node), `clickhouse-go/v2` v2.47, native protocol :9000 | `deploy/clickhouse.yaml` (README's local-run `:latest` is dev-only) |
| Web SPA | **React 18.3, TypeScript 5.6, Vite 5.4**, built on **Node 22**, tested with vitest; bundled into the dashboard image | `web/package.json`, `Dockerfile.dashboard` |

## 2. Architecture

```
 spoke cluster (×N)                              hub cluster
┌───────────────────────────┐        ┌─────────────────────────────────────────────┐
│ kube-apiserver            │        │  hub (×2)                                    │
│   │ validating admission  │ HTTPS  │  POST /v1/events ─ auth ─ diff ─ classify    │
│   ▼ (Ignore, 2s timeout)  │ bearer │        │                                     │
│ agent (×2) :8443/webhook ─┼───────►│        ▼ Batcher (500 / 2s)                  │
│   parse → filter → buffer │ token  │  ClickHouse 24.8 (1 node, 20Gi)              │
│   → forward every 2s      │        │   change_events ──MV──► resource_change_stats│
└───────────────────────────┘        │        ▲                                     │
                                     │  dashboard (×2) :8081  /api/* + SPA          │
                                     │        ▲  (no auth: must sit behind SSO)     │
                                     └────────┼─────────────────────────────────────┘
                                          browser
```

**Why this shape fits.** The job is to record *what changed, who changed it, and where* across many clusters, without control-plane access. The AdmissionReview is the only in-cluster source that carries the actor (`userInfo`), the before (`oldObject`) and the after (`object`) in one payload. Apiserver audit logs need control-plane configuration that managed Kubernetes does not give you. Informers/watches see state but not the actor or the prior object. A central columnar store fits append-only, time-filtered audit rows. Hub-and-spoke keeps each spoke to one small stateless Deployment that needs **no Kubernetes RBAC** (the apiserver calls the agent).

## 3. Components and responsibilities

| Component | Code | Responsibility | Deliberately does not |
|---|---|---|---|
| **Agent** (per spoke, 2 replicas) | `cmd/agent`, `internal/webhook`, `internal/buffer`, `internal/forward` | Serve `/webhook` over TLS. Parse `AdmissionReview` into `event.ChangeEvent`, with an agent-generated UUID `event_id`. Drop `EXCLUDE_KINDS` (default `classify.NoiseKinds`: Lease, Event, Endpoints, EndpointSlices, access/token reviews). **Always answer `allowed: true`.** Hold events in a bounded 10k FIFO that drops the oldest when full. Every 2s, POST the drained batch (3 attempts, 0/200/400ms backoff, 10s timeout each). Log a `received/forwarded/dropped` heartbeat every 60s. | Diff, classify, talk to ClickHouse, need any k8s API permission |
| **Hub / ingest** (2 replicas, stateless) | `cmd/hub`, `internal/ingest`, `internal/diff`, `internal/classify`, `internal/storage` (write) | Map the bearer token to a cluster (static `AGENT_TOKENS`), and **overwrite `cluster`** so an agent cannot claim another cluster. Compute the structured diff. Derive `change_class` and `actor_type`. Enqueue into the Batcher (channel of 1000; flush at 500 events or every 2s; 3 insert retries). Run idempotent migrations at startup. | Serve reads |
| **ClickHouse** (StatefulSet ×1) | schema in `internal/storage/clickhouse.go` | `change_events`: MergeTree, partitioned by month, `ORDER BY (cluster, namespace, kind, name, event_time)`, **TTL 7 days**, bloom-filter index on `user_name`. `resource_change_stats`: SummingMergeTree, **30-day TTL**, fed by an MV that excludes dry-run rows; it powers rarity/churn for the incident lens. | Replicate or back up |
| **Dashboard API + SPA** (2 replicas) | `cmd/dashboard`, `internal/dashboardapi`, `internal/storage` (read), `internal/rank`, `web/` | Read-only: `GET /api/events` (filters plus cursor/`since` pagination, limit ≤200), `/api/events/{id}`, `/api/activity`, `/api/facets`, `/api/incident` (window query plus deterministic Go-side scoring and grouping in `internal/rank`). Serves the SPA from `SPA_DIR`. | Write, authenticate users |

**Boundaries.** Only the hub writes and only the dashboard reads; the agent never touches storage. Diffing and classification exist **only at the hub**, so there is one implementation and agents stay thin and tolerant of version skew. The only shared code is the `NoiseKinds` constant: the agent imports `internal/classify` so that its default exclusions and the ranker's noise floor use the same set. That overlap is intentional.

## 4. Data flow

1. A write reaches a spoke's apiserver. After mutating admission and schema validation, the apiserver calls the agent's validating webhook, and waits at most 2s.
2. The agent parses the request and drops excluded kinds. It appends the event to its buffer under a mutex and immediately returns `allowed: true`.
3. Within about 2s, the flush loop drains the buffer and POSTs `{"events":[…]}` (JSON) to `HUB_URL` with `Authorization: Bearer <CLUSTER_TOKEN>`.
4. The hub authenticates the token (401 if unknown), sets `cluster`, and computes `diff`, `change_class` and `actor_type` per event. It enqueues each event and returns **202**.
5. The Batcher inserts within 2s or at 500 events. The MV updates `resource_change_stats` as part of the same insert. Rows that fail `validForInsert` (bad UUID/source/operation) are skipped and logged per row.
6. The dashboard queries ClickHouse per request. The live feed uses the `since` parameter (`web/src/useLiveFeed.ts`); event IDs drill down to the full diff and old/new objects.

Typical end-to-end latency is about 4–5s (two 2s flush intervals).

## 5. Significant decisions and their trade-offs

1. **Capture through a validating admission webhook with `failurePolicy: Ignore` and a 2s timeout.**
   - *Gains:* the actor, the before and the after in one payload; works on managed Kubernetes; can never block a deploy.
   - *Gives up:* completeness. A down or slow agent means the change is silently missed, with no backfill. Admission also runs **before persistence**, so a write later denied by another webhook or lost to an update conflict is still recorded, as a *phantom* event.
   - *Rejected:* apiserver audit logs (no control-plane access), informers (no actor, no prior object), and `Fail` (an audit tool must not be able to break the cluster).
2. **Diff and classification at the hub, at ingest time.**
   - *Gains:* one implementation, dumb agents, reads never diff.
   - *Gives up:* hub CPU per event, and a classifier change never reclassifies stored rows. Old rows age out within the 7-day TTL; there is no backfill.
   - *Rejected:* diffing at the agent (N copies to upgrade) and diffing at query time (repeated cost on every read).
3. **Plain append-only `MergeTree`, with no deduplication on write or read.**
   - *Gains:* the simplest insert path and no merge-time semantics.
   - *Gives up:* delivery is at-least-once. A POST that the hub accepted but whose response timed out is retried, producing **two rows with the same `event_id`**. No read query uses `FINAL`, `LIMIT 1 BY` or `argMax`. Duplicates inflate activity counts, incident `event_count` and churn stats.
   - *Rejected:* the July spec's `ReplacingMergeTree` option.
4. **In-memory, lossy buffering at both hops instead of a durable queue.**
   - *Gains:* no Kafka or NATS to operate, and bounded memory.
   - *Gives up:* anything more than a brief hub or ClickHouse outage loses data, and a pod killed beyond its drain window loses its buffer.
   - *Rejected:* a durable queue (deferred in the July spec).
5. **Static per-cluster bearer tokens; a dashboard with no built-in auth.**
   - *Gains:* trivial setup, and the hub (not the agent) binds cluster identity.
   - *Gives up:* rotating a token means editing the Secret and restarting the hub; there is no mTLS. Dashboard access control depends entirely on an external SSO/ingress proxy that is not part of this repo.

**Retention:** 7 days of raw events and 30 days of stats. KubeWatch is therefore an operational change tracker, not a long-term compliance archive. The 20Gi PVC is sized for that.

## 6. Failure modes

| Failure | What happens today | Survived? |
|---|---|---|
| One agent replica dies | The other serves the webhook; the dead pod's in-memory buffer is lost | Yes, minus that buffer |
| All agents down, or webhook TLS/`caBundle` wrong | Fails open: cluster writes proceed, changes are **not recorded**, and nothing shows it except the absence of rows | Cluster yes; audit completeness **no** |
| Agent slow or unreachable | Every matching write in the cluster waits **up to 2s** before the apiserver gives up. The rules match all resources and all operations, so this latency is cluster-wide | Degraded |
| Hub refuses connections or returns 5xx/401 | 3 attempts in about 0.6s, then the batch is **dropped** with a `warn` log. The heartbeat's `dropped` counts only buffer overflow, **not** forward failures | **No** beyond ~1s outages |
| Hub black-holed (connections hang) | Up to 3×10s per batch, while the flush loop is blocked. The buffer fills and, at 10k, drops the oldest (metered) | **No**, partially metered |
| Hub Batcher channel full (1000) | Event dropped and counted at the hub, but the handler **still returns 202**, so the agent believes the batch was delivered | **No**; visible only in the hub's 60s drop log |
| ClickHouse down when the hub starts | `log.Fatalf`, and the hub goes into CrashLoopBackOff | Recovers when ClickHouse returns; agents lose data meanwhile |
| ClickHouse down while running | 3 insert retries (0/200/400ms), then the batch is dropped and counted | **No** |
| ClickHouse node or PVC lost | Single replica, no backup: all history is lost | **No** |
| Duplicate delivery | Duplicate rows (see decision 3) | Data is kept, counts are wrong |
| Phantom events | A denied or conflicted write is recorded as if it happened | **No** |
| Oversized ingest body | No `MaxBytesReader`, so the whole batch is decoded in memory. A burst near the 10k buffer cap made of large objects (ConfigMaps up to 1MiB) can OOM the hub (512Mi limit) | **No** |
| Agent graceful shutdown | Final drain sends under `context.Background()`, so it can block up to ~30s against a hung hub; Kubernetes' default grace period is 30s | Mostly |
| Two hub replicas migrating at once | Every statement is `IF NOT EXISTS`, `ADD COLUMN IF NOT EXISTS` or an idempotent `MODIFY TTL` | Yes |
| **Sensitive data** | `Secret` is not in `NoiseKinds` and nothing redacts it, so Secret `data` (base64) is captured, forwarded and stored in `old_object`, `new_object` and `diff`. It is served by a dashboard with no built-in auth | **No: the most serious gap** |
| Observability | No `/metrics` (no Prometheus or expvar). Loss is visible only in logs: the agent heartbeat every 60s, and the hub drop count when it is above 0 | Alerting requires log-based rules |

## 7. What this design is NOT building

This document adds nothing new. Not built today, explicitly:

- a reconcile/informer source (the `source` enum reserves `'reconcile'`)
- a durable queue
- mTLS
- dashboard authentication
- alerting or notifications
- a Prometheus metrics endpoint
- backfill
- HA ClickHouse or backups
- rollback assistance

It also includes **no fixes for the gaps in §6**. Those are recorded as known gaps in §8, not scoped work.

## 8. Known gaps (not addressed by this document)

In order of severity:

1. **Secret contents are stored in clear (base64) and exposed to anyone who reaches the dashboard.**
2. **The hub acknowledges events it has dropped** (202 on a full Batcher), so the agent cannot retry.
3. **The agent gives up on a batch after about 0.6s of retries.** Forward-failure drops are logged but not counted in the heartbeat.
4. **Retried deliveries produce duplicate rows** that no read collapses.
5. **The ingest request body is unbounded.**
6. **Documentation is stale:** the `deploy/clickhouse.yaml` comment says "1d TTL"; the schema is 7 days.

## 9. Verification basis

Checked on 2026-09-11 against `main` @ `b8ac95e`:

- **Read:** `README.md`, `deploy/README.md`, the change-audit and incident-lens specs, `cmd/{agent,hub,dashboard}`, `internal/{ingest,buffer,forward,webhook}` handlers, the storage schema and migrations, the webhook and deploy manifests. The code was searched for metrics, redaction, read-side dedup and request-body limits; none exist.
- **Ran:** `go vet ./...` is clean. `go test -count=1 ./...` passes all 11 packages. **However, the 3 ClickHouse integration tests in `internal/storage` skip** when `CLICKHOUSE_DSN` is unset, so the storage read and write paths were not exercised against a real ClickHouse.
- **Not run:** the web vitest suite, and no end-to-end run against a cluster.
