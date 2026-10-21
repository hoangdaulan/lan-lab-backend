# syntax=docker/dockerfile:1

# One Go build stage produces every binary; the runtime stages differ only in
# what they install. Build a stage with: docker build --target api|worker .

FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/seed ./cmd/seed

# Shared runtime base: unprivileged user and the data dir that the named
# `data` volume is mounted on (a fresh volume copies this ownership).
FROM debian:bookworm-slim AS runtime-base
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates tzdata \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --system --uid 10001 --create-home app \
 && mkdir -p /data \
 && chown app:app /data
ENV DATA_DIR=/data

FROM runtime-base AS api
COPY --from=build /out/api /out/migrate /out/seed /app/
USER app
EXPOSE 8080
ENTRYPOINT ["/app/api"]

# The worker image carries the C++17 toolchain. The g++ version comes from
# Debian bookworm and must be recorded/pinned before grading starts
# (BACKEND_CONTEXT.md §6); judge_runs.compiler_info stores the actual version.
FROM runtime-base AS worker
RUN apt-get update \
 && apt-get install -y --no-install-recommends build-essential \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/worker /app/
USER app
ENTRYPOINT ["/app/worker"]
