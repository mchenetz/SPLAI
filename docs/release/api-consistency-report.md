# API Consistency Report (Draft)

Date: 2026-02-19

## Scope checked

- REST surface in gateway handlers
- OpenAPI spec: `openapi/splai-admin-task.yaml`
- JSON DTOs: `pkg/splaiapi/types.go`
- Proto contracts under `proto/splai/v1/`
- User-facing docs (`README.md`, persistent control-plane reference)

## Result

No blocking inconsistencies found for the currently implemented REST endpoints.

## Verified alignments

- Submit request supports `tenant` in REST and is documented in OpenAPI/README.
- Task listing endpoint supports `status`, `worker_id`, `limit`, `offset`.
- Dead-letter requeue supports `dry_run` request and response fields.
- Audit endpoint supports filters (`action`, `actor`, `tenant`, `result`, `from`, `to`) and `format=csv`.
- Auth scope expectations are consistently documented:
  - `operator` for admin endpoints.
  - `metrics` (or `operator`) for metrics endpoints.
  - `tenant:<tenant-id>` for tenant-scoped job/task access.

## Noted boundary (non-blocking)

- Proto contracts model gRPC control-plane APIs and include tenant metadata via `Metadata.tenant`.
- Admin REST endpoints (dead-letter/audit) are not represented as gRPC services yet.
- Recommendation: if gRPC parity is required, add `AdminService` proto in a future increment.

## Release recommendation

- Proceed to staging with current API surface.
- Keep `openapi/splai-admin-task.yaml` as source of truth for REST compatibility checks in CI.

