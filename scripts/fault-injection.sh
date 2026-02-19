#!/usr/bin/env bash
set -euo pipefail

NS="${1:-splai-system}"

echo "[fault-injection] namespace=${NS}"
echo "[1/3] Restarting Redis deployment"
kubectl -n "${NS}" rollout restart deployment/splai-redis
kubectl -n "${NS}" rollout status deployment/splai-redis --timeout=180s

echo "[2/3] Restarting Postgres deployment"
kubectl -n "${NS}" rollout restart deployment/splai-postgres
kubectl -n "${NS}" rollout status deployment/splai-postgres --timeout=180s

echo "[3/3] Restarting one worker pod (if present)"
POD="$(kubectl -n "${NS}" get pods -l app=splai-worker -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
if [[ -n "${POD}" ]]; then
  kubectl -n "${NS}" delete pod "${POD}"
else
  echo "no worker pod found, skipping"
fi

echo "fault injection sequence complete"
