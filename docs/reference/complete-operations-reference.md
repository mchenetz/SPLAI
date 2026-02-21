# SPLAI Complete Reference: API, Policy, Commands, Variables, and Integration Patterns

This document is the operator and integrator reference for SPLAI.
It is implementation-aligned with the current codebase and intended as a single source for day-2 operations.

## 1. Scope and Runtime Surface

SPLAI consists of:

- API gateway (`cmd/api-gateway`)
- Scheduler engine (`internal/scheduler`)
- Planner/compiler (`internal/planner`)
- Worker agent (`worker/cmd/worker-agent`)
- Optional OpenAI-compatible endpoints (`/v1/chat/completions`, `/v1/responses`)
- Optional persistence and queue backends (Postgres, Redis)
- Optional policy and model-routing config files

## 2. Command Reference

## 2.1 Make Targets

From repository root (`/Users/mchenetz/git/DAIEF`):

```bash
make build           # go build ./...
make test            # go test ./...
make proto           # regenerate proto stubs
make infra-up        # docker compose up postgres+redis for local persistent mode
make infra-down      # docker compose down
make build-worker    # build dist/splai-worker
make build-splaictl  # build dist/splaictl
make install-worker  # install splai-worker + splaictl to /usr/local/bin (or BIN_DIR)
make helm-lint       # helm lint charts/splai
```

## 2.2 `splaictl` CLI

### Install/upgrade control plane via Helm

```bash
splaictl install \
  --release splai \
  --namespace splai-system \
  --chart ./charts/splai \
  --create-namespace \
  --values ./values.prod.yaml
```

Flags:

- `--release` default `splai`
- `--namespace` default `splai-system`
- `--chart` default `./charts/splai`
- `--create-namespace` default `true`
- `--values` optional values file

### Generate worker token

```bash
splaictl worker token create --length 32
```

Flags:

- `--length` random bytes before base64url encoding, minimum `16`, default `32`

### Join a host as worker

```bash
splaictl worker join \
  --url http://gateway.example:8080 \
  --worker-id worker-node-01 \
  --token <worker-token> \
  --worker-bin splai-worker \
  --service systemd \
  --artifact-root /var/lib/splai/artifacts \
  --max-parallel-tasks 4 \
  --heartbeat-seconds 5 \
  --poll-millis 1500 \
  --env-file ~/.config/splai/worker.env
```

Flags:

- `--url` required control-plane URL
- `--worker-id` default hostname
- `--token` optional API token, written to `SPLAI_API_TOKEN`
- `--worker-bin` default `splai-worker`
- `--service` one of `systemd|launchd|none`
- `--artifact-root` default OS-specific path
- `--max-parallel-tasks` default `2`
- `--heartbeat-seconds` default `5`
- `--poll-millis` default `1500`
- `--env-file` default OS-specific path

### Verify control plane and worker assignment API

```bash
splaictl verify --url http://localhost:8080 --worker-id worker-local --token <token>
```

Flags:

- `--url` default `http://localhost:8080`
- `--worker-id` optional; if set, validates assignments endpoint
- `--token` optional auth token header (`X-SPLAI-Token`)

## 2.3 Utility Scripts

```bash
./scripts/install-worker.sh
./scripts/benchmark-scheduler.sh
WORKERS=1000 ./scripts/scale-proof.sh
WORKERS=10000 JOBS=20000 TARGET_UTIL=0.70 ./scripts/nfr-proof.sh
./scripts/fault-injection.sh splai-system
```

Notes:

- Benchmarks and proof runs write output into `docs/release/benchmarks/`
- `fault-injection.sh` restarts Redis, Postgres, and one worker pod

## 3. API Reference

Base URL example:

- `http://localhost:8080`

## 3.1 Common Headers

- `Content-Type: application/json` for JSON requests
- `Authorization: Bearer <token>` or `X-SPLAI-Token: <token>` when auth is enabled
- `X-SPLAI-Tenant: <tenant>` optional tenant override (default `default`)
- `X-SPLAI-Confirm: <confirm-token>` required for large admin requeue operations when safety controls are configured

## 3.2 Health and Metrics

### GET `/healthz`

- Purpose: control-plane health probe
- Auth: none
- Response: `{ "status": "ok" }`

### GET `/v1/metrics`

- Purpose: JSON metrics snapshot
- Auth scope: `metrics` or `operator`

