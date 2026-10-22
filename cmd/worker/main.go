// Command worker judges queued submissions. It runs as a separate process
// (its own container) so compiling/running student code never happens inside
// the API process.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/hoangdaulan/lan-lab-backend/internal/config"
	"github.com/hoangdaulan/lan-lab-backend/internal/db"
	"github.com/hoangdaulan/lan-lab-backend/internal/judge"
)

func main() {
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

	// The sandbox is not chosen yet (BACKEND_CONTEXT.md §19): with
	// DisabledJudger the worker only recovers expired leases and leaves
	// queued jobs alone.
	svc := judge.NewService(judge.NewRepository(pool), judge.DisabledJudger{}, judge.DefaultRetry, cfg.WorkerLease)
	w := judge.NewWorker(svc, cfg.WorkerID, cfg.WorkerConcurrency, cfg.WorkerPollInterval, log)
	return w.Run(ctx)
}
