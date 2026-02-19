# Queue Reliability SLOs

## SLO targets

1. Dead-letter backlog SLO
- Objective: `dead_letter_count == 0` for 99% of 30-day windows.
- Error budget event: any minute with `dead_letter_count > 0`.

2. Retry error rate SLO
- Objective: `sum(rate(queue_nacked_total{reason="error"}[5m])) < 1` averaged over 30 days.
- Error budget event: any 5-minute interval above threshold.

3. Expired lease churn SLO
- Objective: `sum(rate(queue_expired_requeued_total[5m])) < 0.5` averaged over 30 days.
- Error budget event: any 5-minute interval above threshold.

4. Scheduler assignment stability SLO
- Objective: `sum(rate(scheduler_assignment_errors_total[5m])) == 0` for 99.9% of 30-day windows.
- Error budget event: any non-zero 5-minute interval.

## Alert-to-runbook mapping

- `DAEFDeadLetterNonZero` -> `docs/reference/queue-reliability-runbook.md`
- `DAEFQueueRetryStorm` -> `docs/reference/queue-reliability-runbook.md`
- `DAEFSchedulerAssignmentErrors` -> `docs/reference/queue-reliability-runbook.md`
- `DAEFExpiredLeaseChurn` -> `docs/reference/queue-reliability-runbook.md`
