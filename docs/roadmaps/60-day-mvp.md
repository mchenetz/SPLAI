# 60-Day MVP Execution Roadmap

## Scope baseline
Phase-1 only: DAG execution, worker registration, CPU distributed inference, basic scheduler, retries/timeouts, artifact storage, execution tracing.

## Day 0-7: Foundation

- Lock architecture, API contracts, and CRD schemas.
- Stand up local dev stack: Postgres, Redis/NATS, MinIO, OTel collector.
- Implement gateway skeleton (`POST/GET/DELETE /v1/jobs`).
- Implement proto package and code generation workflow.

Exit criteria:
- End-to-end "submit job" persists `SPLAIJob` and returns `job_id`.

## Day 8-14: Planner + DAG persistence

- Implement template planner and hybrid planner interface.
- Add DAG representation to `SPLAIJob.spec` and task status fields.
- Add job lifecycle transitions and validation.

Exit criteria:
- Input request is deterministically transformed into runnable DAG.

## Day 15-21: Worker registration + heartbeat

- Implement worker agent boot + registration flow.
- Implement 5-second heartbeat updates with utilization snapshots.
- Mark stale workers and expose worker availability API.

Exit criteria:
- Workers appear/disappear reliably and scheduler sees live capacity.

## Day 22-30: Scheduler v1 + retries/timeouts

- Implement runnable-task queue.
- Implement scoring function and worker selection.
- Add retries, backoff, timeout handling, and dead-letter state.

Exit criteria:
- Tasks are assigned/executed across multiple CPU workers with recovery from failures.

## Day 31-40: Execution runtime + artifacts

- Implement worker task executor for `llm_inference`, `tool_execution`, `aggregation`.
- Add artifact URI write/read paths through object store + metadata DB.
- Implement cancellation propagation from job to task runtime.

Exit criteria:
- Completed jobs produce reproducible artifacts and linked lineage.

## Day 41-50: Observability + policy gate

- Instrument traces and metrics across gateway/planner/scheduler/worker.
- Add policy evaluation before scheduling (model/data/classification allow/deny).
- Add dashboard views for job latency, retries, and worker utilization.

Exit criteria:
- Every job has traceable path and policy audit result.

## Day 51-60: Hardening + benchmark

- Multi-tenant isolation checks and quota enforcement.
- Scale and chaos tests (node loss, retry storms, delayed heartbeats).
- Benchmark against single-node baseline.
- Produce MVP release artifacts and runbook.

Exit criteria:
- Demonstrate 5-10x throughput improvement on CPU cluster workloads.
- Show >=70% CPU utilization during benchmark window.
- Show resumable workflows with lineage intact.

## Delivery artifacts at day 60

- `v0.1.0` manifests and container images.
- API/proto docs and operator runbook.
- Benchmark report and known limitations list.
- Phase-2 backlog (GPU pool, advanced planner, autoscaling).
