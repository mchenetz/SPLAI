# Observability Manifests

This directory holds Prometheus/Grafana observability assets for SPLAI queue reliability.

## Included files

- `prometheus-scrape-config.yaml`: scrape config for `/v1/metrics/prometheus`
- `prometheus-rules.yaml`: alert rules for dead-letter, retry storms, lease churn, assignment errors
- `grafana-dashboard-splai-queue.json`: starter dashboard panels for reliability metrics
- `jaeger.yaml`: all-in-one Jaeger (OTLP ingest + tracing UI)

## Prometheus scrape snippet

`prometheus-scrape-config.yaml` adds:

- `job_name: splai-gateway`
- `metrics_path: /v1/metrics/prometheus`
- target `splai-api-gateway.default.svc.cluster.local:8080`

## Alert response

Alerts map to runbook:

- `docs/reference/queue-reliability-runbook.md`

## Tracing UI

Deploy Jaeger:

```bash
kubectl apply -f deploy/kubernetes/observability/jaeger.yaml
```

Open UI locally:

```bash
kubectl -n splai-system port-forward svc/jaeger 16686:16686
```

Configure SPLAI services to export traces:

- `SPLAI_OTEL_EXPORTER=otlpgrpc`
- `SPLAI_OTEL_ENDPOINT=jaeger.splai-system.svc.cluster.local:4317`
