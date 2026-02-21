# SPLAI Integration Guide

This guide shows how to integrate SPLAI with common AI frameworks, infrastructure, and operations tooling.

Use this document when you already have an application stack and want to plug SPLAI into it.

## Integration patterns

SPLAI supports two common patterns:

1. Native async job API (`/v1/jobs`, `/v1/jobs/{id}`, `/v1/jobs/{id}/tasks`, `/v1/jobs/{id}/stream`)
2. OpenAI-compatible API (`/v1/chat/completions`, `/v1/responses`)

Choose native async API if you need explicit job lifecycle control.
Choose OpenAI-compatible API if you want minimal code changes in existing AI app frameworks.

## 1) OpenAI SDK (Python)

Use SPLAI as a drop-in OpenAI endpoint.

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="local-dev-token",  # required by SDK
)

resp = client.chat.completions.create(
    model="llama3-8b-q4",
    messages=[
        {"role": "system", "content": "You are a support analyst."},
        {"role": "user", "content": "Summarize top root causes from this incident set."},
    ],
)

print(resp.choices[0].message.content)
```

## 2) OpenAI SDK (JavaScript/TypeScript)

```ts
import OpenAI from "openai";

const client = new OpenAI({
  baseURL: "http://localhost:8080/v1",
  apiKey: "local-dev-token",
});

const resp = await client.chat.completions.create({
  model: "llama3-8b-q4",
  messages: [{ role: "user", content: "Generate 5 incident themes." }],
});

console.log(resp.choices[0].message.content);
```

## 3) LangChain

```python
from langchain_openai import ChatOpenAI

llm = ChatOpenAI(
    model="llama3-8b-q4",
    base_url="http://localhost:8080/v1",
    api_key="local-dev-token",
)

print(llm.invoke("Classify these support incidents by root cause."))
```

## 4) LlamaIndex

```python
from llama_index.llms.openai import OpenAI

llm = OpenAI(
    model="llama3-8b-q4",
    api_base="http://localhost:8080/v1",
    api_key="local-dev-token",
)

print(llm.complete("Summarize this incident cluster."))
```

## 5) Native async integration (any backend service)

This is the most reliable production pattern.

Submit job:

```bash
curl -s -X POST http://localhost:8080/v1/jobs \
  -H 'content-type: application/json' \
  -d '{
    "type":"chat",
    "input":"Analyze yesterday incident export and produce root causes",
    "policy":"enterprise-default",
    "priority":"batch"
  }' | jq
```

Poll status:

```bash
curl -s http://localhost:8080/v1/jobs/job-1 | jq
```

Stream events instead of polling:

```bash
curl -N http://localhost:8080/v1/jobs/job-1/stream
```

Read task details:

```bash
curl -s http://localhost:8080/v1/jobs/job-1/tasks | jq
```

## 6) Airflow

Use `PythonOperator` or `HttpOperator` with submit + wait loop.

```python
import requests
import time

BASE = "http://splai-api-gateway.splai-system.svc.cluster.local:8080"

job = requests.post(f"{BASE}/v1/jobs", json={
    "type": "chat",
    "input": "Analyze daily ticket export",
    "policy": "enterprise-default",
    "priority": "batch",
}).json()

job_id = job["job_id"]
while True:
    s = requests.get(f"{BASE}/v1/jobs/{job_id}").json()
    if s["status"] in ["Completed", "Failed", "Canceled", "Archived"]:
        print(s)
        break
    time.sleep(2)
```

## 7) Prefect

```python
from prefect import flow, task
import requests, time

BASE = "http://localhost:8080"

@task
def submit_job():
    return requests.post(f"{BASE}/v1/jobs", json={
        "type": "chat",
        "input": "Summarize this batch of incidents",
        "policy": "enterprise-default",
        "priority": "batch",
    }).json()["job_id"]

@task
def wait_job(job_id: str):
    while True:
        status = requests.get(f"{BASE}/v1/jobs/{job_id}").json()
        if status["status"] in ["Completed", "Failed", "Canceled", "Archived"]:
            return status
        time.sleep(2)

@flow
def splai_flow():
    job_id = submit_job()
    result = wait_job(job_id)
    print(result)

splai_flow()
```

## 8) Dagster

```python
import requests, time
from dagster import op, job

BASE = "http://localhost:8080"

@op
def splai_submit():
    return requests.post(f"{BASE}/v1/jobs", json={
        "type": "chat",
        "input": "Find top incident causes",
        "policy": "enterprise-default",
        "priority": "interactive",
    }).json()["job_id"]

@op
def splai_wait(job_id: str):
    while True:
        s = requests.get(f"{BASE}/v1/jobs/{job_id}").json()
        if s["status"] in ["Completed", "Failed", "Canceled", "Archived"]:
            return s
        time.sleep(1)

@job
def splai_job():
    splai_wait(splai_submit())
