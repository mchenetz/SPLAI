#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="${ROOT}/docs/release/benchmarks"
mkdir -p "${OUT_DIR}"

WORKERS="${WORKERS:-1000}"
RUN_AT="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
OUT_FILE="${OUT_DIR}/scale-proof-${RUN_AT}.txt"

echo "SPLAI scale proof benchmark at ${RUN_AT}" | tee "${OUT_FILE}"
echo "workers=${WORKERS}" | tee -a "${OUT_FILE}"
(
  cd "${ROOT}"
  SPLAI_BENCH_WORKERS="${WORKERS}" GOTOOLCHAIN=local GOCACHE="${ROOT}/.gocache" go test ./internal/scheduler -run Benchmark -bench BenchmarkAssignmentPath -benchmem -count=1
) | tee -a "${OUT_FILE}"

NS_PER_OP="$(awk '/BenchmarkAssignmentPath/ {print $(NF-5)}' "${OUT_FILE}" | tail -n 1 || true)"
echo "ns_per_op=${NS_PER_OP}" | tee -a "${OUT_FILE}"
echo "wrote ${OUT_FILE}"
