# SPLAI Helm Chart

## Install

```bash
helm upgrade --install splai ./charts/splai \
  -n splai-system --create-namespace
```

## Upgrade with values file

```bash
helm upgrade --install splai ./charts/splai \
  -n splai-system --create-namespace \
  -f values.prod.yaml
```

## Full Values Reference

All available values from `charts/splai/values.yaml`:

### Global

| Key | Type | Default | Description |
|---|---|---|---|
| `nameOverride` | string | `""` | Override chart name portion in resource names. |
| `fullnameOverride` | string | `""` | Fully override generated resource base name. |
| `imagePullSecrets` | list | `[]` | Image pull secrets for all workloads. |

### API Gateway

| Key | Type | Default | Description |
|---|---|---|---|
| `apiGateway.replicaCount` | int | `2` | Number of API gateway replicas. |
| `apiGateway.image.repository` | string | `ghcr.io/mchenetz/splai-api-gateway` | Gateway image repository. |
| `apiGateway.image.tag` | string | `latest` | Gateway image tag. |
| `apiGateway.image.pullPolicy` | string | `IfNotPresent` | Gateway image pull policy. |
| `apiGateway.service.type` | string | `ClusterIP` | Service type for gateway. |
| `apiGateway.service.port` | int | `8080` | Gateway service port. |
| `apiGateway.env.SPLAI_STORE` | string | `postgres` | Store backend (`memory` or `postgres`). |
| `apiGateway.env.SPLAI_QUEUE` | string | `redis` | Queue backend (`memory` or `redis`). |
| `apiGateway.env.SPLAI_LEASE_SECONDS` | string | `"15"` | Task lease duration seconds. |
| `apiGateway.env.SPLAI_POSTGRES_DSN` | string | `""` | Optional explicit DSN. If empty, chart computes DSN from postgres values. |
| `apiGateway.env.SPLAI_REDIS_ADDR` | string | `""` | Optional explicit redis address. If empty, chart uses in-cluster redis service. |

### Scheduler

| Key | Type | Default | Description |
|---|---|---|---|
| `scheduler.enabled` | bool | `true` | Deploy standalone scheduler service. |
| `scheduler.replicaCount` | int | `1` | Scheduler replicas. |
| `scheduler.service.type` | string | `ClusterIP` | Scheduler service type. |
| `scheduler.service.port` | int | `8082` | Scheduler service port. |
| `scheduler.image.repository` | string | `ghcr.io/mchenetz/splai-scheduler` | Scheduler image repository. |
| `scheduler.image.tag` | string | `latest` | Scheduler image tag. |
| `scheduler.image.pullPolicy` | string | `IfNotPresent` | Scheduler image pull policy. |

### Planner

| Key | Type | Default | Description |
|---|---|---|---|
| `planner.enabled` | bool | `true` | Deploy standalone planner service. |
| `planner.replicaCount` | int | `1` | Planner replicas. |
| `planner.service.type` | string | `ClusterIP` | Planner service type. |
| `planner.service.port` | int | `8081` | Planner service port. |
| `planner.image.repository` | string | `ghcr.io/mchenetz/splai-planner` | Planner image repository. |
| `planner.image.tag` | string | `latest` | Planner image tag. |
| `planner.image.pullPolicy` | string | `IfNotPresent` | Planner image pull policy. |

### Worker

| Key | Type | Default | Description |
|---|---|---|---|
| `worker.enabled` | bool | `true` | Deploy worker DaemonSet. |
| `worker.image.repository` | string | `ghcr.io/mchenetz/splai-worker-agent` | Worker image repository. |
| `worker.image.tag` | string | `latest` | Worker image tag. |
| `worker.image.pullPolicy` | string | `IfNotPresent` | Worker image pull policy. |
| `worker.extraEnv` | list | `[{name:SPLAI_OLLAMA_BASE_URL,value:http://127.0.0.1:11434},{name:SPLAI_WORKER_BACKENDS,value:ollama}]` | Extra environment variables injected into worker containers. Defaults to node-local Ollama capability. |
| `worker.artifactRoot` | string | `/var/lib/splai/artifacts` | Path used by worker for artifacts. |
| `worker.volume.type` | string | `emptyDir` | Artifact volume mode (`emptyDir`, `hostPath`, or `ephemeralPVC`). |
| `worker.volume.hostPath` | string | `/var/lib/splai/artifacts` | Host path when `worker.volume.type=hostPath`. |
| `worker.volume.ephemeralPVC.size` | string | `20Gi` | Per-worker ephemeral PVC size when `worker.volume.type=ephemeralPVC`. |
| `worker.volume.ephemeralPVC.accessModes` | list | `[ReadWriteOnce]` | Access modes for per-worker ephemeral PVC claims. |
| `worker.volume.ephemeralPVC.storageClassName` | string | `""` | StorageClass for per-worker ephemeral PVC claims. Empty uses cluster default. |
| `worker.maxParallelTasks` | string | `"2"` | Max concurrent tasks per worker. |
| `worker.heartbeatSeconds` | string | `"5"` | Heartbeat period seconds. |
| `worker.pollMillis` | string | `"1500"` | Assignment poll interval milliseconds. |

