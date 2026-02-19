# Founding-Team Repository Structure

This structure separates control-plane services, worker runtime, API contracts, and deployment assets.

## Top-level layout

- `cmd/`
  - `api-gateway/`: HTTP API ingress and job lifecycle endpoints.
  - `planner/`: request-to-DAG planner service.
  - `scheduler/`: assignment loop and queue orchestration.
  - `operator/`: Kubernetes controllers/webhooks for CRDs.
- `internal/`
  - `api/`: authn/authz, tenancy, request validation.
  - `planner/`: template/LLM/hybrid decomposition logic.
  - `scheduler/`: scoring, queues, retry/preemption logic.
  - `policy/`: policy evaluation engine.
  - `artifacts/`: object store + metadata registry adapters.
  - `models/`: inference backend adapters and routing.
  - `observability/`: OTel traces/metrics/log helpers.
- `worker/`
  - `cmd/worker-agent/`: worker process entrypoint.
  - `internal/config/`: local worker configuration loading.
  - `internal/registration/`: worker register + refresh logic.
  - `internal/heartbeat/`: periodic health/resource reports.
  - `internal/executor/`: sandboxed task execution runtime.
  - `internal/runtime/`: task lifecycle state machine.
  - `internal/telemetry/`: worker-side traces/metrics emission.
- `proto/daef/v1/`: gRPC/protobuf APIs for services and workers.
- `config/crd/bases/`: CRD definitions.
- `deploy/kubernetes/`
  - `control-plane/`: API/scheduler/planner/operator manifests.
  - `worker/`: worker DaemonSet manifests.
  - `observability/`: Prometheus/OpenTelemetry bootstrap.
- `docs/`
  - `architecture/`: system and platform design.
  - `roadmaps/`: execution plans by time horizon.
  - `reference/`: conventions, repo docs, runbooks.

## Team ownership suggestion

- Platform (2 engineers): operator, scheduler, deployment assets.
- Runtime (2 engineers): worker agent and executor.
- AI systems (1-2 engineers): planner and model router.
- Product/backend (1 engineer): API gateway and tenancy.
- SRE/DevEx (1 engineer): observability, CI/CD, load testing.
