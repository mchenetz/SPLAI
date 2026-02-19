# Release Hardening Checklist

## API and compatibility

- Verify backward compatibility for `/v1/jobs/{id}/tasks` response fields.
- Confirm admin endpoint auth rules (`operator`, `metrics`, tenant scopes).
- Validate OpenAPI file at `openapi/splai-admin-task.yaml` reflects implementation.

## Data and migrations

- Apply migrations in staging and verify:
  - `0004_audit_events.sql`
  - `0005_tenant_and_audit_chain.sql`
- Validate migration rollback plan and backup snapshot timing.

## Reliability and security

- Validate dead-letter safety controls:
  - max batch
  - rate limit
  - confirmation token
  - dry-run behavior
- Validate tenant access controls for job submit/read/update/report.

## Observability and operations

- Deploy Prometheus scrape config and alert rules.
- Import dashboard `deploy/kubernetes/observability/grafana-dashboard-splai-queue.json`.
- Confirm alerts and runbook links are active.

## Staging soak and fault injection

- 24h soak with synthetic load.
- Inject Redis restart and verify queue recovery.
- Inject Postgres restart and verify API/scheduler recovery.
- Verify dead-letter requeue and audit chain integrity post-failure.

## Go/No-Go

- Sign-off from platform, runtime, and on-call owner.
- Archive release artifact digests and manifest versions.
