# Worker Manifests

This directory holds a DaemonSet manifest for the DAEF worker agent.

Expected configuration:

- worker identity and locality labels from node metadata
- CPU-first default runtime
- heartbeat interval 5s
- optional tolerations for edge pools
