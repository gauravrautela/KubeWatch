# KubeWatch — Installation Guide

KubeWatch is a hub-and-spoke system:

- **Hub cluster** runs ClickHouse (storage), the **hub** (ingest API), and the
  **dashboard** (read-only UI). Install once.
- **Spoke clusters** each run an **agent** — a ValidatingWebhook that captures
  every CREATE/UPDATE/DELETE and POSTs it to the hub. Install per cluster.
  (A spoke can be the hub cluster itself.)

```
spoke cluster                        hub cluster
┌─────────────────────┐            ┌──────────────────────────────┐
│ API server ──HTTPS──▶ agent ─────▶ hub ──▶ ClickHouse ◀── dashboard │
│  (admission webhook) │  bearer    │ :8080      :9000        :8081 │
└─────────────────────┘  token     └──────────────────────────────┘
```

Two independent credentials are involved — don't confuse them:

| Credential | Secures | Lives in |
|---|---|---|
| `kubewatch-agent-tls` (TLS cert/key) | API server → agent webhook call | Secret on each spoke; CA in the webhook's `caBundle` |
| `CLUSTER_TOKEN` (bearer token) | agent → hub ingest | Secret on the spoke + `AGENT_TOKENS` map on the hub |

---

## Part 1 — Hub cluster (ClickHouse + hub + dashboard)

Apply order: `clickhouse.yaml` → `hub.yaml` → `dashboard.yaml`.

### 1. ClickHouse password secret (out-of-band — not committed)

```bash
kubectl apply -f deploy/clickhouse.yaml --dry-run=client -o name  # sanity check
kubectl create namespace kubewatch --dry-run=client -o yaml | kubectl apply -f -
kubectl create secret generic clickhouse-credentials -n kubewatch \
  --from-literal=password="$(openssl rand -hex 20)"
```

### 2. ClickHouse

```bash
kubectl apply -f deploy/clickhouse.yaml
kubectl rollout status statefulset/clickhouse -n kubewatch
```

Single-node StatefulSet, 20Gi PVC, native protocol on
`clickhouse.kubewatch.svc:9000`. No manual schema step: the hub runs
`CREATE TABLE IF NOT EXISTS change_events` on startup.

### 3. Register agent tokens, then install the hub

For each spoke cluster, generate a token and add a `token=cluster-name` pair to
the `AGENT_TOKENS` value in the `kubewatch-hub-config` Secret in `hub.yaml`
(comma-separated for multiple spokes):

```bash
openssl rand -hex 32   # one per spoke; you'll reuse it in Part 2 step 2
```

```bash
kubectl apply -f deploy/hub.yaml
kubectl rollout status deployment/kubewatch-hub -n kubewatch
```

The hub's ClickHouse DSN is composed in the Deployment from the
`clickhouse-credentials` Secret — no password ever sits in `hub.yaml`.

### 4. Dashboard

```bash
kubectl apply -f deploy/dashboard.yaml
kubectl rollout status deployment/kubewatch-dashboard -n kubewatch
```

> ⚠️ The dashboard has **no built-in auth** and exposes audit data. Put it
> behind an authenticating ingress/SSO proxy; never expose the ClusterIP
> directly. For a quick look: `kubectl port-forward svc/kubewatch-dashboard
> 8081:8081 -n kubewatch` → http://localhost:8081

### 5. Expose the hub to spokes (skip if agents run on the hub cluster)

Agents on other clusters need to reach the hub's `/v1/events`. Add an
Ingress/LoadBalancer in front of `svc/kubewatch-hub:8080`, TLS-terminated
(agents send a bearer token — never over plaintext across clusters).

---

## Part 2 — Each spoke cluster (agent)

Apply order: `agent-rbac.yaml` → TLS secret → `agent.yaml` →
`validatingwebhookconfiguration.yaml` (webhook **last**, after the agent is Ready).

### 1. Namespace, ServiceAccount, token Secret

Set `CLUSTER_TOKEN` in `agent-rbac.yaml` to the token you registered for this
cluster in the hub's `AGENT_TOKENS` (Part 1 step 3), then:

```bash
kubectl apply -f deploy/agent-rbac.yaml
```

The agent needs **no** Kubernetes API permissions (the API server calls *it*);
the optional read-only ClusterRole in that file stays commented out.

### 2. Webhook TLS cert (`kubewatch-agent-tls`)

The API server calls admission webhooks over HTTPS only, and verifies the
agent's cert against the webhook's `caBundle`. A dedicated self-signed CA is
fine — it does **not** need to be the cluster CA:

```bash
# Dedicated CA for this webhook (keep ca.key safe for future rotation)
openssl req -x509 -newkey rsa:2048 -nodes -keyout ca.key -out ca.crt \
  -days 3650 -subj "/CN=kubewatch-agent-ca"

# Serving cert — SAN must be the Service DNS name the API server dials
openssl req -newkey rsa:2048 -nodes -keyout tls.key -out tls.csr \
  -subj "/CN=kubewatch-agent.kubewatch.svc"
printf "subjectAltName=DNS:kubewatch-agent.kubewatch.svc,DNS:kubewatch-agent.kubewatch.svc.cluster.local\nextendedKeyUsage=serverAuth\nbasicConstraints=CA:FALSE\n" > ext.cnf
openssl x509 -req -in tls.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out tls.crt -days 3650 -extfile ext.cnf

kubectl create secret tls kubewatch-agent-tls -n kubewatch \
  --cert=tls.crt --key=tls.key
```

### 3. Agent Deployment

In `agent.yaml`, set `HUB_URL` to the full ingest endpoint (path included):

- same cluster as the hub: `http://kubewatch-hub.kubewatch.svc:8080/v1/events`
- different cluster: the externally exposed HTTPS URL, e.g.
  `https://kubewatch-hub.example.com/v1/events`

```bash
kubectl apply -f deploy/agent.yaml
kubectl rollout status deployment/kubewatch-agent -n kubewatch
```

### 4. ValidatingWebhookConfiguration

Set `caBundle` in `validatingwebhookconfiguration.yaml` to the base64 CA cert:

```bash
base64 -i ca.crt | tr -d '\n'   # paste as the caBundle value
kubectl apply -f deploy/validatingwebhookconfiguration.yaml
```

`failurePolicy: Ignore` means a broken webhook never blocks cluster operations
— it just silently misses changes, which is why the agent runs 2 replicas.

### 5. Verify end-to-end

```bash
kubectl get pods -n kubewatch                             # agent pods Ready
kubectl create configmap kubewatch-smoke -n default       # make any change
kubectl delete configmap kubewatch-smoke -n default
```

The change should appear in the dashboard within seconds. If not, check agent
logs (`kubectl logs deploy/kubewatch-agent -n kubewatch`) for hub auth/URL
errors, and hub logs for token rejections.

---

## Troubleshooting quick hits

| Symptom | Cause / fix |
|---|---|
| `FailedMount ... secret "kubewatch-agent-tls" not found` | TLS Secret not created — Part 2 step 2 |
| Agent logs `401`/auth errors to hub | `CLUSTER_TOKEN` doesn't match a hub `AGENT_TOKENS` entry |
| Changes not appearing, no errors | `caBundle` doesn't match the CA that signed the serving cert, or cert SAN ≠ `kubewatch-agent.kubewatch.svc` (webhook fails open) |
| Hub `CrashLoopBackOff` on start | ClickHouse unreachable or `clickhouse-credentials` missing |
| `exec format error` in pod logs | Image built for the wrong CPU arch — rebuild via the CI pipeline |
