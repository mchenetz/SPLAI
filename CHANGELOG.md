# Changelog

All notable changes to this project are documented in this file.

## [0.1.0] - 2026-02-19

### Added
- Initial Distributed AI Execution Fabric (DAEF) control-plane and worker scaffold.
- In-memory end-to-end execution path for job submission, planning, scheduling, assignment, and task reporting.
- Persistent backends:
  - Postgres state store.
  - Redis queue with claim/ack/nack semantics.
- Queue reliability features:
  - lease/visibility expiration requeue.
  - dead-letter queue listing and selective requeue.
  - dry-run dead-letter requeue.
- Admin and observability APIs:
  - `GET /v1/jobs/{id}/tasks` with filters and pagination.
  - `GET /v1/admin/queue/dead-letter`.
  - `POST /v1/admin/queue/dead-letter`.
  - `GET /v1/admin/audit` with filters and CSV export.
  - `GET /v1/metrics` (JSON).
  - `GET /v1/metrics/prometheus`.
- Token-based auth scopes:
  - `metrics`
  - `operator`
  - `tenant:<tenant-id>`
- Audit integrity-chain fields (`prev_hash`, `event_hash`) and tenant tagging.
- Kubernetes observability assets:
  - scrape config
  - alert rules
  - Grafana dashboard
- Release/OpenAPI assets:
  - `openapi/daef-admin-task.yaml`
  - release hardening checklist

### Database Migrations
- `0001_init.sql`
- `0002_indexes.sql`
- `0003_task_leases.sql`
- `0004_audit_events.sql`
- `0005_tenant_and_audit_chain.sql`