### GET `/v1/metrics/prometheus`

- Purpose: Prometheus text exposition
- Auth scope: `metrics` or `operator`

## 3.3 Jobs

### POST `/v1/jobs`

Submit a job.

Request body:

```json
{
  "type": "chat",
  "input": "Analyze 500 support tickets and produce root causes.",
  "policy": "enterprise-default",
  "priority": "interactive",
  "install_model_if_missing": true,
  "planner_mode": "hybrid",
  "tenant": "tenant-a",
  "model": "llama3-8b-q4",
  "latency_class": "interactive",
  "reasoning_required": true,
  "data_classification": "internal",
  "network_isolation": "default"
}
```

Defaults if omitted:

- `priority=interactive`
- `policy=enterprise-default`
- `planner_mode=template`
- `latency_class=priority`
- tenant defaults to `X-SPLAI-Tenant` header, then `default`
- `install_model_if_missing=false`

Response:

```json
{ "job_id": "job-123" }
```

Auth:

- If auth disabled: open
- If auth enabled: tenant action `submit` required

### GET `/v1/jobs/{id}`

Get job status.

Response shape:

```json
{
  "job_id": "job-123",
  "status": "Running",
  "message": "",
  "result_artifact_uri": "artifact://job-123/t4/output.json",
  "created_at": "Thu, 19 Feb 2026 17:00:00 GMT",
  "updated_at": "Thu, 19 Feb 2026 17:00:03 GMT"
}
```

Auth:

- If auth enabled: tenant action `read` required

### DELETE `/v1/jobs/{id}`

Cancel a job.

Response:

```json
{ "accepted": true }
```

Auth:

- If auth enabled: tenant action `cancel` required

### GET `/v1/jobs/{id}/tasks`

List task-level status for a job.

Query params:

- `status` exact status filter
- `worker_id` exact worker filter
- `limit` non-negative integer (`0` means no page limit)
- `offset` non-negative integer

Response shape:

```json
{
  "job_id": "job-123",
  "total": 4,
  "returned": 2,
  "limit": 2,
  "offset": 0,
  "tasks": [
    {
      "task_id": "t1",
      "type": "llm_inference",
      "status": "Running",
      "attempt": 1,
      "worker_id": "node-1",
      "lease_id": "lease-abc",
      "lease_expires": "Thu, 19 Feb 2026 17:00:10 GMT",
      "output_artifact_uri": "",
      "error": "",
      "created_at": "Thu, 19 Feb 2026 17:00:00 GMT",
      "updated_at": "Thu, 19 Feb 2026 17:00:05 GMT"
    }
  ]
}
```

## 3.4 Worker Lifecycle Endpoints

### POST `/v1/workers/register`

Register a worker.

```json
{
  "worker_id": "node-1",
  "cpu": 32,
  "memory": "128Gi",
  "gpu": false,
  "models": ["llama3-8b-q4"],
  "tools": ["bash", "python"],
  "locality": "cluster-a"
}
```

Response:

```json
{
  "accepted": true,
  "heartbeat_interval_seconds": 5
}
```

### POST `/v1/workers/{worker_id}/heartbeat`

```json
{
  "queue_depth": 0,
  "running_tasks": 1,
  "cpu_utilization": 15,
  "memory_utilization": 20,
  "health": "healthy",
  "timestamp_unix": 1770000000
}
```

Response:

```json
{ "accepted": true }
```

### GET `/v1/workers/{worker_id}/assignments?max_tasks=1`

Response:

```json
{
  "assignments": [
    {
      "job_id": "job-1",
      "task_id": "t1",
      "type": "llm_inference",
      "inputs": {"prompt": "..."},
      "attempt": 1,
      "lease_id": "lease-1"
    }
  ]
}
```

## 3.5 Task Result Reporting

### POST `/v1/tasks/report`

```json
{
  "worker_id": "node-1",
  "job_id": "job-1",
  "task_id": "t1",
  "lease_id": "lease-1",
  "idempotency_key": "node-1:job-1:t1:1",
  "status": "Completed",
  "output_artifact_uri": "artifact://job-1/t1/output.json",
  "duration_millis": 1234
}
```

On failure:

```json
{
  "worker_id": "node-1",
  "job_id": "job-1",
  "task_id": "t1",
  "status": "Failed",
  "error": "tool execution timeout",
  "duration_millis": 1234
}
```

Response:

```json
{ "accepted": true }
```

