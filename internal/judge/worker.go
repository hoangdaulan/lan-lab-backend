package judge

import (
	"context"
	"log/slog"
	"time"
)

// Worker polls the durable queue and judges up to Concurrency submissions at
// once. Run currently only ticks; claiming and judging land in the judge step.
//
// Sandbox requirements for that step (BACKEND_CONTEXT.md §11): student
// programs run as an unprivileged user with no network, a wall/CPU timeout
// and memory/process/output limits (setrlimit), and no DB transaction is held
// while compiling or running.
type Worker struct {
	svc          *Service
	id           string
	concurrency  int
	pollInterval time.Duration
	log          *slog.Logger
}

func NewWorker(svc *Service, id string, concurrency int, pollInterval time.Duration, log *slog.Logger) *Worker {
	return &Worker{svc: svc, id: id, concurrency: concurrency, pollInterval: pollInterval, log: log}
}

// Run blocks until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) error {
	w.log.Info("worker started", "worker_id", w.id, "concurrency", w.concurrency)
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.log.Info("worker stopping", "worker_id", w.id)
			return nil
		case <-ticker.C:
			// Placeholder: recover expired leases, then claim and judge jobs.
		}
	}
}
