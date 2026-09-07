.PHONY: build run test lint docker-up docker-down

TEST_DB_CONTAINER := ucollection-test-db-ephemeral
TEST_DB_PORT := 5433
TEST_DATABASE_URL := postgres://ucollection:ucollection@localhost:$(TEST_DB_PORT)/ucollection?sslmode=disable

build:
	go build -o bin/ucollection ./cmd/ucollection

run: build
	./bin/ucollection

test:
	# Fresh, throwaway Postgres per run — same postgres:16-alpine image and
	# credentials as .github/workflows/ci.yml's service container, so a
	# local `make test` reproduces CI's "no leftover state from a previous
	# run" condition instead of drifting against whatever a long-lived local
	# dev container happens to still have in it. Separate port (5433) so
	# this doesn't collide with any persistent local Postgres on 5432.
	docker run -d --rm --name $(TEST_DB_CONTAINER) \
		-e POSTGRES_USER=ucollection -e POSTGRES_PASSWORD=ucollection -e POSTGRES_DB=ucollection \
		-p $(TEST_DB_PORT):5432 postgres:16-alpine >/dev/null
	@until docker exec $(TEST_DB_CONTAINER) pg_isready -U ucollection >/dev/null 2>&1; do sleep 0.5; done
	# -p 1: packages with real-PostgreSQL integration tests share one DB
	# instance/table set, so package test binaries must not run concurrently.
	DATABASE_URL=$(TEST_DATABASE_URL) go test ./... -race -p 1; \
	status=$$?; \
	docker stop $(TEST_DB_CONTAINER) >/dev/null; \
	exit $$status

lint:
	golangci-lint run ./...

docker-up:
	docker compose up --build

docker-down:
	docker compose down