Auth:

- If auth enabled: tenant action `report` required

## 3.6 Admin Queue and Audit

### GET `/v1/admin/queue/dead-letter?limit=50`

Auth scope: `operator`

Response:

```json
{
  "tasks": [
    {"job_id": "job-1", "task_id": "t2"}
  ]
}
```

### POST `/v1/admin/queue/dead-letter`

Auth scope: `operator`

Request:

```json
{
  "tasks": [
    {"job_id": "job-1", "task_id": "t2"}
  ],
  "dry_run": true
}
```

Response dry run:

```json
{ "dry_run": true, "requested": 1, "requeued": 1 }
```

Response real:

```json
{ "requested": 1, "requeued": 1 }
```

Safety controls:

- `SPLAI_ADMIN_REQUEUE_MAX_BATCH`
- `SPLAI_ADMIN_REQUEUE_RATE_LIMIT_PER_MIN`
- `SPLAI_ADMIN_REQUEUE_CONFIRM_THRESHOLD`
- `SPLAI_ADMIN_REQUEUE_CONFIRM_TOKEN` with header `X-SPLAI-Confirm`

### GET `/v1/admin/audit`

Auth scope: `operator`

Query params:

- `limit` (default `50`, positive)
- `offset` (default `0`, non-negative)
- `action`
- `actor`
- `tenant`
- `result`
- `from` RFC3339
- `to` RFC3339
- `format=csv` for CSV export

JSON response:

```json
{
  "returned": 1,
  "limit": 50,
  "offset": 0,
  "events": [
    {
      "id": 1,
      "action": "dead_letter_requeue",
      "actor": "tok-1234",
      "tenant": "default",
      "remote_addr": "127.0.0.1:50000",
      "resource": "queue/dead-letter",
      "payload_hash": "...",
      "prev_hash": "...",
      "event_hash": "...",
      "requested": 1,
      "result": "ok",
      "details": "requeued=1",
      "created_at": "Thu, 19 Feb 2026 17:00:10 GMT"
    }
  ]
}
```

### POST `/v1/admin/models/prefetch`

Submit a centralized model prefetch job to workers.

Auth scope: `operator`

Request:

```json
{
  "model": "meta-llama/Llama-3-8B-Instruct",
  "source": "huggingface",
  "workers": ["worker-a", "worker-b"],
  "only_missing": true,
  "priority": "standard",
  "tenant": "platform"
}
```

Response:

```json
{
  "job_id": "job-200",
  "model": "meta-llama/Llama-3-8B-Instruct",
  "source": "huggingface",
  "targeted_workers": ["worker-a", "worker-b"],
  "scheduled_tasks": 2,
  "only_missing": true
}
```

## 3.7 OpenAI-Compatible Endpoints (Optional)

Enable with:

```bash
SPLAI_OPENAI_COMPAT=true
```

### POST `/v1/chat/completions`

Compatibility notes:

- `stream=true` is not supported
- request is translated into internal job execution and polled until completion or timeout

Example request:

```json
{
  "model": "llama3-8b-q4",
  "messages": [
    {"role": "system", "content": "You are a support analyst."},
    {"role": "user", "content": "Summarize root causes."}
  ]
}
```

### POST `/v1/responses`

Example request:

```json
{
  "model": "llama3-8b-q4",
  "input": "Generate a summary report"
}
```

Timeout control:

- `SPLAI_OPENAI_COMPAT_TIMEOUT_SECONDS` default `60`

## 4. Auth and RBAC Reference

Auth is disabled unless `SPLAI_API_TOKENS` is set.

## 4.1 Token Format

```bash
SPLAI_API_TOKENS='operator-token:operator|metrics,tenant-a-token:tenant:tenant-a'
```

Each token entry: `token:scope1|scope2|...`

## 4.2 Supported Scope Concepts

- `operator`
- `metrics`
- `tenant:<tenant-id>`
- `tenant:*`
- `job:read`
- `job:submit`
- `job:cancel`
- `task:report`
- `admin`

## 4.3 Role Mapping

Optional role definitions:

```bash
SPLAI_API_ROLES='ops=operator|metrics,reader=metrics'
SPLAI_API_TOKEN_ROLES='token-a=ops,token-b=reader'
```

Built-in default roles:

