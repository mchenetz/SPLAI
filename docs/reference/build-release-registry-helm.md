# Build, Release, and Helm Registry Guide

This guide explains how to:

1. Compile all SPLAI artifacts
2. Publish release assets (binaries + container images)
3. Publish Helm chart assets to a registry
4. Deploy from registry artifacts with Helm

## 1. Prerequisites

Required tools:

- `go` (same major version used by repo)
- `git`
- `helm` (v3.8+ recommended for OCI workflows)
- `docker` with `buildx` (if using Docker image builds)
- optional: `ko` (recommended for this repo because there are no Dockerfiles)

Optional tools:

- `cosign` for image signing
- `gh` for GitHub release automation

## 2. Version and Release Variables

Set these once per release:

```bash
export VERSION=v0.2.0
export REGISTRY=ghcr.io
export ORG=mchenetz
export IMAGE_PREFIX=${REGISTRY}/${ORG}
export CHART_REF=oci://${REGISTRY}/${ORG}/charts
```

## 3. Compile All Artifacts

Run from repo root:

```bash
cd /Users/mchenetz/git/SPLAI
```

## 3.1 Validate source before packaging

```bash
make test
make helm-lint
```

## 3.2 Generate protobuf artifacts (if proto changed)

```bash
make proto
```

## 3.3 Build local binaries

```bash
make build
make build-worker
make build-splaictl
```

This produces local binaries in `dist/` for worker/bootstrap tools.

## 3.4 Build release binaries (multi-platform)

```bash
mkdir -p dist/release
for target in \
  "linux amd64" \
  "linux arm64" \
  "darwin arm64"; do
  set -- $target
  GOOS=$1 GOARCH=$2 go build -ldflags "-s -w -X main.version=${VERSION}" -o dist/release/splaictl-$1-$2 ./cmd/splaictl
  GOOS=$1 GOARCH=$2 go build -ldflags "-s -w" -o dist/release/splai-worker-$1-$2 ./worker/cmd/worker-agent
  GOOS=$1 GOARCH=$2 go build -ldflags "-s -w" -o dist/release/splai-api-gateway-$1-$2 ./cmd/api-gateway
  GOOS=$1 GOARCH=$2 go build -ldflags "-s -w" -o dist/release/splai-scheduler-$1-$2 ./cmd/scheduler
  GOOS=$1 GOARCH=$2 go build -ldflags "-s -w" -o dist/release/splai-planner-$1-$2 ./cmd/planner
  GOOS=$1 GOARCH=$2 go build -ldflags "-s -w" -o dist/release/splai-operator-$1-$2 ./cmd/operator
done
```

Generate checksums:

```bash
( cd dist/release && shasum -a 256 * > SHA256SUMS )
```

## 4. Build and Push Container Images

This repo currently has no Dockerfiles. Use one of these approaches.

## 4.1 Recommended: `ko` builds directly from Go packages

Install `ko` and login:

```bash
docker login ghcr.io
```

Build and push images:

```bash
export KO_DOCKER_REPO=${IMAGE_PREFIX}

ko build ./cmd/api-gateway --tags ${VERSION}
ko build ./cmd/scheduler --tags ${VERSION}
ko build ./cmd/planner --tags ${VERSION}
ko build ./cmd/operator --tags ${VERSION}
ko build ./worker/cmd/worker-agent --tags ${VERSION}
```

Expected image names:

- `${IMAGE_PREFIX}/api-gateway:${VERSION}`
- `${IMAGE_PREFIX}/scheduler:${VERSION}`
- `${IMAGE_PREFIX}/planner:${VERSION}`
- `${IMAGE_PREFIX}/operator:${VERSION}`
- `${IMAGE_PREFIX}/worker-agent:${VERSION}`

## 4.2 Alternative: Docker buildx with custom Dockerfiles

If your org requires explicit Dockerfiles, add per-service Dockerfiles and use:

