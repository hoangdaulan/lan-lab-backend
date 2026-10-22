.PHONY: up down migrate seed build test test-integration test-sandbox data

# ./data is bind-mounted into api and worker; create it before compose does
# (compose would create it as root).
data:
	mkdir -p data

# Build images and start postgres, api and worker in the background.
up: data
	docker compose up -d --build

down:
	docker compose down

# Apply migrations with the migrate binary inside the api image.
# Pass ARGS=down / ARGS=status for other goose commands.
ARGS ?= up
migrate:
	docker compose run --rm --entrypoint /app/migrate api $(ARGS)

# Create the first teacher. Requires SEED_TEACHER_CODE, SEED_TEACHER_FULLNAME
# and SEED_TEACHER_PASSWORD in the environment.
seed:
	docker compose run --rm \
		-e SEED_TEACHER_CODE -e SEED_TEACHER_FULLNAME -e SEED_TEACHER_PASSWORD \
		--entrypoint /app/seed api

build:
	go build -o bin/ ./cmd/...

test:
	go vet ./...
	go test ./...

# Queue/requeue tests against the dev PostgreSQL (127.0.0.1:5433); each test creates
# and drops its own database.
test-integration:
	set -a && . ./.env && set +a && \
	TEST_DATABASE_URL="postgres://$$POSTGRES_USER:$$POSTGRES_PASSWORD@127.0.0.1:5433/$$POSTGRES_DB?sslmode=disable" \
	go test -race -count=1 -tags=integration ./internal/judge/ ./internal/problem/ ./internal/realtime/ ./internal/board/

# Sandbox tests with real g++ and real limits; they need the worker
# container (root + its capabilities), so the test binary runs there.
test-sandbox:
	CGO_ENABLED=0 go test -c -tags=sandbox -o bin/cppjudge-sandbox.test ./internal/cppjudge/
	docker compose run --rm --no-deps -v "$(CURDIR)/bin:/test:ro" \
		--entrypoint /test/cppjudge-sandbox.test worker -test.v -test.count=1