- `admin`: `operator`, `metrics`, `admin`, `tenant:*`, `job:*`, `task:report`
- `ops`: `operator`, `metrics`
- `tenant-runner`: `job:submit`, `job:read`, `task:report`
- `tenant-reader`: `job:read`

## 4.4 Tenant Resolution

Tenant used for authorization resolves as:

1. `tenant` field in request body (for submit)
2. `X-SPLAI-Tenant` header
3. fallback `default`

## 5. Environment Variable Reference

## 5.1 API Gateway / Control Plane

- `SPLAI_GATEWAY_PORT` default `8080`
- `SPLAI_OPENAI_COMPAT` default `false`
- `SPLAI_OPENAI_COMPAT_TIMEOUT_SECONDS` default `60`

Persistence and queue:

- `SPLAI_STORE` default `memory`; supported `memory|postgres`
- `SPLAI_POSTGRES_DSN` required if `SPLAI_STORE=postgres`
- `SPLAI_QUEUE` default `memory`; supported `memory|redis`
- `SPLAI_REDIS_ADDR` default `127.0.0.1:6379`
- `SPLAI_REDIS_PASSWORD` optional
- `SPLAI_REDIS_DB` default `0`
- `SPLAI_REDIS_KEY` default `splai:tasks`
- `SPLAI_REDIS_DEADLETTER_MAX` default `5`
- `SPLAI_LEASE_SECONDS` default `15`

Scheduler scoring:

- `SPLAI_SCHEDULER_PREEMPT` default `true`
- `SPLAI_SCHED_WEIGHT_CAPABILITY_MATCH` default `1.0`
- `SPLAI_SCHED_WEIGHT_MODEL_WARM_CACHE` default `1.5`
- `SPLAI_SCHED_WEIGHT_LOCALITY` default `1.0`
- `SPLAI_SCHED_WEIGHT_QUEUE_PENALTY` default `0.15`
- `SPLAI_SCHED_WEIGHT_LATENCY` default `1.0`
- `SPLAI_SCHED_WEIGHT_FAIRNESS` default `0.05`
- `SPLAI_SCHED_WEIGHT_WAIT_AGE` default `0.05`

Auth and admin safety:

- `SPLAI_API_TOKENS` optional; enables auth when set
- `SPLAI_API_ROLES` optional
- `SPLAI_API_TOKEN_ROLES` optional
- `SPLAI_ADMIN_REQUEUE_MAX_BATCH` default `100`
- `SPLAI_ADMIN_REQUEUE_RATE_LIMIT_PER_MIN` default `30`
- `SPLAI_ADMIN_REQUEUE_CONFIRM_THRESHOLD` default `20`
- `SPLAI_ADMIN_REQUEUE_CONFIRM_TOKEN` optional

Planner / routing / policy:

- `SPLAI_POLICY_FILE` optional YAML policy file
- `SPLAI_MODEL_ROUTING_FILE` optional YAML model routing file
- `SPLAI_LLM_PLANNER_ENDPOINT` optional remote planner endpoint
- `SPLAI_LLM_PLANNER_API_KEY` optional bearer token for remote planner

Observability:

- `SPLAI_OTEL_EXPORTER` one of `none|stdout|otlpgrpc|otlp|otlphttp`
- `SPLAI_OTEL_ENDPOINT` exporter endpoint
- `SPLAI_OTEL_INSECURE` default `true`
- `SPLAI_OTEL_HEADERS` comma-separated `k=v`
- `SPLAI_OTEL_SAMPLER` one of `always_on|always_off|ratio`
- `SPLAI_OTEL_SAMPLER_RATIO` default `1.0`
- `SPLAI_ENVIRONMENT` optional resource tag

## 5.2 Worker Agent

- `SPLAI_CONTROL_PLANE_URL` default `http://localhost:8080`
- `SPLAI_WORKER_ID` default `worker-local`
- `SPLAI_API_TOKEN` optional token sent via `X-SPLAI-Token`
- `SPLAI_MAX_PARALLEL_TASKS` default `2`
- `SPLAI_HEARTBEAT_SECONDS` default `5`
- `SPLAI_POLL_MILLIS` default `1500`
- `SPLAI_ARTIFACT_ROOT` default `/tmp/splai-artifacts`
- `SPLAI_MODEL_CACHE_DIR` default `${SPLAI_ARTIFACT_ROOT}/models`
- `SPLAI_ARTIFACT_BACKEND` default `local`; use `minio` for object upload
- `SPLAI_MINIO_ENDPOINT` required when backend is `minio`
- `SPLAI_MINIO_ACCESS_KEY` optional
- `SPLAI_MINIO_SECRET_KEY` optional
- `SPLAI_MINIO_BUCKET` default `splai-artifacts`
- `SPLAI_MINIO_USE_SSL` default `false`
- `HF_TOKEN` optional, used by Hugging Face CLI download tooling when workers fetch private models

