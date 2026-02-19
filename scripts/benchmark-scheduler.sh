#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="${ROOT}/docs/release/benchmarks"
mkdir -p "${OUT_DIR}"

RUN_AT="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
OUT_FILE="${OUT_DIR}/scheduler-${RUN_AT}.txt"

echo "SPLAI scheduler benchmark run at ${RUN_AT}" | tee "${OUT_FILE}"
echo "command: go test ./internal/scheduler -run Benchmark -bench . -benchmem -count=1" | tee -a "${OUT_FILE}"
(
  cd "${ROOT}"
  GOTOOLCHAIN=local GOCACHE="${ROOT}/.gocache" go test ./internal/scheduler -run Benchmark -bench . -benchmem -count=1
) | tee -a "${OUT_FILE}"

echo "wrote ${OUT_FILE}"
