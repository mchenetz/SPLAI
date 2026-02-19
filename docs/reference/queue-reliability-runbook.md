# Queue Reliability Runbook

## Scope

Use this runbook when queue reliability alerts fire:

- `SPLAIDeadLetterNonZero`
- `SPLAIQueueRetryStorm`
- `SPLAISchedulerAssignmentErrors`
- `SPLAIExpiredLeaseChurn`

## Immediate checks

1. Query metrics:
- `queue_nacked_total`
- `dead_letter_count`
- `queue_expired_requeued_total`
- `scheduler_assignment_errors_total`

2. Check dead-letter queue:

```bash
curl -s http://<gateway>/v1/admin/queue/dead-letter?limit=100
```

3. Check worker heartbeat freshness and running task skew:
- Compare worker heartbeat cadence against `SPLAI_LEASE_SECONDS`
- Look for stale workers holding many running tasks

## Remediation

1. Retry storm (`queue_nacked_total{reason="error"}` high)
- Verify queue backend health (Redis/Postgres reachability).
- Verify worker process and tool dependencies.
- Increase `SPLAI_LEASE_SECONDS` if workers are frequently timing out due to long tasks.

2. Expired lease churn high
- Confirm workers can send heartbeat every 5s.
- Check node/network instability.
- Temporarily increase lease duration to reduce thrash.

3. Dead-letter queue non-zero
- Inspect list:

```bash
curl -s http://<gateway>/v1/admin/queue/dead-letter?limit=100
```

- Requeue only explicitly selected tasks:

```bash
curl -s -X POST http://<gateway>/v1/admin/queue/dead-letter \
  -H 'content-type: application/json' \
  -d '{"tasks":[{"job_id":"job-1","task_id":"t3"}]}'
```

- If task repeatedly returns to dead-letter, quarantine and investigate task input/runtime.

## Exit criteria

- `dead_letter_count == 0` for 30 minutes.
- `rate(queue_nacked_total{reason="error"}[5m])` below alert threshold for 30 minutes.
- `rate(queue_expired_requeued_total[5m])` below alert threshold for 30 minutes.
