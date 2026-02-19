# Persistent Control Plane (Postgres + Redis)

DAEF now supports pluggable control-plane persistence and queue backends.

## Backends

- Store (`DAEF_STORE`): `memory` (default) or `postgres`
- Queue (`DAEF_QUEUE`): `memory` (default) or `redis`

## Environment variables

- `DAEF_STORE`
- `DAEF_QUEUE`
- `DAEF_POSTGRES_DSN` (required if `DAEF_STORE=postgres`)
- `DAEF_REDIS_ADDR` (default `127.0.0.1:6379`)
- `DAEF_REDIS_PASSWORD` (optional)
- `DAEF_REDIS_DB` (default `0`)
- `DAEF_REDIS_KEY` (default `daef:tasks`)
- `DAEF_REDIS_DEADLETTER_MAX` (default `5`)
- `DAEF_LEASE_SECONDS` (default `15`)
- `DAEF_ADMIN_REQUEUE_MAX_BATCH` (default `100`)
- `DAEF_ADMIN_REQUEUE_RATE_LIMIT_PER_MIN` (default `30`)
- `DAEF_ADMIN_REQUEUE_CONFIRM_THRESHOLD` (default `20`)
- `DAEF_ADMIN_REQUEUE_CONFIRM_TOKEN` (optional, enables `X-DAEF-Confirm` requirement for large requeues)

## Start local infra

```bash
docker compose -f deploy/docker-compose.dev.yml up -d
```

## Run gateway with persistent backends

```bash
DAEF_STORE=postgres \
DAEF_POSTGRES_DSN='postgres://daef:daef@127.0.0.1:5432/daef?sslmode=disable' \
DAEF_QUEUE=redis \
DAEF_REDIS_ADDR=127.0.0.1:6379 \
go run ./cmd/api-gateway
```

## Notes

- Postgres schema is auto-created at gateway startup.
- SQL migrations are loaded from `db/migrations/*.sql` and tracked in `schema_migrations`.
- Redis queue uses list operations for FIFO task dispatch.
- Redis queue now uses claim/ack semantics with:
  - pending list: `<key>:pending`
  - claims hash + visibility zset: `<key>:claims`, `<key>:visibility`
  - dead-letter list: `<key>:dead`
- If persistence env vars are omitted, DAEF falls back to in-memory mode.

## Dead-letter admin API

- List dead-letter tasks:
  - `GET /v1/admin/queue/dead-letter?limit=50`
- Requeue selected dead-letter tasks:
  - `POST /v1/admin/queue/dead-letter`
  - body:
    ```json
    {
      "tasks": [
        {"job_id": "job-1", "task_id": "t3"}
      ]
    }
    ```

Authentication/authorization:

- Optional token auth can be enabled via `DAEF_API_TOKENS`.
- Format: `token:scope1|scope2,token2:scope`.
- Required scopes:
  - `operator` for dead-letter admin endpoints
  - `metrics` (or `operator`) for metrics endpoints
  - `tenant:<tenant-id>` for tenant-scoped access to job/task endpoints

## Admin audit API

- `GET /v1/admin/audit?limit=50&offset=0`
- Optional filters:
  - `action`, `actor`, `tenant`, `result`
  - `from` and `to` (RFC3339 timestamps)
  - `format=csv` for CSV export
- Scope required: `operator`
- Returns audit events for admin actions (for example dead-letter list/requeue), including actor, tenant, payload hash, and result.
- Audit records include integrity-chain fields: `prev_hash` and `event_hash`.

## Job task status API

- `GET /v1/jobs/{id}/tasks`
- Returns per-task status, attempt, worker assignment, lease fields, error, output URI, and timestamps.
- Query params:
  - `status` (exact match, optional)
  - `worker_id` (exact match, optional)
  - `limit` (non-negative integer, optional)
  - `offset` (non-negative integer, optional)

## Reliability metrics

`GET /v1/metrics` exposes JSON counters and gauges, including:

- `queue_claimed_total` (`queue_backend`, `worker_id`)
- `queue_acked_total` (`queue_backend`, `worker_id`)
- `queue_nacked_total` (`queue_backend`, `worker_id`, `reason`)
- `queue_expired_requeued_total` (`queue_backend`)
- `dead_letter_requeued_total` (`queue_backend`)
- `dead_letter_count` gauge (`queue_backend`)
- `scheduler_assignment_errors_total` (`queue_backend`, `worker_id`)

Prometheus text format:

- `GET /v1/metrics/prometheus`

Deployment assets:

- `deploy/kubernetes/observability/prometheus-scrape-config.yaml`
- `deploy/kubernetes/observability/prometheus-rules.yaml`
- `deploy/kubernetes/observability/grafana-dashboard-daef-queue.json`

## Add a migration

1. Create a new file in `db/migrations/` with an increasing numeric prefix (for example `0002_add_indexes.sql`).
2. Put forward-only SQL in that file.
3. Restart the gateway; unapplied files are executed automatically.

Current migrations:

- `0001_init.sql`: base tables (`jobs`, `tasks`, `workers`)
- `0002_indexes.sql`: indexes for scheduler hot paths
  - `tasks(job_id, status)`
  - `workers(last_heartbeat)`
- `0003_task_leases.sql`: lease and idempotency columns for task execution
  - `lease_id`, `lease_expires_at`, `last_report_key`
  - index `tasks(status, lease_expires_at)`
- `0004_audit_events.sql`: admin audit event persistence
  - table `audit_events` with actor/action/resource/payload hash/outcome
  - indexes on `created_at` and `(action, created_at)`
- `0005_tenant_and_audit_chain.sql`: tenant and audit integrity additions
  - `jobs.tenant` and tenant/status scheduling index
  - `audit_events.tenant`, `audit_events.prev_hash`, `audit_events.event_hash`
  - indexes on `(tenant, created_at)` and `(actor, created_at)`
