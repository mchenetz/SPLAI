# Persistent Control Plane (Postgres + Redis)

SPLAI now supports pluggable control-plane persistence and queue backends.

## Backends

- Store (`SPLAI_STORE`): `memory` (default) or `postgres`
- Queue (`SPLAI_QUEUE`): `memory` (default) or `redis`

## Environment variables

- `SPLAI_STORE`
- `SPLAI_QUEUE`
- `SPLAI_POSTGRES_DSN` (required if `SPLAI_STORE=postgres`)
- `SPLAI_REDIS_ADDR` (default `127.0.0.1:6379`)
- `SPLAI_REDIS_PASSWORD` (optional)
- `SPLAI_REDIS_DB` (default `0`)
- `SPLAI_REDIS_KEY` (default `splai:tasks`)
- `SPLAI_REDIS_DEADLETTER_MAX` (default `5`)
- `SPLAI_LEASE_SECONDS` (default `15`)
- `SPLAI_POLICY_FILE` (optional YAML policy config; enforces submit + assignment policy gates)
- `SPLAI_MODEL_ROUTING_FILE` (optional YAML model routing rules; used by API gateway)
- `SPLAI_LLM_PLANNER_ENDPOINT` (optional HTTP endpoint for `planner_mode=llm_planner`)
- `SPLAI_LLM_PLANNER_API_KEY` (optional bearer token for LLM planner endpoint)
- `SPLAI_OTEL_EXPORTER` (`none`, `stdout`, `otlpgrpc`/`otlp`, `otlphttp`)
- `SPLAI_OTEL_ENDPOINT` (gRPC: `host:port`; HTTP: full URL)
- `SPLAI_OTEL_INSECURE` (`true`/`false`)
- `SPLAI_OTEL_HEADERS` (comma-separated `k=v`)
- `SPLAI_OTEL_SAMPLER` (`always_on`, `always_off`, `ratio`)
- `SPLAI_OTEL_SAMPLER_RATIO` (`0.0`-`1.0`)
- `SPLAI_ENVIRONMENT` (optional tracing resource tag)
- OpenAI compatibility:
  - `SPLAI_OPENAI_COMPAT` (`true`/`false`)
  - `SPLAI_OPENAI_COMPAT_TIMEOUT_SECONDS` (default `60`)
- RBAC role mapping:
  - `SPLAI_API_ROLES` (for example `ops=operator|metrics`)
  - `SPLAI_API_TOKEN_ROLES` (for example `operator-token=ops`)
- Scheduler tuning:
  - `SPLAI_SCHEDULER_PREEMPT`
  - `SPLAI_SCHED_WEIGHT_CAPABILITY_MATCH`
  - `SPLAI_SCHED_WEIGHT_MODEL_WARM_CACHE`
  - `SPLAI_SCHED_WEIGHT_LOCALITY`
  - `SPLAI_SCHED_WEIGHT_QUEUE_PENALTY`
  - `SPLAI_SCHED_WEIGHT_LATENCY`
  - `SPLAI_SCHED_WEIGHT_FAIRNESS`
  - `SPLAI_SCHED_WEIGHT_WAIT_AGE`
- Worker artifact backend:
  - `SPLAI_ARTIFACT_BACKEND` (`local` or `minio`)
  - `SPLAI_ARTIFACT_ROOT` (local artifact root, default `/tmp/splai-artifacts`)
  - `SPLAI_MINIO_ENDPOINT`
  - `SPLAI_MINIO_ACCESS_KEY`
  - `SPLAI_MINIO_SECRET_KEY`
  - `SPLAI_MINIO_BUCKET`
  - `SPLAI_MINIO_USE_SSL`
- `SPLAI_ADMIN_REQUEUE_MAX_BATCH` (default `100`)
- `SPLAI_ADMIN_REQUEUE_RATE_LIMIT_PER_MIN` (default `30`)
- `SPLAI_ADMIN_REQUEUE_CONFIRM_THRESHOLD` (default `20`)
- `SPLAI_ADMIN_REQUEUE_CONFIRM_TOKEN` (optional, enables `X-SPLAI-Confirm` requirement for large requeues)

## Start local infra

```bash
docker compose -f deploy/docker-compose.dev.yml up -d
```

## Run gateway with persistent backends

```bash
SPLAI_STORE=postgres \
SPLAI_POSTGRES_DSN='postgres://splai:splai@127.0.0.1:5432/splai?sslmode=disable' \
SPLAI_QUEUE=redis \
SPLAI_REDIS_ADDR=127.0.0.1:6379 \
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
- If persistence env vars are omitted, SPLAI falls back to in-memory mode.

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

- Optional token auth can be enabled via `SPLAI_API_TOKENS`.
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
- Policy decision events are also persisted:
  - `policy_check_submit`
  - `policy_check_assignment`

## Policy file example

```yaml
default_action: allow
tenant_quotas:
  tenant-a:
    max_running_jobs: 20
    max_running_tasks: 200
rules:
  - name: deny-confidential-external
    effect: deny
    reason: confidential_external_forbidden
    match:
      data_classification: confidential
      model: external_api
```

## Model routing file example

```yaml
default_backend: ollama
default_model: llama3-8b-q4
rules:
  - name: reasoning-gpu
    latency_class: interactive
    reasoning_required: true
    use_backend: vllm
    use_model: llama3-70b
```

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
- `deploy/kubernetes/observability/grafana-dashboard-splai-queue.json`

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
