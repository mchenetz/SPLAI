# Distributed AI Execution Fabric (DAEF)

Build a distributed runtime that treats AI execution like Kubernetes treats containers.

## Included deliverables

- Kubernetes-native CRD model and manifests
- Proto/gRPC contracts and generation wiring
- Worker agent reference implementation
- Founding-team repo layout
- 60-day MVP roadmap
- End-to-end MVP happy path (in-memory control plane + worker)

## Run happy path locally

Terminal 1:

```bash
go run ./cmd/api-gateway
```

Terminal 2:

```bash
go run ./worker/cmd/worker-agent
```

Terminal 3:

```bash
curl -s -X POST http://localhost:8080/v1/jobs \
  -H 'content-type: application/json' \
  -d '{"type":"chat","input":"Analyze 500 support tickets and produce root causes.","policy":"enterprise-default","priority":"interactive"}'
```

Then poll status:

```bash
curl -s http://localhost:8080/v1/jobs/job-1
```

Task-level state:

```bash
curl -s http://localhost:8080/v1/jobs/job-1/tasks
```

Filtered/paginated task view:

```bash
curl -s 'http://localhost:8080/v1/jobs/job-1/tasks?status=Queued&limit=10&offset=0'
```

## Run with persistent backends

Start dependencies:

```bash
make infra-up
```

Run gateway with Postgres + Redis:

```bash
DAEF_STORE=postgres \
DAEF_POSTGRES_DSN='postgres://daef:daef@127.0.0.1:5432/daef?sslmode=disable' \
DAEF_QUEUE=redis \
DAEF_REDIS_ADDR=127.0.0.1:6379 \
DAEF_LEASE_SECONDS=15 \
DAEF_REDIS_DEADLETTER_MAX=5 \
go run ./cmd/api-gateway
```

Migrations in `db/migrations/*.sql` are applied automatically at startup.

Dead-letter admin endpoints:

- `GET /v1/admin/queue/dead-letter?limit=50`
- `POST /v1/admin/queue/dead-letter` with explicit tasks to requeue
- `GET /v1/admin/audit?limit=50&offset=0`
- `GET /v1/admin/audit?action=dead_letter_requeue&result=ok&tenant=default&from=2026-02-18T00:00:00Z&to=2026-02-19T00:00:00Z`
- `GET /v1/admin/audit?format=csv&limit=500`

Metrics endpoint:

- `GET /v1/metrics`
- `GET /v1/metrics/prometheus`

Sensitive endpoint auth (optional):

- Set `DAEF_API_TOKENS` to enable token auth for metrics/admin endpoints.
- Format: comma-separated `token:scope1|scope2`.
- Scopes:
  - `metrics` for `/v1/metrics*`
  - `operator` for `/v1/admin/queue/dead-letter*` and `/v1/admin/audit*`
  - `tenant:<tenant-id>` for tenant-scoped job submit/status/report access
- Example:
  - `DAEF_API_TOKENS='operator-token:operator|metrics,tenant-a-token:tenant:tenant-a,metrics-token:metrics'`

Admin safety controls (optional):

- `DAEF_ADMIN_REQUEUE_MAX_BATCH` (default `100`)
- `DAEF_ADMIN_REQUEUE_RATE_LIMIT_PER_MIN` (default `30`)
- `DAEF_ADMIN_REQUEUE_CONFIRM_THRESHOLD` (default `20`)
- `DAEF_ADMIN_REQUEUE_CONFIRM_TOKEN` (required to enforce confirm-header checks)

Requeue dry-run support:

- `POST /v1/admin/queue/dead-letter` accepts `{"tasks":[...],"dry_run":true}` to preview requeue count without mutating queue state.

## Key paths

- Architecture: `docs/architecture/kubernetes-crd-native.md`
- CRDs: `config/crd/bases/`
- Proto contracts: `proto/daef/v1/`
- Proto generation: `docs/reference/proto-generation.md`
- Persistent mode: `docs/reference/persistent-control-plane.md`
- Checked-in stubs: `gen/proto/daef/v1/stubs.pb.go`
- Worker runtime: `worker/`
- Roadmap: `docs/roadmaps/60-day-mvp.md`
