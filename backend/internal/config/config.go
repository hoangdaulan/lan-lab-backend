// Package config loads process configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	// DatabaseURL is a pgx connection string, e.g.
	// postgres://user:pass@postgres:5432/judge?sslmode=disable
	DatabaseURL string

	// HTTPAddr is the API listen address. ":8080" listens on all interfaces
	// so students on the LAN can reach http://<server-ip>:8080.
	HTTPAddr string

	// HTTPSAddr is the HTTPS listen address (":8443"); empty disables HTTPS.
	// Browsers allow screen sharing only in a secure context, so students
	// and teachers use https://<server-ip>:8443; plain HTTP stays for tools.
	HTTPSAddr string

	// HTTPSPublicPort is the HTTPS port as seen by browsers (the host port
	// Compose publishes), used to redirect http://<LAN-IP>:8080/ there.
	HTTPSPublicPort string

	// TLSDir holds the self-signed certificate and key (created on start).
	TLSDir string

	// TLSHosts are extra names/IPs the certificate must cover, typically the
	// server's LAN IP (a container only sees its own bridge address).
	TLSHosts []string

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

	// WSAllowedOrigins are extra Origin host patterns allowed to open the
	// WebSocket (e.g. a front-end dev server "localhost:5173"); same-origin
	// is always allowed.
	WSAllowedOrigins []string

	// WorkerListen makes idle workers wake on NOTIFY instead of only
	// polling (WORKER_LISTEN=false disables it, for experiments).
	WorkerListen bool

	// JudgeWorkDir holds per-job scratch directories (compiled binaries,
	// program output); it must not be under DATA_DIR.
	JudgeWorkDir string

	// DispatchAddr is where the API accepts judge workers over the TCP
	// dispatch protocol (":7070"); empty disables it (workers only poll).
	DispatchAddr string

	// DispatchToken is a shared secret workers send in HELLO; empty accepts
	// any worker (only safe when the port is not reachable from the LAN).
	DispatchToken string

	// WorkerDispatchAddr is the dispatcher a worker connects to: host:port,
	// "auto" to find it by UDP discovery, or empty to only poll.
	WorkerDispatchAddr string

	// DiscoveryPort is the UDP port for LAN server discovery (broadcast);
	// 0 disables it.
	DiscoveryPort int

	// DiscoveryInterval is how often the API broadcasts its announcement.
	DiscoveryInterval time.Duration

	// AdvertiseHost is the host/IP put in discovery announcements; empty
	// uses the first TLS_HOSTS entry, else the IP of each interface.
	AdvertiseHost string

	// AnnounceMulticastAddr is the UDP multicast group teacher
	// announcements are mirrored to ("239.255.42.99:9998"); empty disables.
	AnnounceMulticastAddr string
}

// Load reads every setting; callers validate what their binary needs with
// RequireAPI / RequireWorker.
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		HTTPAddr:    getenv("HTTP_ADDR", ":8080"),
		HTTPSAddr:   os.Getenv("HTTPS_ADDR"),
		JWTSecret:   os.Getenv("JWT_SECRET"),
		DataDir:     getenv("DATA_DIR", "/data"),
		WorkerID:    os.Getenv("WORKER_ID"),

		JudgeWorkDir: getenv("JUDGE_WORK_DIR", "/var/judge"),
	}
	cfg.WorkerListen = os.Getenv("WORKER_LISTEN") != "false"
	cfg.HTTPSPublicPort = getenv("HTTPS_PUBLIC_PORT", "8443")
	cfg.TLSDir = getenv("TLS_DIR", strings.TrimRight(cfg.DataDir, "/")+"/tls")
	cfg.TLSHosts = splitList(os.Getenv("TLS_HOSTS"))
	cfg.WSAllowedOrigins = splitList(os.Getenv("WS_ALLOWED_ORIGINS"))
	cfg.DispatchAddr = os.Getenv("DISPATCH_ADDR")
	cfg.DispatchToken = os.Getenv("DISPATCH_TOKEN")
	cfg.WorkerDispatchAddr = os.Getenv("WORKER_DISPATCH_ADDR")
	cfg.AdvertiseHost = os.Getenv("ADVERTISE_HOST")
	if cfg.AdvertiseHost == "" && len(cfg.TLSHosts) > 0 {
		cfg.AdvertiseHost = cfg.TLSHosts[0]
	}
	cfg.AnnounceMulticastAddr = os.Getenv("ANNOUNCE_MULTICAST_ADDR")

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
	if cfg.DiscoveryPort, err = getenvInt("DISCOVERY_PORT", 0); err != nil {
		return Config{}, err
	}
	if cfg.DiscoveryPort < 0 || cfg.DiscoveryPort > 65535 {
		return Config{}, errors.New("DISCOVERY_PORT must be 0-65535")
	}
	if cfg.DiscoveryInterval, err = getenvDuration("DISCOVERY_INTERVAL", 5*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.DiscoveryInterval < 500*time.Millisecond {
		return Config{}, errors.New("DISCOVERY_INTERVAL must be at least 500ms")
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

// splitList parses a comma-separated list, dropping empty items.
func splitList(v string) []string {
	var out []string
	for _, o := range strings.Split(v, ",") {
		if o = strings.TrimSpace(o); o != "" {
			out = append(out, o)
		}
	}
	return out
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
