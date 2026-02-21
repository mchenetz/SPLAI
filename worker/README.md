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
- `SPLAI_WORKER_BACKENDS` (optional, CSV): explicitly advertise reachable LLM backends (for example `ollama,vllm`).
- `SPLAI_EMBEDDING_BACKEND` (default `local`): embedding backend (`local`, `ollama`, `vllm`, `remote_api`).
- `SPLAI_EMBEDDING_MODEL` (default `nomic-embed-text`): default embedding model ID.
- `SPLAI_EMBEDDING_DIMENSION` (default `384`): default local embedding vector size when task input does not override it.
- `SPLAI_EMBEDDING_HTTP_RETRIES` (default `2`): retry count for embedding backend HTTP calls.
- `SPLAI_RETRIEVAL_BACKEND` (default `local`): retrieval backend (`local` or `remote_api`).
- `SPLAI_RETRIEVAL_BASE_URL` (optional): remote retrieval service base URL when `SPLAI_RETRIEVAL_BACKEND=remote_api`.
- `SPLAI_RETRIEVAL_API_KEY` (optional): bearer API key for remote retrieval requests.
- `SPLAI_RETRIEVAL_HTTP_RETRIES` (default `2`): retry count for remote retrieval HTTP calls.

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

## Aggregation inputs

The `aggregation` task supports structured reduction over dependency artifacts and inline items:

- `dep:<task_id>:output_uri`: dependency artifact URI(s), typically injected by scheduler.
- `items_json`: optional inline JSON array of objects to include as aggregation sources.
- `required_fields` or `required_fields_json`: required keys that must exist in merged output.
- `strict_dependencies` (default `true`): fail if dependency artifact cannot be loaded.

## Backend-aware scheduling guidance

Recommended for distributed + locality-aware scale:

1. Run Ollama per worker node (or otherwise node-local to the worker process).
2. Set `SPLAI_OLLAMA_BASE_URL` to the node-local endpoint (example `http://127.0.0.1:11434`).
3. Prefetch models to workers (`model_download` tasks) so model inventory is warm.
4. Ensure worker registration advertises backend/model capabilities.

Scheduler behavior:

- `llm_inference` tasks are assigned only to workers that advertise required backend/model capabilities when inventory is available.
