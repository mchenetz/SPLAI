# SPLAI Quickstart (Instructional)

This quickstart is for first-time users.

Goal: run SPLAI locally, submit a job, inspect task progress, and read output artifacts.

## Before you begin

You need:

- Go 1.22+
- `curl`
- `jq` (recommended)

Verify:

```bash
go version
curl --version
jq --version
```

## Step 1: Start the API gateway

Open terminal A:

```bash
cd /Users/mchenetz/git/SPLAI
go run ./cmd/api-gateway
```

## Step 2: Start a worker

Open terminal B:

```bash
cd /Users/mchenetz/git/SPLAI
go run ./worker/cmd/worker-agent
```

## Step 3: Submit a job

Open terminal C:

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

Expected response shape:

```json
{
  "job_id": "job-1"
}
```

## Step 4: Track the job

```bash
curl -s http://localhost:8080/v1/jobs/job-1 | jq
```

Possible states:

- `Queued`
- `Assigned`
- `Running`
- `Completed`
- `Failed`

## Step 5: Track individual tasks

```bash
curl -s http://localhost:8080/v1/jobs/job-1/tasks | jq
```

Useful task filters:

Completed tasks:

```bash
curl -s "http://localhost:8080/v1/jobs/job-1/tasks?status=Completed" | jq
```

Queued tasks:

```bash
curl -s "http://localhost:8080/v1/jobs/job-1/tasks?status=Queued" | jq
```

Tasks run by one worker:

```bash
curl -s "http://localhost:8080/v1/jobs/job-1/tasks?worker_id=worker-local" | jq
```

## Step 6: Read task artifacts

Artifacts are written to:

`/tmp/splai-artifacts/<job_id>/<task_id>/output.json`

Example:

```bash
cat /tmp/splai-artifacts/job-1/t2-embed/output.json | jq
```

## Step 7: Cancel a job (optional)

```bash
curl -s -X DELETE http://localhost:8080/v1/jobs/job-1 | jq
```

## Step 8: Stream live job events (optional)

```bash
curl -N http://localhost:8080/v1/jobs/job-1/stream
```

Event types include:

- `job.snapshot`
- `job.status`
- `task.update`
- terminal event such as `job.completed`

## Step 9: Run with two workers (optional)

Open terminal D:

```bash
cd /Users/mchenetz/git/SPLAI
SPLAI_WORKER_ID=worker-2 go run ./worker/cmd/worker-agent
```

Submit another job and inspect task `worker_id` values to see distribution.

## Troubleshooting

If tasks stay queued:

1. Confirm worker process is running.
2. Confirm API gateway is running on `localhost:8080`.
3. Query tasks to inspect status and errors:

```bash
curl -s http://localhost:8080/v1/jobs/job-1/tasks | jq
```

For a full end-to-end guide (including OpenAI-compatible and Kubernetes examples), use:

- `/Users/mchenetz/git/SPLAI/docs/reference/user-guide.md`
