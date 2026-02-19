.PHONY: build test proto infra-up infra-down

build:
	go build ./...

test:
	go test ./...

proto:
	./scripts/gen-proto.sh

infra-up:
	docker compose -f deploy/docker-compose.dev.yml up -d

infra-down:
	docker compose -f deploy/docker-compose.dev.yml down