## 5.3 Test/Benchmark-Only Variables

- `SPLAI_POSTGRES_DSN_INTEGRATION`
- `SPLAI_REDIS_ADDR_INTEGRATION`
- `SPLAI_BENCH_WORKERS`
- `SPLAI_RUN_NFR_PROOF`
- `SPLAI_NFR_WORKERS`
- `SPLAI_NFR_JOBS`
- `SPLAI_NFR_TARGET_UTILIZATION`

## 6. Policy Engine Reference

Policy file is loaded via `SPLAI_POLICY_FILE`.

Schema:

```yaml
default_action: allow # allow|deny

tenant_quotas:
  tenant-a:
    max_running_jobs: 10
    max_running_tasks: 100

rules:
  - name: deny-confidential-external
    effect: deny # allow|deny
    reason: confidential_external_forbidden
    match:
      tenant: tenant-a
      job_type: chat
      task_type: llm_inference
      model: external_api
      data_classification: confidential
      priority: interactive
      network_isolation: default
      worker_locality: cluster-a
      requires_gpu: false
```

Evaluation behavior:

- submit-time quota check: `max_running_jobs`
- assignment-time quota check: `max_running_tasks`
- first matching rule wins
- if no rule matches, `default_action` applies

## 6.1 Policy Examples

### Example A: Confidential data deny for external model

```yaml
default_action: allow
rules:
  - name: deny-confidential-external
    effect: deny
    reason: confidential_external_forbidden
    match:
      data_classification: confidential
      model: external_api
```

### Example B: Strict default deny + explicit allow

```yaml
default_action: deny
rules:
  - name: allow-tenant-a-chat
    effect: allow
    match:
      tenant: tenant-a
      job_type: chat
```

### Example C: Tenant quota gate

```yaml
default_action: allow
tenant_quotas:
  tenant-a:
    max_running_jobs: 5
    max_running_tasks: 50
```

## 7. Model Routing Reference

Routing file is loaded via `SPLAI_MODEL_ROUTING_FILE`.

Schema:

```yaml
default_backend: ollama
default_model: llama3-8b-q4
rules:
  - name: reasoning-gpu
    latency_class: interactive
    reasoning_required: true
    data_classification: internal
    use_backend: vllm
    use_model: llama3-70b
```

Matching behavior:

- first rule match wins
- if request includes explicit `model`, it overrides default model before rule application

## 8. Planner and Runtime Modes

Planner modes (`planner_mode` on submit):

- `template`
- `llm_planner`
- `hybrid`

Task types currently used:

- `llm_inference`
- `model_download` (downloads model artifacts; currently supports Hugging Face source)
- `tool_execution`
- `embedding`
- `retrieval`
- `aggregation`

Job/task statuses used by scheduler:

- `Queued`
- `Running`
- `Completed`
- `Failed`
- `Canceled`

## 9. Helm Deployment Reference

Primary chart path:

- `charts/splai/`

Install:

```bash
helm install splai ./charts/splai -n splai-system --create-namespace
```

Upgrade with custom values:

```bash
helm upgrade --install splai ./charts/splai -n splai-system --create-namespace -f values.prod.yaml
```

Common values to tune:

- `apiGateway.replicaCount`
- `apiGateway.image.repository/tag`
- `apiGateway.env.SPLAI_POSTGRES_DSN`
- `apiGateway.env.SPLAI_REDIS_ADDR`
- `worker.enabled`
- `worker.image.repository/tag`
- `worker.maxParallelTasks`
- `postgres.enabled`
- `redis.enabled`
- `minio.enabled`

## 10. End-to-End Usage Snippets

## 10.1 Direct REST Submission

```bash
curl -s -X POST http://localhost:8080/v1/jobs \
  -H 'Content-Type: application/json' \
  -H 'X-SPLAI-Tenant: tenant-a' \
  -H 'Authorization: Bearer tenant-token' \
  -d '{
    "type": "chat",
    "input": "Analyze support incidents and extract top root causes.",
    "policy": "enterprise-default",
    "priority": "interactive",
    "planner_mode": "hybrid",
    "data_classification": "internal",
    "reasoning_required": true
  }'
```

