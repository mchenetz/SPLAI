# Fault Injection Runbook

Use this runbook to validate control-plane recovery from dependency failures.

## Preconditions

- Kubernetes deployment applied in namespace `splai-system`.
- API gateway, Redis, and Postgres running.

## Run

```bash
./scripts/fault-injection.sh splai-system
```

## Sequence

1. Restart Redis deployment.
2. Restart Postgres deployment.
3. Delete one worker pod (if present).

## Verify

- New jobs can still be accepted.
- Existing jobs recover and continue execution.
- Audit endpoint remains available:
  - `GET /v1/admin/audit`
