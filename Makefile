.PHONY: build run test lint docker-up docker-down

build:
	go build -o bin/ucollection ./cmd/ucollection

run: build
	./bin/ucollection

test:
	# -p 1: packages with real-PostgreSQL integration tests share one DB
	# instance/table set, so package test binaries must not run concurrently.
	go test ./... -race -p 1

lint:
	golangci-lint run ./...

docker-up:
	docker compose up --build

docker-down:
	docker compose down
