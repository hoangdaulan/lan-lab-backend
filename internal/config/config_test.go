package config

import (
	"strings"
	"testing"
	"time"
)

// setEnv clears every variable Load reads, then sets the given ones.
func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, k := range []string{"DATABASE_URL", "HTTP_ADDR", "JWT_SECRET", "DATA_DIR", "WORKER_ID",
		"WORKER_CONCURRENCY", "WORKER_POLL_INTERVAL", "WORKER_LEASE", "ACCESS_TOKEN_TTL", "REFRESH_TOKEN_TTL"} {
		t.Setenv(k, "")
	}
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

const goodSecret = "0123456789abcdef0123456789abcdef"

func TestLoadDefaults(t *testing.T) {
	setEnv(t, map[string]string{"DATABASE_URL": "postgres://x", "JWT_SECRET": goodSecret})
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.DataDir != "/data" || cfg.WorkerConcurrency != 2 ||
		cfg.WorkerPollInterval != time.Second || cfg.WorkerLease != time.Minute || cfg.AccessTokenTTL != 15*time.Minute ||
		cfg.RefreshTokenTTL != 7*24*time.Hour || cfg.WorkerID == "" {
		t.Errorf("defaults = %+v", cfg)
	}
	if err := cfg.RequireAPI(); err != nil {
		t.Errorf("RequireAPI: %v", err)
	}
	if err := cfg.RequireWorker(); err != nil {
		t.Errorf("RequireWorker: %v", err)
	}
}

func TestRequireAPIRejectsMissingOrShortSecret(t *testing.T) {
	for _, secret := range []string{"", "too-short"} {
		setEnv(t, map[string]string{"DATABASE_URL": "postgres://x", "JWT_SECRET": secret})
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		err = cfg.RequireAPI()
		if err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
			t.Errorf("secret %q: err = %v, want JWT_SECRET error", secret, err)
		}
	}
}

func TestRequireAPIReportsAllMissing(t *testing.T) {
	setEnv(t, nil)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	err = cfg.RequireAPI()
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") || !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Errorf("err = %v, want both DATABASE_URL and JWT_SECRET", err)
	}
	// The worker does not sign tokens.
	if err := cfg.RequireWorker(); err == nil || strings.Contains(err.Error(), "JWT_SECRET") {
		t.Errorf("RequireWorker err = %v, want DATABASE_URL only", err)
	}
}

func TestLoadRejectsMalformedValues(t *testing.T) {
	cases := map[string]string{
		"WORKER_CONCURRENCY":   "two",
		"WORKER_POLL_INTERVAL": "1 second",
		"ACCESS_TOKEN_TTL":     "15",
		"REFRESH_TOKEN_TTL":    "week",
		"WORKER_LEASE":         "1s",
	}
	for key, val := range cases {
		setEnv(t, map[string]string{key: val})
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("%s=%q: err = %v, want error naming %s", key, val, err, key)
		}
	}
	setEnv(t, map[string]string{"WORKER_CONCURRENCY": "0"})
	if _, err := Load(); err == nil {
		t.Error("WORKER_CONCURRENCY=0 accepted")
	}
}

func TestLoadOverrides(t *testing.T) {
	setEnv(t, map[string]string{"HTTP_ADDR": ":9000", "DATA_DIR": "/srv/data", "WORKER_ID": "w1",
		"WORKER_CONCURRENCY": "8", "ACCESS_TOKEN_TTL": "5m", "REFRESH_TOKEN_TTL": "24h"})
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":9000" || cfg.DataDir != "/srv/data" || cfg.WorkerID != "w1" ||
		cfg.WorkerConcurrency != 8 || cfg.AccessTokenTTL != 5*time.Minute || cfg.RefreshTokenTTL != 24*time.Hour {
		t.Errorf("overrides = %+v", cfg)
	}
}
