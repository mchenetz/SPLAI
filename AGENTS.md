# AGENTS.md

## Cursor Cloud specific instructions

### Overview

SPLAI is a Go 1.22 monolith — a distributed AI execution fabric that decomposes jobs into task DAGs and schedules them across workers. See `README.md` for full architecture and usage.

### Services (local dev, in-memory mode)

| Service | Command | Port | Notes |
|---------|---------|------|-------|
| API Gateway | `go run ./cmd/api-gateway` | `:8080` | Core entry point; embeds planner + scheduler. Zero-dep in-memory mode by default. |
| Worker Agent | `go run ./worker/cmd/worker-agent` | N/A (polls gateway) | At least 1 needed to execute tasks. |

Both services default to in-memory store/queue (`SPLAI_STORE=memory`, `SPLAI_QUEUE=memory`), so **no Docker/Postgres/Redis needed** for local dev.

### Build / Test / Lint

Standard commands are in the `Makefile`:

- `make build` — compile all packages
- `make test` — run all Go tests (`go test ./...`)
- `go vet ./...` — lint (no dedicated linter config; `go vet` is the primary static check)

### Non-obvious gotchas

- **Python symlink required**: The builtin `split` tool in the worker's tool catalog runs `python -c "..."`. The sandbox restricts PATH to `/usr/bin:/bin`. If only `python3` is installed, you must create a symlink: `sudo ln -sf /usr/bin/python3 /usr/bin/python`. Without this, `tool_execution` tasks with `op: split` fail with exit status 127.
- **LLM inference tasks require an actual backend**: `llm_inference` tasks fail without Ollama (or another configured backend like vLLM). Set `SPLAI_OLLAMA_BASE_URL` to point at a running Ollama instance. This is expected in local dev without a GPU — the other task types (`tool_execution`, `embedding` with local backend, `retrieval` with local backend, `aggregation`) all work without external services.
- **Job "Failed" is normal without LLM backend**: When the planner decomposes a "chat" job containing "analyze", it creates a DAG with `tool_execution` → `embedding` → `llm_inference` → `aggregation`. The first two complete locally; `llm_inference` will fail without Ollama. This is expected behavior, not a bug.
- **Submitting a hello-world job**: `curl -s -X POST http://localhost:8080/v1/jobs -H 'content-type: application/json' -d '{"type":"chat","input":"Analyze 500 support tickets and produce root causes.","policy":"enterprise-default","priority":"interactive"}' | jq`
- **Health check**: `curl -s http://localhost:8080/healthz` should return `{"status":"ok"}`.
