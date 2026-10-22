// Command worker judges queued submissions. It runs as a separate process
// (its own container) so compiling/running student code never happens inside
// the API process.
//
// The worker runs as root inside its container (CAP_SYS_ADMIN for the
// network namespace, CAP_SETUID/SETGID to switch uid); student programs run
// as unprivileged runner uids with no capabilities (internal/sandbox).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/hoangdaulan/lan-lab-backend/internal/config"
	"github.com/hoangdaulan/lan-lab-backend/internal/cppjudge"
	"github.com/hoangdaulan/lan-lab-backend/internal/db"
	"github.com/hoangdaulan/lan-lab-backend/internal/judge"
	"github.com/hoangdaulan/lan-lab-backend/internal/sandbox"
)

// Runner uids are 20000 + slot; they exist only as numbers (no passwd entry).
const runnerUIDBase = 20000

func main() {
	// Must run first: the same binary is re-executed as the sandbox helper.
	sandbox.Main()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("worker exited", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.RequireWorker(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	judger, err := newJudger(cfg, log)
	if err != nil {
		return err
	}
	svc := judge.NewService(judge.NewRepository(pool), judger, judge.DefaultRetry, cfg.WorkerLease)
	w := judge.NewWorker(svc, cfg.WorkerID, cfg.WorkerConcurrency, cfg.WorkerPollInterval, log)
	return w.Run(ctx)
}

// newJudger returns the C++ judger, or DisabledJudger when the process
// cannot sandbox (not root), so jobs stay queued instead of failing.
func newJudger(cfg config.Config, log *slog.Logger) (judge.Judger, error) {
	if os.Geteuid() != 0 {
		log.Warn("worker is not root: cannot switch to runner uids, judging disabled")
		return judge.DisabledJudger{}, nil
	}
	// Runner uids must not read DATA_DIR (other submissions, expected
	// outputs). The owner (APP_UID, the API and the teacher on the host)
	// keeps full access; root reads through CAP_DAC_OVERRIDE.
	if err := os.Chmod(cfg.DataDir, 0o750); err != nil {
		return nil, fmt.Errorf("protect %s: %w", cfg.DataDir, err)
	}
	j, err := cppjudge.New(cppjudge.Config{
		DataDir: cfg.DataDir,
		WorkDir: cfg.JudgeWorkDir,
		UIDBase: runnerUIDBase,
		Slots:   cfg.WorkerConcurrency,
	})
	if err != nil {
		return nil, err
	}
	log.Info("judger ready", "compiler", j.CompilerInfo(), "flags", cppjudge.DefaultCompileFlags,
		"work_dir", cfg.JudgeWorkDir, "runner_uids", fmt.Sprintf("%d-%d", runnerUIDBase, runnerUIDBase+cfg.WorkerConcurrency-1))
	return j, nil
}
