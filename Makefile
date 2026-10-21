SQLC_VERSION ?= latest

.PHONY: up down migrate seed sqlc build test data

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

# Regenerate internal/db/sqlc from migrations/ + queries/.
sqlc:
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION) generate

build:
	go build -o bin/ ./cmd/...

test:
	go vet ./...
	go test ./...
