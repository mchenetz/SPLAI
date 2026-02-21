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

## Runtime knobs

- `SPLAI_ARTIFACT_ROOT` (default `/tmp/splai-artifacts`): local root where task outputs are written before being reported as `artifact://...` URIs.
- `SPLAI_MODEL_CACHE_DIR` (default `${SPLAI_ARTIFACT_ROOT}/models`): local model cache root used for `model_download` and optional auto-install behavior.
- `SPLAI_API_TOKEN` (optional): bearer token sent as `X-SPLAI-Token` for register/heartbeat/assignment/report requests when control-plane auth is enabled.
- `HF_TOKEN` (optional): token passed through to `hf` / `huggingface-cli` / `git` model download flows for private Hugging Face repos.

## Worker container image tools

The published worker image includes model download tooling required by `model_download` tasks:

- `hf` (Hugging Face CLI, preferred)
- `huggingface-cli`
- `git`

These are installed via `packaging/worker/Dockerfile` and published in CI by `.github/workflows/release-assets.yml`.

## Bootstrap via `splaictl`

1. Build/install binaries: `make install-worker`
2. Join a worker host: `splaictl worker join --url http://<gateway>:8080`
3. Validate connectivity: `splaictl verify --url http://<gateway>:8080 --worker-id <worker-id>`

## Implemented task executors

- `llm_inference`
- `model_download` (Hugging Face model prefetch/install)
- `tool_execution`
- `embedding`
- `retrieval`
- `aggregation`
