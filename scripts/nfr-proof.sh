#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="${ROOT}/docs/release/benchmarks"
mkdir -p "${OUT_DIR}"

RUN_AT="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
OUT_FILE="${OUT_DIR}/nfr-proof-${RUN_AT}.txt"
WORKERS="${WORKERS:-10000}"
JOBS="${JOBS:-20000}"
TARGET_UTIL="${TARGET_UTIL:-0.70}"

echo "SPLAI NFR proof at ${RUN_AT}" | tee "${OUT_FILE}"
echo "workers=${WORKERS} jobs=${JOBS} target_util=${TARGET_UTIL}" | tee -a "${OUT_FILE}"

(
  cd "${ROOT}"
  SPLAI_RUN_NFR_PROOF=1 \
  SPLAI_NFR_WORKERS="${WORKERS}" \
  SPLAI_NFR_JOBS="${JOBS}" \
  SPLAI_NFR_TARGET_UTILIZATION="${TARGET_UTIL}" \
  GOTOOLCHAIN=local GOCACHE="${ROOT}/.gocache" \
  go test ./internal/scheduler -run TestNFRUtilizationProof -count=1 -v
) | tee -a "${OUT_FILE}"

echo "wrote ${OUT_FILE}"