### Postgres

| Key | Type | Default | Description |
|---|---|---|---|
| `postgres.enabled` | bool | `true` | Deploy in-cluster postgres. |
| `postgres.flavor` | string | `openshift` | Postgres env-var mode (`openshift` or `upstream`). |
| `postgres.dataMountPath` | string | `/var/lib/pgsql/data` | Container mount path for postgres data volume. |
| `postgres.image.repository` | string | `quay.io/sclorg/postgresql-16-c9s` | Postgres image repository. |
| `postgres.image.tag` | string | `latest` | Postgres image tag. |
| `postgres.image.pullPolicy` | string | `IfNotPresent` | Postgres image pull policy. |
| `postgres.auth.username` | string | `splai` | Postgres username. |
| `postgres.auth.password` | string | `splai` | Postgres password. |
| `postgres.auth.database` | string | `splai` | Postgres database name. |
| `postgres.persistence.enabled` | bool | `false` | Enable PVC for postgres data. |
| `postgres.persistence.size` | string | `10Gi` | PVC size when persistence enabled. |
| `postgres.persistence.accessModes` | list | `[ReadWriteOnce]` | Access modes for postgres PVC. |
| `postgres.persistence.storageClassName` | string | `""` | StorageClass for postgres PVC. Empty uses cluster default. |

### Redis

| Key | Type | Default | Description |
|---|---|---|---|
| `redis.enabled` | bool | `true` | Deploy in-cluster redis. |
| `redis.image.repository` | string | `redis` | Redis image repository. |
| `redis.image.tag` | string | `"7"` | Redis image tag. |
| `redis.image.pullPolicy` | string | `IfNotPresent` | Redis image pull policy. |
| `redis.dataMountPath` | string | `/data` | Redis data directory mount path. |
| `redis.securityContextProfile` | string | `auto` | Security profile mode (`auto`, `kubernetes`, `openshift`). In `auto`, OpenShift clusters use the OpenShift profile. |
| `redis.podSecurityContext.fsGroup` | int | `1001` | Pod fsGroup used for Kubernetes profile to ensure writable data volume. |
| `redis.podSecurityContext.fsGroupChangePolicy` | string | `OnRootMismatch` | fsGroup permission propagation policy. |
| `redis.podSecurityContext.seccompProfile.type` | string | `RuntimeDefault` | Pod seccomp profile type. |
| `redis.openshiftPodSecurityContext.seccompProfile.type` | string | `RuntimeDefault` | OpenShift profile pod seccomp type; omits fixed fsGroup to stay SCC-compatible. |
| `redis.containerSecurityContext.runAsNonRoot` | bool | `true` | Run redis container as non-root user. |
| `redis.containerSecurityContext.allowPrivilegeEscalation` | bool | `false` | Disable privilege escalation. |
| `redis.containerSecurityContext.capabilities.drop` | list | `[ALL]` | Drop all Linux capabilities. |
| `redis.volume.type` | string | `emptyDir` | Redis data volume mode (`emptyDir` or `pvc`). |
| `redis.volume.size` | string | `8Gi` | PVC size when `redis.volume.type=pvc`. |
| `redis.volume.accessModes` | list | `[ReadWriteOnce]` | Access modes for redis PVC. |
| `redis.volume.storageClassName` | string | `""` | StorageClass for redis PVC. Empty uses cluster default. |

### MinIO

