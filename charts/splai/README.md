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

Worker volume mode:

- Default is OpenShift-safe `emptyDir`:
  - `worker.volume.type=emptyDir`
- Optional host mount (requires SCC/PSP permissions):
  - `worker.volume.type=hostPath`
  - `worker.volume.hostPath=/var/lib/splai/artifacts`

Example (force hostPath):

```bash
helm upgrade --install splai ./charts/splai \
  -n splai-system --create-namespace \
  --set worker.volume.type=hostPath \
  --set worker.volume.hostPath=/var/lib/splai/artifacts
```
