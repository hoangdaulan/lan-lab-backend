// Package config loads process configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	// DatabaseURL is a pgx connection string, e.g.
	// postgres://user:pass@postgres:5432/judge?sslmode=disable
	DatabaseURL string

	// HTTPAddr is the API listen address. ":8080" listens on all interfaces
	// so students on the LAN can reach http://<server-ip>:8080.
	HTTPAddr string

	// JWTSecret signs access tokens. Refresh tokens are random strings stored
	// only as hashes (see BACKEND_CONTEXT.md §4), so they need no secret.
	// Algorithm is HS256 (BACKEND_CONTEXT.md §17).
	JWTSecret string

	// AccessTokenTTL is the JWT lifetime; RefreshTokenTTL the refresh token's.
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration

	// DataDir is the server-managed root for submitted sources and test files.
	// DB columns store paths relative to this directory.
	DataDir string

	// WorkerID identifies this worker process in judge_jobs.worker_id.
	WorkerID string

	// WorkerConcurrency caps how many submissions one worker judges at once.
	WorkerConcurrency int

	// WorkerPollInterval is how often an idle worker looks for queued jobs.
	WorkerPollInterval time.Duration

	// WorkerLease is how long a claimed job stays reserved without a
	// heartbeat (BACKEND_CONTEXT.md §19: 60 s).
	WorkerLease time.Duration

	// JudgeWorkDir holds per-job scratch directories (compiled binaries,
	// program output); it must not be under DATA_DIR.
	JudgeWorkDir string
}

// Load reads every setting; callers validate what their binary needs with
// RequireAPI / RequireWorker.
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		HTTPAddr:    getenv("HTTP_ADDR", ":8080"),
		JWTSecret:   os.Getenv("JWT_SECRET"),
		DataDir:     getenv("DATA_DIR", "/data"),
		WorkerID:    os.Getenv("WORKER_ID"),

		JudgeWorkDir: getenv("JUDGE_WORK_DIR", "/var/judge"),
	}

	var err error
	if cfg.WorkerConcurrency, err = getenvInt("WORKER_CONCURRENCY", 2); err != nil {
		return Config{}, err
	}
	if cfg.WorkerConcurrency < 1 {
		return Config{}, errors.New("WORKER_CONCURRENCY must be >= 1")
	}
	if cfg.WorkerPollInterval, err = getenvDuration("WORKER_POLL_INTERVAL", time.Second); err != nil {
		return Config{}, err
	}
	if cfg.WorkerLease, err = getenvDuration("WORKER_LEASE", 60*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.WorkerLease < 3*time.Second {
		return Config{}, errors.New("WORKER_LEASE must be at least 3s")
	}
	if cfg.AccessTokenTTL, err = getenvDuration("ACCESS_TOKEN_TTL", 15*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.RefreshTokenTTL, err = getenvDuration("REFRESH_TOKEN_TTL", 7*24*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.WorkerID == "" {
		cfg.WorkerID, _ = os.Hostname()
	}
	return cfg, nil
}

func (c Config) RequireAPI() error {
	var errs []error
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if len(c.JWTSecret) < 32 {
		errs = append(errs, errors.New("JWT_SECRET is required and must be at least 32 characters"))
	}
	if c.DataDir == "" {
		errs = append(errs, errors.New("DATA_DIR is required"))
	}
	return errors.Join(errs...)
}

func (c Config) RequireWorker() error {
	var errs []error
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if c.DataDir == "" {
		errs = append(errs, errors.New("DATA_DIR is required"))
	}
	if c.WorkerID == "" {
		errs = append(errs, errors.New("WORKER_ID is required (hostname lookup failed)"))
	}
	return errors.Join(errs...)
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}

func getenvDuration(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}
