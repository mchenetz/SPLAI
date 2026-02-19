#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
DIST_DIR="${ROOT_DIR}/dist"
BIN_DIR="${BIN_DIR:-/usr/local/bin}"

mkdir -p "${DIST_DIR}"

echo "building splai worker binaries..."
(
  cd "${ROOT_DIR}"
  go build -o "${DIST_DIR}/splai-worker" ./worker/cmd/worker-agent
  go build -o "${DIST_DIR}/splaictl" ./cmd/splaictl
)

echo "installing to ${BIN_DIR} (may require sudo)..."
install -m 0755 "${DIST_DIR}/splai-worker" "${BIN_DIR}/splai-worker"
install -m 0755 "${DIST_DIR}/splaictl" "${BIN_DIR}/splaictl"

echo "installed: ${BIN_DIR}/splai-worker and ${BIN_DIR}/splaictl"
