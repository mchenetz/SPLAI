# Kubernetes-Native SPLAI (CRD-Based)

## Goal
Run SPLAI as a Kubernetes control plane where desired state is represented as Custom Resources and reconciled by controllers.

## Core CRDs

1. `SPLAIJob` (`splaijobs.splai.io`)
- Represents one user-submitted execution request.
- Spec captures tenant, policy, priority, DAG tasks/dependencies, and timeout.
- Status tracks lifecycle (`Queued`, `Planning`, `Scheduled`, `Running`, `Completed`, `Failed`, `Canceled`) and progress counters.

2. `SPLAIWorker` (`splaiworkers.splai.io`)
- Represents one execution-capable worker node/agent.
- Spec declares static capabilities (cpu/memory/gpu/models/tools/locality).
- Status reports heartbeat timestamp, health, utilization, queue depth, and running tasks.

3. `SPLAIPolicy` (`splaipolicies.splai.io`)
- Tenant-scoped policy rules used during admission and scheduling.
- Supports deny and allow rules for model usage, data classification, and quotas.

4. `SPLAIModelRoute` (`splaimodelroutes.splai.io`)
- Declarative routing rules for backend selection (ollama, vLLM, llama.cpp, remote API).
- Rule matching keys: interaction class, reasoning_required, tenant, classification.

## Control Plane Controllers

1. Job controller
- Reconciles `SPLAIJob` submission lifecycle.
- Invokes planner, writes DAG into `.spec.tasks`/`.spec.dependencies` if absent.
- Emits per-task execution records and final job result artifact URI.

2. Scheduler controller
- Watches runnable tasks from `SPLAIJob`.
- Watches healthy `SPLAIWorker` resources.
- Computes score:
  `capability_match + model_warm_cache + locality_score - queue_penalty - latency_prediction`
- Writes assignment to task state and worker queue.

3. Worker heartbeat controller
- Converts worker agent heartbeat stream into `SPLAIWorker.status` updates.
- Marks workers stale if heartbeat exceeds threshold.

4. Policy admission webhook
- Validates jobs against matching `SPLAIPolicy` before persistence.
- Rejects unsupported model/data combinations and quota violations.

## Runtime Objects and Data Flow

1. User submits `SPLAIJob` via API gateway.
2. Job controller ensures DAG exists.
3. Scheduler assigns runnable tasks to `SPLAIWorker`.
4. Worker agent executes task sandbox and writes artifacts to object store.
5. Worker reports task updates; controller updates status.
6. Job transitions to terminal state with lineage and artifact pointers.

## Kubernetes Packaging

- Operator deployment: `cmd/operator`
- API Gateway deployment: `deploy/kubernetes/control-plane/`
- Worker DaemonSet: `deploy/kubernetes/worker/`
- CRDs: `config/crd/bases/`

## MVP Fit
This CRD model is scoped to Phase-1 features only:

- DAG execution
- Worker registration/heartbeat
- CPU-first scheduling and execution
- Retry/timeout primitives
- Artifact references
- OpenTelemetry-friendly trace IDs in status/annotations
