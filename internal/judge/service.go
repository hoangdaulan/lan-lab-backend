package judge

import (
	"context"
	"errors"
	"time"
)

// Repository is the durable queue (judge_jobs + judge_runs).
type Repository interface {
	// Claim leases one claimable job (queued, available_at reached, run at
	// the current problem version) to workerID and returns its snapshot.
	// Runs left at an old version are marked obsolete instead. ErrNoJob when
	// nothing can be claimed.
	Claim(ctx context.Context, workerID string, lease time.Duration) (Job, error)
	// Heartbeat extends the lease; ErrLeaseLost if it expired or moved.
	Heartbeat(ctx context.Context, job Job, lease time.Duration) error
	// Complete writes the per-test results, the run summary and job done in
	// one transaction, only if the lease is still held and the version is
	// current. ErrLeaseLost / ErrStale otherwise (nothing written; a stale
	// run is marked obsolete).
	Complete(ctx context.Context, job Job, o Outcome) error
	// Fail records an infrastructure failure: requeue with backoff, or after
	// policy.MaxAttempts mark the run system_error (final = true).
	Fail(ctx context.Context, job Job, cause string, policy RetryPolicy) (final bool, err error)
	// RecoverExpired handles running jobs whose lease expired: requeue (as a
	// failed attempt) or obsolete if the version changed. Returns how many.
	RecoverExpired(ctx context.Context, policy RetryPolicy) (int, error)
}

// Judger compiles and runs one job in the sandbox. It returns an Outcome
// for anything the program did (including CE), and an error only for
// infrastructure problems, which are retried.
type Judger interface {
	Judge(ctx context.Context, job Job) (Outcome, error)
}

// ErrJudgingDisabled is returned by DisabledJudger: the sandbox is not
// configured yet, so the worker must not claim jobs.
var ErrJudgingDisabled = errors.New("judging disabled: no sandbox configured")

// DisabledJudger stands in until the sandbox is chosen (phase 9).
type DisabledJudger struct{}

func (DisabledJudger) Judge(context.Context, Job) (Outcome, error) {
	return Outcome{}, ErrJudgingDisabled
}

type Service struct {
	repo   Repository
	judger Judger
	retry  RetryPolicy
	lease  time.Duration
}

func NewService(repo Repository, judger Judger, retry RetryPolicy, lease time.Duration) *Service {
	return &Service{repo: repo, judger: judger, retry: retry, lease: lease}
}

// Enabled reports whether this service may claim jobs.
func (s *Service) Enabled() bool {
	_, disabled := s.judger.(DisabledJudger)
	return !disabled
}