```

## 9) n8n / Zapier / Make

Use four nodes/modules:

1. HTTP request: `POST /v1/jobs`
2. Delay/wait
3. HTTP request: `GET /v1/jobs/{id}`
4. Branch on `status`

Optional:

- Add another HTTP request to `GET /v1/jobs/{id}/tasks`
- Parse `output_artifact_uri` and fetch artifacts

## 10) Kubernetes + Helm

Install:

```bash
helm upgrade --install splai ./charts/splai \
  -n splai-system --create-namespace
```

Port-forward for local integration testing:

```bash
kubectl -n splai-system port-forward svc/splai-splai-api-gateway 8080:8080
```

Then run same `curl`/SDK examples.

## 11) Argo Workflows

Example step template:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  generateName: splai-run-
spec:
  entrypoint: run
  templates:
  - name: run
    container:
      image: curlimages/curl:8.5.0
      command: [sh, -c]
      args:
      - |
        JOB_ID=$(curl -s -X POST http://splai-splai-api-gateway.splai-system.svc.cluster.local:8080/v1/jobs \
          -H 'content-type: application/json' \
          -d '{"type":"chat","input":"Analyze incidents","policy":"enterprise-default","priority":"batch"}' | sed -n 's/.*"job_id":"\([^"]*\)".*/\1/p')
        echo "job=$JOB_ID"
```

## 12) Model serving backends

SPLAI workers can route `llm_inference` and `embedding` calls to common backends.

Environment examples for worker:

### Ollama

```bash
SPLAI_OLLAMA_BASE_URL=http://ollama:11434
```

### vLLM

```bash
SPLAI_VLLM_BASE_URL=http://vllm:8000
```

### Remote OpenAI-compatible provider

```bash
SPLAI_REMOTE_API_BASE_URL=https://api.openai.com
SPLAI_REMOTE_API_KEY=<token>
```

## 13) Data plane integrations (Postgres, Redis, MinIO)

### Postgres + Redis (control plane persistence)

```bash
SPLAI_STORE=postgres
SPLAI_POSTGRES_DSN='postgres://splai:splai@postgres:5432/splai?sslmode=disable'
SPLAI_QUEUE=redis
SPLAI_REDIS_ADDR='redis:6379'
```

### MinIO artifact backend (worker)

```bash
SPLAI_ARTIFACT_BACKEND=minio
SPLAI_MINIO_ENDPOINT=minio:9000
SPLAI_MINIO_ACCESS_KEY=splai
SPLAI_MINIO_SECRET_KEY=splaiminio
SPLAI_MINIO_BUCKET=splai-artifacts
SPLAI_MINIO_USE_SSL=false
```

## 14) Auth and API gateway integrations

Enable token-based auth in SPLAI:

```bash
SPLAI_API_TOKENS='operator-token:operator|metrics,tenant-a-token:tenant:tenant-a'
```

Client request example:

```bash
curl -s -X POST http://localhost:8080/v1/jobs \
  -H 'authorization: Bearer tenant-a-token' \
  -H 'content-type: application/json' \
  -d '{"type":"chat","input":"hello","policy":"enterprise-default","priority":"interactive"}'
```

## 15) Observability integrations

### Prometheus scraping

```bash
curl -s http://localhost:8080/v1/metrics/prometheus
```

### OpenTelemetry tracing

Set your standard OTel exporter env vars on API gateway/scheduler/worker processes (for your collector).

## 16) CI/CD smoke test integration

Minimal GitHub Actions step:

```yaml
- name: SPLAI smoke test
  run: |
    set -euo pipefail
    go run ./cmd/api-gateway > /tmp/splai-api.log 2>&1 &
    API_PID=$!
    go run ./worker/cmd/worker-agent > /tmp/splai-worker.log 2>&1 &
    WORKER_PID=$!
    sleep 2
    JOB_ID=$(curl -s -X POST http://localhost:8080/v1/jobs \
      -H 'content-type: application/json' \
      -d '{"type":"chat","input":"smoke","policy":"enterprise-default","priority":"interactive"}' | jq -r .job_id)
    test -n "$JOB_ID"
    kill $WORKER_PID $API_PID || true
```

## 17) Recommended production integration strategy

1. Use native async job API for long-running workflows.
2. Use SSE (`/v1/jobs/{id}/stream`) for live UX/progress updates.
3. Use Postgres + Redis for persistence and queue reliability.
4. Use MinIO/S3 for durable artifacts.
5. Use OpenAI-compatible mode only when minimizing migration effort from existing SDK-based apps.

## Related docs

- `/Users/mchenetz/git/SPLAI/docs/reference/quickstart.md`
- `/Users/mchenetz/git/SPLAI/docs/reference/user-guide.md`
- `/Users/mchenetz/git/SPLAI/docs/reference/complete-operations-reference.md`
- `/Users/mchenetz/git/SPLAI/charts/splai/README.md`
