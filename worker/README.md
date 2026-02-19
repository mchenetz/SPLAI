# Worker Agent Reference Implementation

The worker agent is designed as a long-running process that:

1. Loads local config and capabilities.
2. Registers with control plane.
3. Emits heartbeat every 5 seconds.
4. Polls scheduler for assignments.
5. Executes tasks in sandboxed runtime.
6. Reports task results and telemetry.

## Package map

- `worker/cmd/worker-agent`: process bootstrap.
- `worker/internal/config`: env/file configuration.
- `worker/internal/registration`: registration client.
- `worker/internal/heartbeat`: heartbeat loop.
- `worker/internal/executor`: execution engine for task types.
- `worker/internal/runtime`: orchestration loop.
- `worker/internal/telemetry`: metric/trace emitters.

## MVP execution policy

- CPU-first operation (`gpu=false` default)
- bounded in-flight tasks
- retry only delegated to scheduler
- no implicit external network access during task execution