```bash
docker buildx build --platform linux/amd64,linux/arm64 -t ${IMAGE_PREFIX}/splai-api-gateway:${VERSION} --push -f Dockerfile.api-gateway .
docker buildx build --platform linux/amd64,linux/arm64 -t ${IMAGE_PREFIX}/splai-scheduler:${VERSION} --push -f Dockerfile.scheduler .
docker buildx build --platform linux/amd64,linux/arm64 -t ${IMAGE_PREFIX}/splai-planner:${VERSION} --push -f Dockerfile.planner .
docker buildx build --platform linux/amd64,linux/arm64 -t ${IMAGE_PREFIX}/splai-operator:${VERSION} --push -f Dockerfile.operator .
docker buildx build --platform linux/amd64,linux/arm64 -t ${IMAGE_PREFIX}/splai-worker-agent:${VERSION} --push -f Dockerfile.worker .
```

## 5. Package and Publish Helm Chart

## 5.1 Set chart/app versions

Update `charts/splai/Chart.yaml`:

- `version`: chart package version (e.g. `0.2.0`)
- `appVersion`: app version string (e.g. `"0.2.0"`)

## 5.2 Package chart

```bash
mkdir -p dist/charts
helm package charts/splai --destination dist/charts
```

## 5.3 Push chart to OCI registry

Login once:

```bash
helm registry login ghcr.io
```

Push:

```bash
helm push dist/charts/splai-0.2.0.tgz ${CHART_REF}
```

## 6. Deploy From Registry Assets With Helm

Create release values file with your published image tags, for example `values.release.yaml`:

```yaml
apiGateway:
  image:
    repository: ghcr.io/mchenetz/splai-api-gateway
    tag: v0.2.0

scheduler:
  image:
    repository: ghcr.io/mchenetz/splai-scheduler
    tag: v0.2.0

planner:
  image:
    repository: ghcr.io/mchenetz/splai-planner
    tag: v0.2.0

worker:
  image:
    repository: ghcr.io/mchenetz/splai-worker-agent
    tag: v0.2.0
```

Install directly from OCI chart registry:

```bash
helm install splai oci://ghcr.io/mchenetz/charts/splai \
  --version 0.2.0 \
  -n splai-system \
  --create-namespace \
  -f values.release.yaml
```

Upgrade:

```bash
helm upgrade splai oci://ghcr.io/mchenetz/charts/splai \
  --version 0.2.0 \
  -n splai-system \
  -f values.release.yaml
```

## 7. Verify Release Deployment

```bash
kubectl -n splai-system get pods
kubectl -n splai-system get svc
kubectl -n splai-system port-forward svc/splai-splai-api-gateway 8080:8080
curl -s http://localhost:8080/healthz
```

Worker readiness check:

```bash
splaictl verify --url http://localhost:8080
```

## 8. Recommended Registry Asset Layout

Suggested tags:

- Immutable version tags: `v0.2.0`
- Optional moving tags: `latest`, `stable`

Suggested assets per release:

- Container images for all runtime components
- Helm chart package in OCI registry
- Binary tarballs/checksums in GitHub release artifacts

## 9. GitHub Release Flow (Example)

1. Tag and push:

```bash
git tag ${VERSION}
git push origin ${VERSION}
```

2. CI pipeline builds:

- binaries + checksums
- images to GHCR
- Helm chart package + push to OCI

3. Publish release notes with:

- image references
- Helm install command
- breaking-change notes

## 10. Common Failure Modes

- `non-fast-forward` on push: run `git fetch && git rebase origin/master`
- Helm OCI push denied: verify `helm registry login` and package permissions
- Image pull failures in cluster: check image repository/tag in values file
- Chart installs but wrong image tag: verify `values.release.yaml` overrides
- Missing worker image in chart: update `worker.image.repository/tag`

## 11. Minimal Release Checklist

- [ ] `make test` passes
- [ ] `make helm-lint` passes
- [ ] binaries built and checksummed
- [ ] images pushed with immutable tag
- [ ] chart version bumped and pushed to OCI
- [ ] install/upgrade validated on cluster
- [ ] release notes include exact install command

## 12. Using Assets Published by GitHub Actions

The workflow file is:

- `.github/workflows/release-assets.yml`

Publishing behavior:

- Push to `master` or `main`:
  - Docker images are tagged `latest`
  - Chart version is generated as `<chart-version>-<branch>.<run-number>`
- Push tag `vX.Y.Z`:
  - Docker images are tagged `X.Y.Z`
  - Helm chart is published as version `X.Y.Z`
- Every run also creates Helm OCI alias tag `latest` for the chart artifact (`.../splai:latest`)

Published image repositories:

