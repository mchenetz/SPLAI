.PHONY: build test proto infra-up infra-down build-worker build-splaictl install-worker helm-lint

build:
	go build ./...

build-worker:
	mkdir -p dist
	go build -o dist/splai-worker ./worker/cmd/worker-agent

build-splaictl:
	mkdir -p dist
	go build -o dist/splaictl ./cmd/splaictl

install-worker:
	./scripts/install-worker.sh

test:
	go test ./...

proto:
	./scripts/gen-proto.sh

infra-up:
	docker compose -f deploy/docker-compose.dev.yml up -d

infra-down:
	docker compose -f deploy/docker-compose.dev.yml down

helm-lint:
	helm lint charts/splai
