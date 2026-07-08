# KubeWatch

Hub-and-spoke Kubernetes change auditing. Agents host a validating webhook in
each cluster and forward changes to a central hub that stores them in ClickHouse.

## Run locally

Start ClickHouse:

    docker run -d --name kw-ch -p 9000:9000 clickhouse/clickhouse-server:latest

Start the hub:

    CLICKHOUSE_DSN="clickhouse://default:@localhost:9000/default" \
    AGENT_TOKENS="devtoken=local-cluster" \
    LISTEN_ADDR=":8080" \
    go run ./cmd/hub

The hub creates the `change_events` table on startup.

By default the hub serves plain HTTP. Set `TLS_CERT_FILE`/`TLS_KEY_FILE` to
have it serve TLS directly. In production the hub **must** be fronted by TLS
(either via these env vars or by terminating TLS at an ingress/proxy in front
of it) because agents authenticate with a bearer token that must never travel
in cleartext.

## Deploy an agent

The agent needs a TLS serving cert trusted by the API server (use cert-manager
or a generated CA), then:

- Deploy the agent Deployment + Service in the `kubewatch` namespace.
- Apply `deploy/validatingwebhookconfiguration.yaml` with the matching `caBundle`.
- Set `HUB_URL`, `CLUSTER_TOKEN`, `TLS_CERT_FILE`, `TLS_KEY_FILE`.

## Configuration

Hub: `CLICKHOUSE_DSN`, `LISTEN_ADDR`, `AGENT_TOKENS` (`token=cluster,token2=cluster2`), `TLS_CERT_FILE`, `TLS_KEY_FILE` (optional; enables direct TLS — see above).
Agent: `WEBHOOK_ADDR`, `HUB_URL`, `CLUSTER_TOKEN`, `TLS_CERT_FILE`, `TLS_KEY_FILE`,
`EXCLUDE_KINDS` (comma-separated kinds dropped before forwarding; unset = built-in
noisy-kind list — Leases, Events, EndpointSlices, auth reviews; `none` = capture everything).

## Dashboard (read path)

The dashboard API serves the audit trail over HTTP:

    CLICKHOUSE_DSN="clickhouse://default:@localhost:9000/default" \
    LISTEN_ADDR=":8081" \
    go run ./cmd/dashboard

Endpoints: `GET /api/events` (filters: cluster, namespace, kind, name, user,
operation, from, to; pagination: cursor, since, limit≤200), `GET
/api/events/{id}`, `GET /api/activity?bucket=minute|hour|day`, `GET /api/facets`,
`GET /healthz`. Set `SPA_DIR` to serve the built SPA. Set `TLS_CERT_FILE`/`TLS_KEY_FILE`
for direct TLS.

**Security:** the dashboard exposes sensitive audit data and has no built-in auth.
In production it MUST run behind an authenticating ingress/SSO proxy.
