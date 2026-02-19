# Control Plane Manifests

This directory holds Kubernetes manifests (or Helm/Kustomize overlays) for:

- API Gateway Deployment + Service
- Planner Deployment + Service
- Scheduler Deployment + Service
- DAEF Operator Deployment
- Postgres / Redis / MinIO dependencies

Use `config/crd/bases/` CRDs before deploying controllers.
