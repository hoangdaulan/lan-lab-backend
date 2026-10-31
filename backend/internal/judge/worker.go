package judge

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
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
	// slots bounds jobs judged at once, whether claimed by polling or
	// pushed by the TCP dispatcher (internal/dispatch).
	slots chan struct{}
	// pushed is set while a dispatcher connection is up: the dispatcher
	// claims jobs for this worker, so the polling loops stop claiming.
	pushed atomic.Bool
}

func NewWorker(svc *Service, id string, concurrency int, pollInterval time.Duration, log *slog.Logger) *Worker {
	// Scan for expired leases at least twice per lease, so a crashed
	// worker's job waits at most ~1.5 leases (capped at 30 s per scan).
	recoverEvery := min(30*time.Second, max(svc.lease/2, time.Second))
	return &Worker{svc: svc, id: id, concurrency: concurrency, pollInterval: pollInterval,
		recoverInterval: recoverEvery, log: log.With("worker_id", id), wake: make(chan struct{}, concurrency),
		slots: make(chan struct{}, concurrency)}
}

// ID is the worker id written to judge_jobs.worker_id.
func (w *Worker) ID() string { return w.id }

// Concurrency is how many jobs this worker judges at once.
func (w *Worker) Concurrency() int { return w.concurrency }

// Enabled reports whether this worker can judge (a sandbox is configured).
func (w *Worker) Enabled() bool { return w.svc.Enabled() }

// SetPushed pauses (true) or resumes (false) claiming by polling. Resuming
// wakes the loops so queued jobs are picked up at once.
func (w *Worker) SetPushed(on bool) {
	w.pushed.Store(on)
	if !on {
		w.Wake()
	}
}

// Acquire reserves a judging slot, waiting for one held by a job claimed
// by polling just before the dispatcher connected; Release frees it.
func (w *Worker) Acquire(ctx context.Context) error {
	select {
	case w.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Worker) Release() { <-w.slots }

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
		"lease", w.svc.lease, "recover_every", w.recoverInterval, "max_attempts", w.svc.retry.MaxAttempts)
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
		worked := false
		if !w.pushed.Load() {
			select {
			case w.slots <- struct{}{}:
			case <-ctx.Done():
				return
			}
			var err error
			worked, err = w.ProcessOne(ctx)
			w.Release()
			if err != nil && ctx.Err() == nil {
				w.log.Error("process job", "slot", slot, "err", err)
			}
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
	log := w.JobLog(job)
	log.Info("job claimed", "queue_wait_ms", time.Since(job.QueuedAt).Milliseconds(), "tests", len(job.Tests))
	start := time.Now()
	outcome, jerr := w.Execute(ctx, job, log)
	return true, w.Record(ctx, job, outcome, jerr, time.Since(start), log)
}

// JobLog is the worker logger annotated with the job's identity.
func (w *Worker) JobLog(job Job) *slog.Logger {
	return w.log.With("job_id", job.JobID, "run_id", job.RunID, "submission_id", job.SubmissionID,
		"attempt", job.Attempt)
}

// Execute judges a job this worker holds the lease for, renewing the lease
// in the database while it runs. Judging stops if the lease is lost.
func (w *Worker) Execute(ctx context.Context, job Job, log *slog.Logger) (Outcome, error) {
	// Judge under a context that is cancelled if the lease is lost, so a
	// job reassigned to another worker stops here.
	jctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	hbDone := make(chan struct{})
	go func() {
		defer close(hbDone)
		w.heartbeat(jctx, job, cancel, log)
	}()
	outcome, jerr := w.svc.judger.Judge(jctx, job)
	cancel()
	<-hbDone
	return outcome, jerr
}

// Record writes a judged result (or an infrastructure failure) directly to
// the database. Results dropped because the lease or version moved on are
// logged, not returned as errors.
func (w *Worker) Record(ctx context.Context, job Job, outcome Outcome, jerr error, took time.Duration, log *slog.Logger) error {
	// Writes use a fresh context: a shutdown signal must not drop a result
	// that is already computed.
	wctx, wcancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer wcancel()
	if jerr != nil {
		final, err := w.svc.repo.Fail(wctx, job, jerr.Error(), w.svc.retry)
		switch {
		case errors.Is(err, ErrLeaseLost), errors.Is(err, ErrStale):
			log.Warn("judge failed and result dropped", "cause", jerr, "reason", err)
			return nil
		case err != nil:
			return err
		}
		log.Warn("infrastructure failure", "cause", jerr, "final", final)
		return nil
	}
	switch err := w.svc.repo.Complete(wctx, job, outcome); {
	case errors.Is(err, ErrLeaseLost), errors.Is(err, ErrStale):
		log.Warn("result dropped", "reason", err)
	case err != nil:
		return err
	default:
		log.Info("job done", "verdict", outcome.Verdict, "passed", outcome.PassedTests,
			"total", outcome.TotalTests, "judge_ms", took.Milliseconds())
	}
	return nil
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
