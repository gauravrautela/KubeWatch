#!/usr/bin/env bash
# KubeWatch hub-stack installer: ClickHouse -> hub -> dashboard.
# Run against the HUB cluster. See deploy/README.md Part 1.
#
# Usage:
#   ./install-hub.sh [-d MANIFEST_DIR] [-y]
#
#   -d MANIFEST_DIR  directory with the filled-in yamls
#                    (default: deploy/test if it exists, else deploy)
#   -y               skip the kubectl-context confirmation prompt
#
# The clickhouse-credentials Secret is created with a random password if it
# does not already exist (it is intentionally never stored in a yaml).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NS=kubewatch
MANIFEST_DIR=""
ASSUME_YES=false

while getopts "d:y" opt; do
  case "$opt" in
    d) MANIFEST_DIR="$OPTARG" ;;
    y) ASSUME_YES=true ;;
    *) echo "usage: $0 [-d MANIFEST_DIR] [-y]" >&2; exit 2 ;;
  esac
done

if [[ -z "$MANIFEST_DIR" ]]; then
  if [[ -d "$SCRIPT_DIR/test" ]]; then MANIFEST_DIR="$SCRIPT_DIR/test"; else MANIFEST_DIR="$SCRIPT_DIR"; fi
fi

for f in clickhouse.yaml hub.yaml dashboard.yaml; do
  [[ -f "$MANIFEST_DIR/$f" ]] || { echo "ERROR: $MANIFEST_DIR/$f not found" >&2; exit 1; }
done

# Refuse to ship placeholders: hub.yaml must have real AGENT_TOKENS.
if grep -qE '<TOKEN>|<CLUSTER_NAME>' "$MANIFEST_DIR/hub.yaml"; then
  echo "ERROR: $MANIFEST_DIR/hub.yaml still contains AGENT_TOKENS placeholders." >&2
  echo "Generate a token per spoke (openssl rand -hex 32), fill AGENT_TOKENS," >&2
  echo "and keep the filled-in copy under deploy/test/ (gitignored)." >&2
  exit 1
fi

echo "Manifests:      $MANIFEST_DIR"
echo "kubectl context: $(kubectl config current-context)"
if ! $ASSUME_YES; then
  read -r -p "Deploy the KubeWatch HUB stack to this cluster? [y/N] " ans
  [[ "$ans" == "y" || "$ans" == "Y" ]] || { echo "Aborted."; exit 1; }
fi

echo "==> Namespace $NS"
kubectl create namespace "$NS" --dry-run=client -o yaml | kubectl apply -f -

echo "==> clickhouse-credentials Secret"
if kubectl get secret clickhouse-credentials -n "$NS" >/dev/null 2>&1; then
  echo "    already exists — keeping it (delete it first to rotate)"
else
  kubectl create secret generic clickhouse-credentials -n "$NS" \
    --from-literal=password="$(openssl rand -hex 20)"
  echo "    created with a random password"
fi

echo "==> ClickHouse"
kubectl apply -f "$MANIFEST_DIR/clickhouse.yaml"
kubectl rollout status statefulset/clickhouse -n "$NS" --timeout=300s

echo "==> Hub (runs the change_events schema migration on startup)"
kubectl apply -f "$MANIFEST_DIR/hub.yaml"
kubectl rollout status deployment/kubewatch-hub -n "$NS" --timeout=180s

echo "==> Dashboard"
kubectl apply -f "$MANIFEST_DIR/dashboard.yaml"
kubectl rollout status deployment/kubewatch-dashboard -n "$NS" --timeout=180s

echo
echo "Hub stack deployed. Verify:"
echo "  kubectl get pods -n $NS"
echo "  kubectl port-forward svc/kubewatch-dashboard 8081:8081 -n $NS   # then open http://localhost:8081"
echo
echo "REMINDERS:"
echo "  * The dashboard has NO auth — front it with an authenticating ingress/SSO proxy."
echo "  * Agents on OTHER clusters need the hub exposed via a TLS-terminated ingress/LB"
echo "    in front of svc/kubewatch-hub:8080."