## 10.2 Poll Status

```bash
JOB_ID="job-1"
while true; do
  curl -s "http://localhost:8080/v1/jobs/${JOB_ID}" | jq
  sleep 2
done
```

## 10.3 OpenAI Python SDK (compat mode)

```python
from openai import OpenAI

client = OpenAI(base_url="http://localhost:8080/v1", api_key="token")
resp = client.chat.completions.create(
    model="llama3-8b-q4",
    messages=[{"role": "user", "content": "Summarize this report."}],
)
print(resp.choices[0].message.content)
```

## 10.4 LangChain (compat mode)

```python
from langchain_openai import ChatOpenAI

llm = ChatOpenAI(
    model="llama3-8b-q4",
    base_url="http://localhost:8080/v1",
    api_key="token",
)
print(llm.invoke("Classify these incidents by root cause."))
```

## 10.5 Worker Bootstrap

```bash
make install-worker
splaictl worker token create
splaictl worker join --url http://localhost:8080 --service systemd --token <token>
splaictl verify --url http://localhost:8080 --worker-id <worker-id> --token <token>
```

## 11. Troubleshooting Quick Reference

- `unsupported SPLAI_STORE value`: verify `SPLAI_STORE` is `memory` or `postgres`
- `SPLAI_POSTGRES_DSN is required`: set DSN when `SPLAI_STORE=postgres`
- `unsupported SPLAI_QUEUE value`: verify `SPLAI_QUEUE` is `memory` or `redis`
- `heartbeat request failed`: check worker token, URL, and network reachability
- `tenant action denied`: token lacks required tenant or action scope
- `confirmation token required for large requeue`: include `X-SPLAI-Confirm` and set matching `SPLAI_ADMIN_REQUEUE_CONFIRM_TOKEN`
- `stream=true is not supported`: disable streaming for compatibility endpoints

## 12. Reference File Index

- API DTOs: `pkg/splaiapi/types.go`
- API handlers: `internal/api/server.go`
- Auth/RBAC: `internal/api/auth.go`
- Scheduler bootstrap env wiring: `internal/bootstrap/controlplane.go`
- Policy engine: `internal/policy/engine.go`
- Model routing: `internal/models/router.go`
- Worker config: `worker/internal/config/config.go`
- Helm chart: `charts/splai/`
- OpenAPI: `openapi/splai-admin-task.yaml`

## 13. Standalone Planner/Scheduler Service APIs

These are service-local APIs exposed by `cmd/planner` and `cmd/scheduler` for decoupled control-plane operation.

Planner service (default `SPLAI_PLANNER_PORT=8081`):

- `GET /healthz`
- `GET /v1/metrics`
- `GET /v1/metrics/prometheus`
- `POST /v1/planner/compile`

Example:

```bash
curl -s -X POST http://localhost:8081/v1/planner/compile \
  -H 'content-type: application/json' \
  -d '{"job_id":"job-1","type":"chat","mode":"hybrid","input":"Analyze 500 tickets"}'
```

Scheduler service (default `SPLAI_SCHEDULER_PORT=8082`):

- `GET /healthz`
- `GET /v1/metrics`
- `GET /v1/metrics/prometheus`
- `POST /v1/scheduler/jobs`
- `GET /v1/scheduler/jobs/{id}`
- `GET /v1/scheduler/jobs/{id}/tasks`
- `DELETE /v1/scheduler/jobs/{id}`
- `POST /v1/scheduler/workers/register`
- `POST /v1/scheduler/workers/{id}/heartbeat`
- `GET /v1/scheduler/workers/{id}/assignments?max_tasks=1`
- `POST /v1/scheduler/tasks/report`
- `GET /v1/scheduler/admin/queue/dead-letter?limit=50`
- `POST /v1/scheduler/admin/queue/dead-letter`
- `GET /v1/scheduler/admin/audit`

Notes:

- Scheduler service reads the same persistence/queue env vars as the API gateway (`SPLAI_STORE`, `SPLAI_POSTGRES_DSN`, `SPLAI_QUEUE`, `SPLAI_REDIS_ADDR`, etc.).
- For multi-tenant production use, keep auth/policy enforcement in gateway and treat scheduler service as internal control-plane traffic.
