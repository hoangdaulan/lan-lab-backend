package judge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hoangdaulan/lan-lab-backend/internal/events"
)

// Every transaction here locks in the order problem -> judge_run ->
// judge_job (BACKEND_CONTEXT.md §10) and reads time from the database
// (clock_timestamp()), so all workers share one clock.

type pgRepository struct {
	pool *pgxpool.Pool
}

var _ Repository = (*pgRepository)(nil)

func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgRepository{pool: pool}
}

func newLeaseToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40 // UUID v4
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

func seconds(d time.Duration) float64 { return d.Seconds() }

// lockedState is a job with its run and problem, read under lock.
type lockedState struct {
	problemVersion int
	runStatus      string
	runVersion     int
	jobStatus      string
	leaseToken     *string
	leaseExpired   bool
	available      bool
	attempt        int
}

// lockJob locks problem (SHARE) -> run (FOR UPDATE) -> job (FOR UPDATE, or
// SKIP LOCKED when skipLocked). ok is false when the job row is locked by
// someone else (skipLocked) or gone.
func lockJob(ctx context.Context, tx pgx.Tx, jobID int64, skipLocked bool) (st lockedState, runID int64, ok bool, err error) {
	var problemID int64
	err = tx.QueryRow(ctx,
		`SELECT r.id, s.problem_id FROM judge_jobs j
		 JOIN judge_runs r ON r.id = j.judge_run_id
		 JOIN submissions s ON s.id = r.submission_id
		 WHERE j.id = $1`, jobID).Scan(&runID, &problemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, 0, false, nil
	}
	if err != nil {
		return st, 0, false, err
	}
	if err = tx.QueryRow(ctx, `SELECT version FROM problems WHERE id = $1 FOR SHARE`, problemID).
		Scan(&st.problemVersion); err != nil {
		return st, 0, false, err
	}
	if err = tx.QueryRow(ctx, `SELECT status, problem_version FROM judge_runs WHERE id = $1 FOR UPDATE`, runID).
		Scan(&st.runStatus, &st.runVersion); err != nil {
		return st, 0, false, err
	}
	q := `SELECT status, lease_token::text, lease_expires_at <= clock_timestamp(),
		available_at <= clock_timestamp(), attempt_count FROM judge_jobs WHERE id = $1 FOR UPDATE`
	if skipLocked {
		q += ` SKIP LOCKED`
	}
	var expired *bool
	err = tx.QueryRow(ctx, q, jobID).Scan(&st.jobStatus, &st.leaseToken, &expired, &st.available, &st.attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, runID, false, nil
	}
	if err != nil {
		return st, 0, false, err
	}
	st.leaseExpired = expired != nil && *expired
	return st, runID, true, nil
}

// obsoleteRun marks a run and its job obsolete (version changed), clearing
// the verdict and lease as the CHECK constraints require.
func obsoleteRun(ctx context.Context, tx pgx.Tx, runID int64) error {
	if _, err := tx.Exec(ctx,
		`UPDATE judge_runs SET status = 'obsolete', verdict = NULL WHERE id = $1`, runID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx,
		`UPDATE judge_jobs SET status = 'obsolete', worker_id = NULL, lease_token = NULL,
		   lease_expires_at = NULL, updated_at = clock_timestamp() WHERE judge_run_id = $1`, runID)
	return err
}

// resetRun clears results of a previous attempt (§10: no mixing attempts).
func resetRun(ctx context.Context, tx pgx.Tx, runID int64) error {
	if _, err := tx.Exec(ctx, `DELETE FROM submission_test_results WHERE judge_run_id = $1`, runID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx,
		`UPDATE judge_runs SET status = 'queued', verdict = NULL, passed_tests = 0, total_tests = 0,
		   compiler_info = NULL, compile_output = NULL, system_error = NULL,
		   started_at = NULL, finished_at = NULL WHERE id = $1`, runID)
	return err
}

func (r *pgRepository) Claim(ctx context.Context, workerID string, lease time.Duration) (Job, error) {
	// Candidates are read without locks; each is then locked in order and
	// re-checked, skipping rows another worker holds.
	rows, err := r.pool.Query(ctx,
		`SELECT id FROM judge_jobs WHERE status = 'queued' AND available_at <= clock_timestamp()
		 ORDER BY available_at, id LIMIT 16`)
	if err != nil {
		return Job{}, err
	}
	candidates, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return Job{}, err
	}
	for _, jobID := range candidates {
		job, err := r.tryClaim(ctx, jobID, workerID, lease)
		if errors.Is(err, ErrNoJob) {
			continue
		}
		return job, err
	}
	return Job{}, ErrNoJob
}

