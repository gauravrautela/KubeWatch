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

## Deploy an agent

The agent needs a TLS serving cert trusted by the API server (use cert-manager
or a generated CA), then:

- Deploy the agent Deployment + Service in the `kubewatch` namespace.
- Apply `deploy/validatingwebhookconfiguration.yaml` with the matching `caBundle`.
- Set `HUB_URL`, `CLUSTER_TOKEN`, `TLS_CERT_FILE`, `TLS_KEY_FILE`.

## Configuration

Hub: `CLICKHOUSE_DSN`, `LISTEN_ADDR`, `AGENT_TOKENS` (`token=cluster,token2=cluster2`).
Agent: `WEBHOOK_ADDR`, `HUB_URL`, `CLUSTER_TOKEN`, `TLS_CERT_FILE`, `TLS_KEY_FILE`.
