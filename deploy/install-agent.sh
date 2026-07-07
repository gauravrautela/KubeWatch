#!/usr/bin/env bash
# KubeWatch agent installer for one (spoke) cluster:
#   agent-rbac -> webhook TLS secret -> agent -> ValidatingWebhookConfiguration.
# See deploy/README.md Part 2. Prerequisite: this cluster's token is already
# registered in the hub's AGENT_TOKENS.
#
# Usage:
#   ./install-agent.sh [-d MANIFEST_DIR] [-c CERT_DIR] [-y]
#
#   -d MANIFEST_DIR  directory with the filled-in yamls
#                    (default: deploy/test if it exists, else deploy)
#   -c CERT_DIR      where the webhook CA/cert live or get generated
#                    (default: MANIFEST_DIR/agent-tls). Reused if ca.crt/tls.crt
#                    already exist; generated otherwise. KEEP ca.key for rotation.
#   -y               skip the kubectl-context confirmation prompt
#
# caBundle handling: if validatingwebhookconfiguration.yaml still has the
# <BASE64_CA_BUNDLE> placeholder, it is filled from CERT_DIR/ca.crt at apply
# time (the yaml on disk is not modified).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NS=kubewatch
SVC=kubewatch-agent
MANIFEST_DIR=""
CERT_DIR=""
ASSUME_YES=false

while getopts "d:c:y" opt; do
  case "$opt" in
    d) MANIFEST_DIR="$OPTARG" ;;
    c) CERT_DIR="$OPTARG" ;;
    y) ASSUME_YES=true ;;
    *) echo "usage: $0 [-d MANIFEST_DIR] [-c CERT_DIR] [-y]" >&2; exit 2 ;;
  esac
done

if [[ -z "$MANIFEST_DIR" ]]; then
  if [[ -d "$SCRIPT_DIR/test" ]]; then MANIFEST_DIR="$SCRIPT_DIR/test"; else MANIFEST_DIR="$SCRIPT_DIR"; fi
fi
[[ -n "$CERT_DIR" ]] || CERT_DIR="$MANIFEST_DIR/agent-tls"

for f in agent-rbac.yaml agent.yaml validatingwebhookconfiguration.yaml; do
  [[ -f "$MANIFEST_DIR/$f" ]] || { echo "ERROR: $MANIFEST_DIR/$f not found" >&2; exit 1; }
done

# Refuse to ship placeholders (caBundle is handled separately below).
if grep -q '<REPLACE_WITH_PER_CLUSTER_TOKEN>' "$MANIFEST_DIR/agent-rbac.yaml"; then
  echo "ERROR: CLUSTER_TOKEN placeholder in $MANIFEST_DIR/agent-rbac.yaml — set the" >&2
  echo "token registered for this cluster in the hub's AGENT_TOKENS." >&2
  exit 1
fi
if grep -q '<HUB_URL>' "$MANIFEST_DIR/agent.yaml"; then
  echo "ERROR: HUB_URL placeholder in $MANIFEST_DIR/agent.yaml — set the full ingest" >&2
  echo "endpoint incl. /v1/events (e.g. http://kubewatch-hub.kubewatch.svc:8080/v1/events)." >&2
  exit 1
fi

echo "Manifests:      $MANIFEST_DIR"
echo "Cert dir:       $CERT_DIR"
echo "kubectl context: $(kubectl config current-context)"
if ! $ASSUME_YES; then
  read -r -p "Deploy the KubeWatch AGENT to this cluster? [y/N] " ans
  [[ "$ans" == "y" || "$ans" == "Y" ]] || { echo "Aborted."; exit 1; }
fi

echo "==> Namespace, ServiceAccount, token Secret (agent-rbac.yaml)"
kubectl apply -f "$MANIFEST_DIR/agent-rbac.yaml"

echo "==> Webhook TLS (Secret kubewatch-agent-tls)"
mkdir -p "$CERT_DIR"
if [[ -f "$CERT_DIR/tls.crt" && -f "$CERT_DIR/tls.key" && -f "$CERT_DIR/ca.crt" ]]; then
  echo "    reusing existing certs in $CERT_DIR"
else
  echo "    generating a dedicated self-signed CA + serving cert (SAN $SVC.$NS.svc)"
  openssl req -x509 -newkey rsa:2048 -nodes -keyout "$CERT_DIR/ca.key" \
    -out "$CERT_DIR/ca.crt" -days 3650 -subj "/CN=kubewatch-agent-ca" 2>/dev/null
  openssl req -newkey rsa:2048 -nodes -keyout "$CERT_DIR/tls.key" \
    -out "$CERT_DIR/tls.csr" -subj "/CN=$SVC.$NS.svc" 2>/dev/null
  printf "subjectAltName=DNS:%s.%s.svc,DNS:%s.%s.svc.cluster.local\nextendedKeyUsage=serverAuth\nbasicConstraints=CA:FALSE\n" \
    "$SVC" "$NS" "$SVC" "$NS" > "$CERT_DIR/ext.cnf"
  openssl x509 -req -in "$CERT_DIR/tls.csr" -CA "$CERT_DIR/ca.crt" \
    -CAkey "$CERT_DIR/ca.key" -CAcreateserial -out "$CERT_DIR/tls.crt" \
    -days 3650 -extfile "$CERT_DIR/ext.cnf" 2>/dev/null
  openssl verify -CAfile "$CERT_DIR/ca.crt" "$CERT_DIR/tls.crt" >/dev/null
  echo "    generated — keep $CERT_DIR/ca.key safe for future rotation"
fi
kubectl create secret tls kubewatch-agent-tls -n "$NS" \
  --cert="$CERT_DIR/tls.crt" --key="$CERT_DIR/tls.key" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "==> Agent Deployment"
kubectl apply -f "$MANIFEST_DIR/agent.yaml"
kubectl rollout status deployment/kubewatch-agent -n "$NS" --timeout=180s

echo "==> ValidatingWebhookConfiguration (applied last, agent is Ready)"
WEBHOOK_YAML="$MANIFEST_DIR/validatingwebhookconfiguration.yaml"
if grep -q '<BASE64_CA_BUNDLE>' "$WEBHOOK_YAML"; then
  CA_B64="$(base64 < "$CERT_DIR/ca.crt" | tr -d '\n')"
  sed "s|<BASE64_CA_BUNDLE>|$CA_B64|" "$WEBHOOK_YAML" | kubectl apply -f -
else
  # yaml already carries a caBundle — make sure it matches the CA we just used.
  EXPECTED="$(base64 < "$CERT_DIR/ca.crt" | tr -d '\n')"
  if ! grep -q "$EXPECTED" "$WEBHOOK_YAML"; then
    echo "ERROR: caBundle in $WEBHOOK_YAML does not match $CERT_DIR/ca.crt." >&2
    echo "The API server would reject the agent's cert and fail OPEN (changes" >&2
    echo "silently unrecorded). Fix the caBundle or point -c at the right cert dir." >&2
    exit 1
  fi
  kubectl apply -f "$WEBHOOK_YAML"
fi

echo "==> Smoke test (webhook fails open — always verify!)"
kubectl create configmap kubewatch-smoke -n default >/dev/null
kubectl delete configmap kubewatch-smoke -n default >/dev/null
echo "    created+deleted configmap/kubewatch-smoke in ns default."
echo
echo "Agent deployed. Confirm BOTH smoke-test events appear in the dashboard."
echo "If they don't: kubectl logs deploy/kubewatch-agent -n $NS   (auth/HUB_URL errors)"
