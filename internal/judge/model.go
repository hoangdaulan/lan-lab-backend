// Package judge runs the durable judge queue: workers claim queued jobs with
// a lease, judge them without holding DB locks, and record results only
// while their lease and the problem version are still current
// (BACKEND_CONTEXT.md §10, §19).
package judge

import (
	"errors"
	"time"
)

// Job is a claimed judge job plus a consistent snapshot of what to judge,
// read in the claim transaction.
type Job struct {
	JobID          int64
	RunID          int64
	SubmissionID   int64
	ProblemID      int64
	ProblemVersion int
	Attempt        int // attempt_count after this claim (1 = first try)
	LeaseToken     string
	SourcePath     string // relative to DATA_DIR
	TimeLimitMs    int
	MemoryLimitMb  int
	Tests          []TestCase // in judging order (test_cases.id)
	QueuedAt       time.Time  // judge_jobs.created_at, for wait-time logs
}

type TestCase struct {
	ID         int64
	InputPath  string // relative to DATA_DIR
	OutputPath string
}

// Verdicts (BACKEND_CONTEXT.md §6).
const (
	VerdictAC  = "AC"
	VerdictWA  = "WA"
	VerdictTLE = "TLE"
	VerdictMLE = "MLE"
	VerdictRE  = "RE"
	VerdictCE  = "CE"
)

// Outcome is a finished judgement of a program (not an infrastructure
// failure). Tests holds only the tests that ran: judging stops at the first
// non-AC test (BACKEND_CONTEXT.md §19).
type Outcome struct {
	Verdict       string
	PassedTests   int
	TotalTests    int
	CompilerInfo  string
	CompileOutput string
	Tests         []TestOutcome
}

type TestOutcome struct {
	TestCaseID int64
	Verdict    string // AC, WA, TLE, MLE, RE
	TimeMs     int
	MemoryKB   *int64
}

// Validate checks the outcome against the judge_runs CHECK constraints and
// the stop-at-first-failure rule before it is written.
func (o Outcome) Validate(totalTests int) error {
	if o.TotalTests != totalTests || o.TotalTests <= 0 {
		return errors.New("outcome total_tests does not match the snapshot")
	}
	if o.PassedTests < 0 || o.PassedTests > o.TotalTests {
		return errors.New("outcome passed_tests out of range")
	}
	switch o.Verdict {
	case VerdictCE:
		if o.PassedTests != 0 || len(o.Tests) != 0 {
			return errors.New("CE outcome must not have test results")
		}
		return nil
	case VerdictAC:
		if o.PassedTests != o.TotalTests || len(o.Tests) != o.TotalTests {
			return errors.New("AC outcome must pass every test")
		}
	case VerdictWA, VerdictTLE, VerdictMLE, VerdictRE:
		if o.PassedTests >= o.TotalTests || len(o.Tests) != o.PassedTests+1 {
			return errors.New("failed outcome must stop at its first failing test")
		}
		if o.Tests[len(o.Tests)-1].Verdict != o.Verdict {
			return errors.New("outcome verdict must be the last test's verdict")
		}
	default:
		return errors.New("unknown verdict " + o.Verdict)
	}
	for i, t := range o.Tests {
		if i < len(o.Tests)-1 && t.Verdict != VerdictAC {
			return errors.New("only the last test may fail")
		}
		if t.TimeMs < 0 {
			return errors.New("negative execution time")
		}
	}
	return nil
}

var (
	// ErrNoJob: nothing claimable right now.
	ErrNoJob = errors.New("no claimable job")
	// ErrLeaseLost: the job's lease expired or moved to another worker; the
	// caller must drop its result.
	ErrLeaseLost = errors.New("lease lost")
	// ErrStale: the problem version changed; the run was made obsolete.
	ErrStale = errors.New("problem version changed")
)

// RetryPolicy decides how infrastructure failures are retried
// (BACKEND_CONTEXT.md §19: 3 attempts, backoff 5 s / 30 s / 2 min).
type RetryPolicy struct {
	MaxAttempts int
	Backoff     []time.Duration // delay after attempt i (1-based) is Backoff[i-1]
}

var DefaultRetry = RetryPolicy{
	MaxAttempts: 3,
	Backoff:     []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute},
}

// Delay returns the wait before the next attempt after attempt (1-based).
func (p RetryPolicy) Delay(attempt int) time.Duration {
	if len(p.Backoff) == 0 {
		return 0
	}
	if attempt < 1 {
		attempt = 1
	}
	if attempt > len(p.Backoff) {
		attempt = len(p.Backoff)
	}
	return p.Backoff[attempt-1]
}
