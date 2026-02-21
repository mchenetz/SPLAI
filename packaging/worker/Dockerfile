FROM golang:1.22-bookworm AS builder
WORKDIR /src
ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -o /out/splai-worker ./worker/cmd/worker-agent

FROM python:3.11-slim-bookworm

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates git \
  && rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir "huggingface_hub[cli]>=0.24.0" \
  && if ! command -v huggingface-cli >/dev/null 2>&1 && command -v hf >/dev/null 2>&1; then ln -s "$(command -v hf)" /usr/local/bin/huggingface-cli; fi

COPY --from=builder /out/splai-worker /usr/local/bin/splai-worker

ENTRYPOINT ["/usr/local/bin/splai-worker"]
