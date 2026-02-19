# Control Plane Manifests

This directory contains runnable manifests for:

- `namespace.yaml`
- `postgres.yaml`
- `redis.yaml`
- `minio.yaml`
- `api-gateway.yaml`
- `planner.yaml`
- `scheduler.yaml`
- `operator.yaml`

Apply with:

```bash
kubectl apply -k deploy/kubernetes/control-plane
```

Use `config/crd/bases/` CRDs before deploying controllers.