func (r *pgRepository) tryClaim(ctx context.Context, jobID int64, workerID string, lease time.Duration) (Job, error) {
	token, err := newLeaseToken()
	if err != nil {
		return Job{}, err
	}
	var job Job
	err = inTx(ctx, r.pool, func(tx pgx.Tx) error {
		st, runID, ok, err := lockJob(ctx, tx, jobID, true)
		if err != nil {
			return err
		}
		if !ok || st.jobStatus != "queued" || !st.available || st.runStatus != "queued" {
			return ErrNoJob
		}
		if st.runVersion != st.problemVersion {
			// Created before a version change and not yet requeued: never
			// judge it as current (§8).
			if err := obsoleteRun(ctx, tx, runID); err != nil {
				return err
			}
			return errSkipCommit
		}
		if err := resetRun(ctx, tx, runID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx,
			`UPDATE judge_jobs SET status = 'running', worker_id = $2, lease_token = $3::uuid,
			   lease_expires_at = clock_timestamp() + make_interval(secs => $4),
			   attempt_count = attempt_count + 1, updated_at = clock_timestamp()
			 WHERE id = $1 RETURNING attempt_count, created_at`,
			jobID, workerID, token, seconds(lease)).Scan(&job.Attempt, &job.QueuedAt); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE judge_runs SET status = 'judging', started_at = GREATEST(clock_timestamp(), created_at)
			 WHERE id = $1`, runID); err != nil {
			return err
		}
		if err := events.SubmissionOfRun(ctx, tx, runID); err != nil {
			return err
		}
		job.JobID, job.RunID, job.LeaseToken = jobID, runID, token
		if err := tx.QueryRow(ctx,
			`SELECT s.id, s.source_path, p.id, p.version, p.time_limit_ms, p.memory_limit_mb
			 FROM judge_runs r JOIN submissions s ON s.id = r.submission_id JOIN problems p ON p.id = s.problem_id
			 WHERE r.id = $1`, runID).
			Scan(&job.SubmissionID, &job.SourcePath, &job.ProblemID, &job.ProblemVersion,
				&job.TimeLimitMs, &job.MemoryLimitMb); err != nil {
			return err
		}
		rows, err := tx.Query(ctx,
			`SELECT id, input, output FROM test_cases WHERE problem_id = $1 ORDER BY id`, job.ProblemID)
		if err != nil {
			return err
		}
		job.Tests, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (TestCase, error) {
			var t TestCase
			return t, row.Scan(&t.ID, &t.InputPath, &t.OutputPath)
		})
		return err
	})
	if errors.Is(err, errSkipCommit) {
		return Job{}, ErrNoJob
	}
	if err != nil {
		return Job{}, err
	}
	return job, nil
}

// errSkipCommit, returned from an inTx function, commits the transaction
// (e.g. a run just marked obsolete) and is then returned to the caller.
var errSkipCommit = errors.New("committed, nothing else to do")

// inTx runs fn in a transaction: commit on nil or errSkipCommit, roll back
// on any other error.
func inTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	ferr := fn(tx)
	if ferr != nil && !errors.Is(ferr, errSkipCommit) {
		return ferr
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return ferr
}

func (r *pgRepository) Heartbeat(ctx context.Context, job Job, lease time.Duration) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE judge_jobs SET lease_expires_at = clock_timestamp() + make_interval(secs => $3),
		   updated_at = clock_timestamp()
		 WHERE id = $1 AND status = 'running' AND lease_token = $2::uuid AND lease_expires_at > clock_timestamp()`,
		job.JobID, job.LeaseToken, seconds(lease))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

// checkOwned validates, under lock, that this worker may still write.
func checkOwned(ctx context.Context, tx pgx.Tx, job Job) (lockedState, error) {
	st, _, ok, err := lockJob(ctx, tx, job.JobID, false)
	if err != nil {
		return st, err
	}
	if !ok || st.jobStatus != "running" || st.leaseToken == nil || *st.leaseToken != job.LeaseToken || st.leaseExpired {
		return st, ErrLeaseLost
	}
	if st.runVersion != st.problemVersion || st.runStatus != "judging" {
		if err := obsoleteRun(ctx, tx, job.RunID); err != nil {
			return st, err
		}
		return st, errSkipCommit
	}
	return st, nil
}

func (r *pgRepository) Complete(ctx context.Context, job Job, o Outcome) error {
	if err := o.Validate(len(job.Tests)); err != nil {
		return fmt.Errorf("invalid outcome: %w", err)
	}
	err := inTx(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := checkOwned(ctx, tx, job); err != nil {
			return err
		}
		for _, t := range o.Tests {
			// Only tests of this submission's problem may be recorded (§10).
			tag, err := tx.Exec(ctx,
				`INSERT INTO submission_test_results (judge_run_id, test_case_id, verdict, execution_time_ms, memory_used_kb)
				 SELECT $1, id, $3, $4, $5 FROM test_cases WHERE id = $2 AND problem_id = $6`,
				job.RunID, t.TestCaseID, t.Verdict, t.TimeMs, t.MemoryKB, job.ProblemID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return fmt.Errorf("test case %d does not belong to problem %d", t.TestCaseID, job.ProblemID)
			}
		}
		if _, err := tx.Exec(ctx,
			`UPDATE judge_runs SET status = 'completed', verdict = $2, passed_tests = $3, total_tests = $4,
			   compiler_info = NULLIF($5, ''), compile_output = NULLIF($6, ''),
			   finished_at = GREATEST(clock_timestamp(), started_at)
			 WHERE id = $1`,
			job.RunID, o.Verdict, o.PassedTests, o.TotalTests, o.CompilerInfo, o.CompileOutput); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE judge_jobs SET status = 'done', worker_id = NULL, lease_token = NULL,
			   lease_expires_at = NULL, last_error = NULL, updated_at = clock_timestamp() WHERE id = $1`, job.JobID); err != nil {
			return err
		}
		return events.SubmissionOfRun(ctx, tx, job.RunID)
	})
	if errors.Is(err, errSkipCommit) {
		return ErrStale
	}
	return err
}

func (r *pgRepository) Fail(ctx context.Context, job Job, cause string, policy RetryPolicy) (bool, error) {
	final := false
	err := inTx(ctx, r.pool, func(tx pgx.Tx) error {
		st, err := checkOwned(ctx, tx, job)
		if err != nil {
			return err
		}
		final, err = failLocked(ctx, tx, job.JobID, job.RunID, st.attempt, cause, policy)
		return err
	})
	if errors.Is(err, errSkipCommit) {
		return false, ErrStale
	}
	return final, err
}

// failLocked records an infrastructure failure: requeue with backoff, or
// after the last attempt mark the run system_error and the job failed.
// It never turns an infrastructure error into a program verdict (§10).
func failLocked(ctx context.Context, tx pgx.Tx, jobID, runID int64, attempt int, cause string, policy RetryPolicy) (bool, error) {
	if err := events.SubmissionOfRun(ctx, tx, runID); err != nil {
		return false, err
	}
	if attempt >= policy.MaxAttempts {
		if _, err := tx.Exec(ctx, `DELETE FROM submission_test_results WHERE judge_run_id = $1`, runID); err != nil {
			return false, err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE judge_runs SET status = 'system_error', verdict = NULL, passed_tests = 0, total_tests = 0,
			   system_error = $2, started_at = COALESCE(started_at, GREATEST(clock_timestamp(), created_at)),
			   finished_at = GREATEST(clock_timestamp(), COALESCE(started_at, created_at))
			 WHERE id = $1`, runID, cause); err != nil {
			return false, err
		}
		_, err := tx.Exec(ctx,
			`UPDATE judge_jobs SET status = 'failed', worker_id = NULL, lease_token = NULL,
			   lease_expires_at = NULL, last_error = $2, updated_at = clock_timestamp() WHERE id = $1`, jobID, cause)
		return true, err
	}
	if err := resetRun(ctx, tx, runID); err != nil {
		return false, err
	}
	_, err := tx.Exec(ctx,
		`UPDATE judge_jobs SET status = 'queued', worker_id = NULL, lease_token = NULL, lease_expires_at = NULL,
		   available_at = clock_timestamp() + make_interval(secs => $3), last_error = $2,
		   updated_at = clock_timestamp() WHERE id = $1`,
		jobID, cause, seconds(policy.Delay(attempt)))
	return false, err
}

func (r *pgRepository) RecoverExpired(ctx context.Context, policy RetryPolicy) (int, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id FROM judge_jobs WHERE status = 'running' AND lease_expires_at <= clock_timestamp()
		 ORDER BY id LIMIT 100`)
	if err != nil {
		return 0, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		recovered := false
		err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
			st, runID, ok, err := lockJob(ctx, tx, id, true)
			if err != nil || !ok || st.jobStatus != "running" || !st.leaseExpired {
				return err
			}
			recovered = true
			if st.runVersion != st.problemVersion {
				return obsoleteRun(ctx, tx, runID)
			}
			_, err = failLocked(ctx, tx, id, runID, st.attempt, "lease expired (worker stopped or lost connection)", policy)
			return err
		})
		if err != nil {
			return n, err
		}
		if recovered {
			n++
		}
	}
	return n, nil
}
