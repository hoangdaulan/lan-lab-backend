SQLC_VERSION ?= latest

.PHONY: up down migrate sqlc build test

# Build images and start postgres, api and worker in the background.
up:
	docker compose up -d --build

down:
	docker compose down

# Apply migrations with the migrate binary inside the api image.
# Pass ARGS=down / ARGS=status for other goose commands.
ARGS ?= up
migrate:
	docker compose run --rm --entrypoint /app/migrate api $(ARGS)

# Regenerate internal/db/sqlc from migrations/ + queries/.
sqlc:
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION) generate

build:
	go build -o bin/ ./cmd/...

test:
	go vet ./...
	go test ./...
