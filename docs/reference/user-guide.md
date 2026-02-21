# SPLAI User Guide (Beginner-Friendly, End-to-End)

This guide walks you through SPLAI from zero to a complete working run.

You do not need prior knowledge of distributed systems, Kubernetes, or AI infrastructure to follow this guide.

## What you are building

By the end of this guide, you will:

1. Start the SPLAI control plane.
2. Start one or more workers.
3. Submit jobs.
4. Watch tasks execute.
5. Read outputs and artifacts.
6. Run common operational actions (cancel, filter tasks, prefetch models).

## Mental model (simple)

Think of SPLAI as a job manager for AI tasks:

1. You submit one job request.
2. SPLAI turns it into smaller tasks (a DAG).
3. A scheduler assigns tasks to workers.
4. Workers execute tasks and store outputs as artifacts.
5. You query status and fetch results.

## Prerequisites

Install:

- Go 1.22+
- `curl`
- `jq` (optional but strongly recommended)

Check:

```bash
go version
curl --version
jq --version
```

## Step 1: Start SPLAI (local mode)

Open terminal window A:

```bash
cd /Users/mchenetz/git/SPLAI
go run ./cmd/api-gateway
```

Expected result:

- Service starts on `http://localhost:8080`

## Step 2: Start one worker

Open terminal window B:

```bash
cd /Users/mchenetz/git/SPLAI
go run ./worker/cmd/worker-agent
```

Expected result:

- Worker registers automatically.
- Worker starts heartbeat and polls for assignments.

## Step 3: Verify the system is alive

In terminal window C:

```bash
curl -s http://localhost:8080/healthz
```

You should get a healthy response.

## Step 4: Submit your first job

```bash
curl -s -X POST http://localhost:8080/v1/jobs \
  -H 'content-type: application/json' \
  -d '{
    "type":"chat",
    "input":"Analyze 500 support tickets and produce root causes.",
    "policy":"enterprise-default",
    "priority":"interactive"
  }' | jq
```

Expected output pattern:

```json
{
  "job_id": "job-1"
}
```

Save the returned `job_id`.

## Step 5: Watch job status

Replace `job-1` with your ID if needed.

```bash
curl -s http://localhost:8080/v1/jobs/job-1 | jq
```

Status transitions you may see:

- `Queued`
- `Assigned`
- `Running`
- `Completed`
- `Failed`
- `Archived`

## Step 6: Inspect per-task execution

```bash
curl -s http://localhost:8080/v1/jobs/job-1/tasks | jq
```

What to look for:

- `task_id`
- `type` (for example `model_download`, `embedding`, `llm_inference`, `aggregation`)
- `status`
- `attempt`
- `worker_id`
- `output_artifact_uri`

## Step 7: Filter tasks (practical examples)

Only completed tasks:

```bash
curl -s "http://localhost:8080/v1/jobs/job-1/tasks?status=Completed" | jq
```

Only queued tasks:

```bash
curl -s "http://localhost:8080/v1/jobs/job-1/tasks?status=Queued" | jq
```

Only tasks executed by a specific worker:

```bash
curl -s "http://localhost:8080/v1/jobs/job-1/tasks?worker_id=worker-1" | jq
```

## Step 8: Read artifact output files locally

Workers write local artifacts under:

`/tmp/splai-artifacts/<job_id>/<task_id>/output.json`

Example:

```bash
cat /tmp/splai-artifacts/job-1/t2-embed/output.json | jq
```

Typical outputs:

- Embedding task: vectors and model metadata
- Retrieval task: ranked documents and scores
- Aggregation task: merged fields, conflicts, provenance

## Step 9: Run model prefetch (optional but useful)

You can pre-download a model to workers:

```bash
curl -s -X POST http://localhost:8080/v1/admin/models/prefetch \
  -H 'Content-Type: application/json' \
  -d '{
    "model":"meta-llama/Llama-3-8B-Instruct",
    "source":"huggingface",
    "workers":["worker-local"],
    "only_missing":true
  }' | jq
```

Then submit a job that uses that model.

## Step 10: Cancel a job

```bash
curl -s -X DELETE http://localhost:8080/v1/jobs/job-1 | jq
```

Then verify:

```bash
curl -s http://localhost:8080/v1/jobs/job-1 | jq
```

## Multi-worker example

Start another worker in terminal window D:

```bash
cd /Users/mchenetz/git/SPLAI
SPLAI_WORKER_ID=worker-2 go run ./worker/cmd/worker-agent
```

Submit another job and observe task distribution:

```bash
curl -s -X POST http://localhost:8080/v1/jobs \
  -H 'content-type: application/json' \
  -d '{
    "type":"chat",
    "input":"Cluster incident summaries and produce top causes.",
    "policy":"enterprise-default",
    "priority":"batch"
  }' | jq
```

Then:

```bash
curl -s http://localhost:8080/v1/jobs/job-2/tasks | jq
```

You should see different `worker_id` values over time.

## OpenAI-compatible example (drop-in API usage)

Start gateway with compatibility enabled:

```bash
SPLAI_OPENAI_COMPAT=true go run ./cmd/api-gateway
```

Then send OpenAI-style request:

```bash
curl -s -X POST http://localhost:8080/v1/chat/completions \
  -H 'content-type: application/json' \
  -d '{
    "model":"llama3-8b-q4",
    "messages":[
      {"role":"system","content":"You are a support analyst."},
      {"role":"user","content":"Summarize likely root causes from these incidents."}
    ],
    "stream":false
  }' | jq
```

## Kubernetes example (Helm)

Install:

```bash
helm upgrade --install splai ./charts/splai \
  -n splai-system --create-namespace
```

Port-forward API gateway:

```bash
kubectl -n splai-system port-forward svc/splai-splai-api-gateway 8080:8080
```

Then use the same `curl` job commands from this guide.

## Common troubleshooting

### Job stuck in `Queued`

Check workers are running and connected:

```bash
curl -s "http://localhost:8080/v1/jobs/job-1/tasks?status=Queued" | jq
```

If all tasks are queued and no workers are active, start a worker process.

### Task retries repeatedly

Inspect task error text:

```bash
curl -s http://localhost:8080/v1/jobs/job-1/tasks | jq
```

Look at `error`, `attempt`, and `type`.

### Model download failures

Ensure worker image/runtime has one of:

- `hf` (preferred)
- `huggingface-cli`
- `git`

## Next steps

1. Use persistent mode with Postgres and Redis:
   `/Users/mchenetz/git/SPLAI/docs/reference/persistent-control-plane.md`
2. Use Helm values examples:
   `/Users/mchenetz/git/SPLAI/charts/splai/README.md`
3. Use full API operations reference:
   `/Users/mchenetz/git/SPLAI/docs/reference/complete-operations-reference.md`
