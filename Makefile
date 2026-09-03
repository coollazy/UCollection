.PHONY: build run test lint docker-up docker-down

build:
	go build -o bin/ucollection ./cmd/ucollection

run: build
	./bin/ucollection

test:
	go test ./... -race

lint:
	golangci-lint run ./...

docker-up:
	docker compose up --build

docker-down:
	docker compose down
