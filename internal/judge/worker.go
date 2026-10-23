package judge

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// Worker runs Concurrency judging loops plus a lease-recovery loop. No DB
// transaction is held while a job is judged (BACKEND_CONTEXT.md §10).
type Worker struct {
	svc             *Service
	id              string
	concurrency     int
	pollInterval    time.Duration
	recoverInterval time.Duration
	log             *slog.Logger
	// wake lets idle loops claim at once when a job may be available
	// (NOTIFY from a submission or rejudge) instead of waiting for the next
	// poll; polling stays as the fallback.
	wake chan struct{}
}

func NewWorker(svc *Service, id string, concurrency int, pollInterval time.Duration, log *slog.Logger) *Worker {
	return &Worker{svc: svc, id: id, concurrency: concurrency, pollInterval: pollInterval,
		recoverInterval: 30 * time.Second, log: log.With("worker_id", id), wake: make(chan struct{}, concurrency)}
}

// Wake signals idle judging loops (non-blocking; extra signals are dropped).
func (w *Worker) Wake() {
	for i := 0; i < cap(w.wake); i++ {
		select {
		case w.wake <- struct{}{}:
		default:
			return
		}
	}
}

// Run blocks until ctx is cancelled and every in-flight job has finished
// or given up its lease.
func (w *Worker) Run(ctx context.Context) error {
	w.log.Info("worker started", "concurrency", w.concurrency, "judging", w.svc.Enabled(),
		"lease", w.svc.lease, "max_attempts", w.svc.retry.MaxAttempts)
	if !w.svc.Enabled() {
		w.log.Warn("no sandbox configured: jobs stay queued; only lease recovery runs")
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.recoverLoop(ctx)
	}()
	if w.svc.Enabled() {
		for i := 0; i < w.concurrency; i++ {
			wg.Add(1)
			go func(slot int) {
				defer wg.Done()
				w.judgeLoop(ctx, slot)
			}(i)
		}
	}
	wg.Wait()
	w.log.Info("worker stopping")
	return nil
}

func (w *Worker) recoverLoop(ctx context.Context) {
	t := time.NewTicker(w.recoverInterval)
	defer t.Stop()
	for {
		if n, err := w.svc.repo.RecoverExpired(ctx, w.svc.retry); err != nil && ctx.Err() == nil {
			w.log.Error("recover expired leases", "err", err)
		} else if n > 0 {
			w.log.Warn("recovered jobs with expired leases", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (w *Worker) judgeLoop(ctx context.Context, slot int) {
	for ctx.Err() == nil {
		worked, err := w.ProcessOne(ctx)
		if err != nil && ctx.Err() == nil {
			w.log.Error("process job", "slot", slot, "err", err)
		}
		if !worked {
			select {
			case <-ctx.Done():
			case <-w.wake:
			case <-time.After(w.pollInterval):
			}
		}
	}
}

// ProcessOne claims and judges at most one job. worked is false when the
// queue had nothing claimable.
func (w *Worker) ProcessOne(ctx context.Context) (worked bool, err error) {
	job, err := w.svc.repo.Claim(ctx, w.id, w.svc.lease)
	if errors.Is(err, ErrNoJob) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	log := w.log.With("job_id", job.JobID, "run_id", job.RunID, "submission_id", job.SubmissionID,
		"attempt", job.Attempt)
	log.Info("job claimed", "queue_wait_ms", time.Since(job.QueuedAt).Milliseconds(), "tests", len(job.Tests))

	// Judge under a context that is cancelled if the lease is lost, so a
	// job reassigned to another worker stops here.
	jctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	hbDone := make(chan struct{})
	go func() {
		defer close(hbDone)
		w.heartbeat(jctx, job, cancel, log)
	}()

	start := time.Now()
	outcome, jerr := w.svc.judger.Judge(jctx, job)
	cancel()
	<-hbDone

	// Writes use a fresh context: a shutdown signal must not drop a result
	// that is already computed.
	wctx, wcancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer wcancel()
	if jerr != nil {
		final, err := w.svc.repo.Fail(wctx, job, jerr.Error(), w.svc.retry)
		switch {
		case errors.Is(err, ErrLeaseLost), errors.Is(err, ErrStale):
			log.Warn("judge failed and result dropped", "cause", jerr, "reason", err)
			return true, nil
		case err != nil:
			return true, err
		}
		log.Warn("infrastructure failure", "cause", jerr, "final", final)
		return true, nil
	}
	switch err := w.svc.repo.Complete(wctx, job, outcome); {
	case errors.Is(err, ErrLeaseLost), errors.Is(err, ErrStale):
		log.Warn("result dropped", "reason", err)
	case err != nil:
		return true, err
	default:
		log.Info("job done", "verdict", outcome.Verdict, "passed", outcome.PassedTests,
			"total", outcome.TotalTests, "judge_ms", time.Since(start).Milliseconds())
	}
	return true, nil
}

// heartbeat renews the lease every third of its duration until ctx ends;
// on loss it cancels judging.
func (w *Worker) heartbeat(ctx context.Context, job Job, cancel context.CancelFunc, log *slog.Logger) {
	t := time.NewTicker(w.svc.lease / 3)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			err := w.svc.repo.Heartbeat(ctx, job, w.svc.lease)
			switch {
			case err == nil, ctx.Err() != nil:
			case errors.Is(err, ErrLeaseLost):
				log.Warn("lease lost, abandoning job")
				cancel()
				return
			default:
				// Transient DB error: keep judging; the next tick retries
				// and Complete re-checks the lease anyway.
				log.Warn("heartbeat failed", "err", err)
			}
		}
	}
}
