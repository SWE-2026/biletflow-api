LOCAL_DB := postgres://biletflow:biletflow@localhost:5432/biletflow?sslmode=disable
TEST_DB  := postgres://biletflow:biletflow@localhost:55433/biletflow_test?sslmode=disable

.PHONY: run db-up db-down check test-unit test test-db-up test-db-down

## run: start the API locally against `make db-up` (local mode: codes are returned as demoCode)
run:
	DATABASE_URL="$(LOCAL_DB)" APP_ENV=local CORS_ORIGINS=http://localhost:5173,http://localhost:8081 go run ./cmd/api

db-up:
	docker compose up -d --wait postgres

db-down:
	docker compose stop postgres

## check: everything that must pass before work counts as done
check:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && echo "gofmt: files above need formatting" && exit 1)
	go vet ./...
	go build ./...

## test-unit: no database or network needed
test-unit:
	go test -race ./...

## test: unit + DB-backed tests against a throwaway Postgres on :55433
test: test-db-up
	TEST_DB_DSN="$(TEST_DB)" go test -race -count=1 ./...
	$(MAKE) test-db-down

test-db-up:
	docker compose --profile test up -d --wait postgres-test

test-db-down:
	docker compose --profile test rm -sf postgres-test
