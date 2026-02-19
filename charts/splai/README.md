# SPLAI Helm Chart

Install:

```bash
helm install splai ./charts/splai -n splai-system --create-namespace
```

Override values:

```bash
helm upgrade --install splai ./charts/splai -n splai-system --create-namespace -f values.prod.yaml
```

Common knobs:

- `apiGateway.image.*`
- `worker.image.*`
- `apiGateway.env.SPLAI_POSTGRES_DSN`
- `apiGateway.env.SPLAI_REDIS_ADDR`
- `postgres.enabled`, `redis.enabled`, `minio.enabled`
