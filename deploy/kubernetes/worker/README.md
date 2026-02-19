# Worker Manifests

This directory holds a runnable DaemonSet manifest for the SPLAI worker agent.

Included files:

- `daemonset.yaml`
- `kustomization.yaml`

Apply with:

```bash
kubectl apply -k deploy/kubernetes/worker
```
