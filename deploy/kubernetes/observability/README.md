# Observability Manifests

This directory holds Prometheus/Grafana observability assets for SPLAI queue reliability.

## Included files

- `prometheus-scrape-config.yaml`: scrape config for `/v1/metrics/prometheus`
- `prometheus-rules.yaml`: alert rules for dead-letter, retry storms, lease churn, assignment errors
- `grafana-dashboard-splai-queue.json`: starter dashboard panels for reliability metrics

## Prometheus scrape snippet

`prometheus-scrape-config.yaml` adds:

- `job_name: splai-gateway`
- `metrics_path: /v1/metrics/prometheus`
- target `splai-api-gateway.default.svc.cluster.local:8080`

## Alert response

Alerts map to runbook:

- `docs/reference/queue-reliability-runbook.md`