| Key | Type | Default | Description |
|---|---|---|---|
| `minio.enabled` | bool | `false` | Deploy in-cluster minio. |
| `minio.image.repository` | string | `minio/minio` | MinIO image repository. |
| `minio.image.tag` | string | `RELEASE.2025-01-20T14-49-07Z` | MinIO image tag. |
| `minio.image.pullPolicy` | string | `IfNotPresent` | MinIO image pull policy. |
| `minio.rootUser` | string | `splai` | MinIO root username. |
| `minio.rootPassword` | string | `splaiminio` | MinIO root password. |
| `minio.bucket` | string | `splai-artifacts` | Bucket name used for artifacts. |

## Common Use Cases

### 1) Local dev (in-cluster postgres + redis)

`values.dev.yaml`:

```yaml
apiGateway:
  replicaCount: 1
  image:
    tag: latest

scheduler:
  replicaCount: 1
  image:
    tag: latest

planner:
  replicaCount: 1
  image:
    tag: latest

worker:
  image:
    tag: latest
  volume:
    type: emptyDir

postgres:
  enabled: true
  persistence:
    enabled: false

redis:
  enabled: true
```

### 2) External Postgres/Redis (managed services)

`values.external-data.yaml`:

```yaml
postgres:
  enabled: false

redis:
  enabled: false

apiGateway:
  env:
    SPLAI_STORE: postgres
    SPLAI_QUEUE: redis
    SPLAI_POSTGRES_DSN: "postgres://user:pass@my-postgres:5432/splai?sslmode=require"
    SPLAI_REDIS_ADDR: "my-redis:6379"
```

### 3) Persist worker artifacts on host

`values.worker-hostpath.yaml`:

```yaml
worker:
  volume:
    type: hostPath
    hostPath: /var/lib/splai/artifacts
```

### 4) Per-worker Ollama (node-local) with backend capability advertisement

`values.worker-ollama-local.yaml`:

```yaml
worker:
  extraEnv:
    - name: SPLAI_OLLAMA_BASE_URL
      value: http://127.0.0.1:11434
    - name: SPLAI_WORKER_BACKENDS
      value: ollama
```

### 4) OpenShift-safe Postgres defaults

`values.openshift.yaml`:

```yaml
postgres:
  flavor: openshift
  image:
    repository: quay.io/sclorg/postgresql-16-c9s
    tag: latest
```

### 5) Upstream Postgres image mode

`values.postgres-upstream.yaml`:

```yaml
postgres:
  flavor: upstream
  image:
    repository: postgres
    tag: "16"
```

### 6) Enable MinIO artifact store

`values.minio.yaml`:

```yaml
minio:
  enabled: true
  rootUser: splai
  rootPassword: change-me
  bucket: splai-artifacts
```

### 7) Portworx storage classes for postgres and workers

Use the included example:

```bash
helm upgrade --install splai ./charts/splai \
  -n splai-system --create-namespace \
  -f charts/splai/values.portworx.yaml
```

### 8) Redis PVC with PodSecurity-restricted defaults

`values.redis-pvc.yaml`:

```yaml
redis:
  securityContextProfile: openshift
  volume:
    type: pvc
    size: 8Gi
    storageClassName: px-csi-db
  openshiftPodSecurityContext:
    seccompProfile:
      type: RuntimeDefault
  containerSecurityContext:
    runAsNonRoot: true
    allowPrivilegeEscalation: false
    capabilities:
      drop:
        - ALL
```

### 9) Force Kubernetes security profile for Redis

```yaml
redis:
  securityContextProfile: kubernetes
  podSecurityContext:
    fsGroup: 1001
    fsGroupChangePolicy: OnRootMismatch
    seccompProfile:
      type: RuntimeDefault
```

`charts/splai/values.portworx.yaml`:

```yaml
postgres:
  persistence:
    enabled: true
    storageClassName: px-csi-db

worker:
  volume:
    type: ephemeralPVC
    ephemeralPVC:
      storageClassName: px-csi-worker
```

## Deploy commands

```bash
helm upgrade --install splai ./charts/splai \
  -n splai-system --create-namespace \
  -f values.dev.yaml
```

```bash
helm upgrade --install splai ./charts/splai \
  -n splai-system --create-namespace \
  -f values.external-data.yaml \
  -f values.worker-hostpath.yaml
```
