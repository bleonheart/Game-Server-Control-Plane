SHELL := /bin/sh

.PHONY: dev down build test test-integration lint fmt migrate docker-build docker-game-image

dev: docker-game-image
	docker compose up --build

down:
	docker compose down

build:
	mkdir -p bin
	go build -trimpath -o bin/api ./cmd/api
	go build -trimpath -o bin/worker ./cmd/worker
	go build -trimpath -o bin/migrate ./cmd/migrate

test:
	go test -race ./...

test-integration:
	@test -n "$$TEST_DATABASE_URL" || (echo "TEST_DATABASE_URL is required" && exit 1)
	go test -race ./tests -run Integration

lint:
	gofmt -l . | tee /tmp/gscp-gofmt
	@test ! -s /tmp/gscp-gofmt
	go vet ./...

fmt:
	gofmt -w $$(find . -name '*.go' -type f)

migrate:
	go run ./cmd/migrate

docker-build:
	docker build -t game-server-control-plane:local .

docker-game-image:
	docker build -f deployments/docker/garrysmod.Dockerfile -t game-server-control-plane/garrysmod:local .