- `ghcr.io/<owner>/splai-api-gateway`
- `ghcr.io/<owner>/splai-scheduler`
- `ghcr.io/<owner>/splai-planner`
- `ghcr.io/<owner>/splai-operator`
- `ghcr.io/<owner>/splai-worker-agent`

Published Helm chart repository (OCI):

- `oci://ghcr.io/<owner>/charts/splai`

## 13. Docker: Pull and Run Published Images

## 13.1 Authenticate to GHCR

```bash
echo "<GHCR_PAT>" | docker login ghcr.io -u <github-username> --password-stdin
```

`GHCR_PAT` must have at least `read:packages` to pull private packages.

## 13.2 Pull specific version (tag release)

```bash
export OWNER=mchenetz
export VERSION=0.2.0

docker pull ghcr.io/${OWNER}/splai-api-gateway:${VERSION}
docker pull ghcr.io/${OWNER}/splai-worker-agent:${VERSION}
```

## 13.3 Pull moving latest (branch release)

```bash
export OWNER=mchenetz
docker pull ghcr.io/${OWNER}/splai-api-gateway:latest
docker pull ghcr.io/${OWNER}/splai-worker-agent:latest
```

## 13.4 Run API gateway + worker via Docker images

```bash
export OWNER=mchenetz
export TAG=latest

docker network create splai-net || true

# API gateway (memory store/queue)
docker run -d --name splai-gateway --network splai-net -p 8080:8080 \
  -e SPLAI_STORE=memory \
  -e SPLAI_QUEUE=memory \
  ghcr.io/${OWNER}/splai-api-gateway:${TAG}

# Worker
docker run -d --name splai-worker --network splai-net \
  -e SPLAI_CONTROL_PLANE_URL=http://splai-gateway:8080 \
  -e SPLAI_WORKER_ID=worker-docker-1 \
  -e SPLAI_ARTIFACT_ROOT=/tmp/splai-artifacts \
  ghcr.io/${OWNER}/splai-worker-agent:${TAG}

curl -s http://localhost:8080/healthz
```

## 14. Helm: Deploy from OCI Chart Registry

## 14.1 Authenticate Helm to GHCR

```bash
echo "<GHCR_PAT>" | helm registry login ghcr.io -u <github-username> --password-stdin
```

## 14.2 Create release values referencing published image tags

Create `values.release.yaml`:

```yaml
apiGateway:
  image:
    repository: ghcr.io/mchenetz/splai-api-gateway
    tag: "0.2.0"

scheduler:
  image:
    repository: ghcr.io/mchenetz/splai-scheduler
    tag: "0.2.0"

planner:
  image:
    repository: ghcr.io/mchenetz/splai-planner
    tag: "0.2.0"

worker:
  image:
    repository: ghcr.io/mchenetz/splai-worker-agent
    tag: "0.2.0"
```

## 14.3 Install a versioned release from OCI

```bash
helm install splai oci://ghcr.io/mchenetz/charts/splai \
  --version 0.2.0 \
  --namespace splai-system \
  --create-namespace \
  -f values.release.yaml
```

## 14.4 Upgrade to a newer version

```bash
helm upgrade splai oci://ghcr.io/mchenetz/charts/splai \
  --version 0.2.1 \
  --namespace splai-system \
  -f values.release.yaml
```

## 14.5 Use latest image tags with Helm (branch-release flow)

For non-tag CI releases, set image tags to `latest` in your values file and install the chart version produced by that CI run.

Note: Helm install/upgrade should still use a semver chart version for `--version`. The `:latest` OCI chart tag is provided as a registry alias for tooling and discovery workflows.

```yaml
apiGateway:
  image:
    repository: ghcr.io/mchenetz/splai-api-gateway
    tag: latest
scheduler:
  image:
    repository: ghcr.io/mchenetz/splai-scheduler
    tag: latest
planner:
  image:
    repository: ghcr.io/mchenetz/splai-planner
    tag: latest
worker:
  image:
    repository: ghcr.io/mchenetz/splai-worker-agent
    tag: latest
```

Then install/upgrade using the exact chart version shown in the workflow logs/artifacts.

## 14.6 Verify Helm deployment

```bash
kubectl -n splai-system get pods
kubectl -n splai-system get svc
kubectl -n splai-system port-forward svc/splai-splai-api-gateway 8080:8080
curl -s http://localhost:8080/healthz
```
