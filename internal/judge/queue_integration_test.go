//go:build integration

// Integration tests for the durable queue against a real PostgreSQL.
//
//	TEST_DATABASE_URL=postgres://user:pass@127.0.0.1:5433/judge?sslmode=disable \
//	  go test -race -count=1 -tags=integration ./internal/judge/
//
// Each test creates its own database from the embedded migrations and drops
// it afterwards.
package judge

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/hoangdaulan/lan-lab-backend/migrations"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// fastRetry keeps the production attempt count but no waiting.
var fastRetry = RetryPolicy{MaxAttempts: 3}

func newTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	admin := os.Getenv("TEST_DATABASE_URL")
	if admin == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	var b [4]byte
	_, _ = rand.Read(b[:])
	name := "judge_it_" + hex.EncodeToString(b[:])

	adminConn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adminConn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = adminConn.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		adminConn.Close(context.Background())
	})

	u, _ := url.Parse(admin)
	u.Path = "/" + name
	dbURL := u.String()

	sqlDB, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatal(err)
	}
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(sqlDB, "."); err != nil {
		t.Fatal(err)
	}
	sqlDB.Close()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// fixture creates one exam/session/problem with `tests` test cases and
// `n` submissions, each with a queued run + job. Returns the problem id.
func fixture(t *testing.T, pool *pgxpool.Pool, tests, n int) int64 {
	t.Helper()
	ctx := context.Background()
	var teacherID, examID, problemID, sessionID int64
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO users (code, fullname, role, password_hash) VALUES ('GV','T','teacher','x') RETURNING id`).Scan(&teacherID))
	must(pool.QueryRow(ctx, `INSERT INTO exams (teacher_id, title) VALUES ($1,'E') RETURNING id`, teacherID).Scan(&examID))
	must(pool.QueryRow(ctx, `INSERT INTO problems (exam_id, code, title, time_limit_ms, memory_limit_mb) VALUES ($1,'A','P',1000,256) RETURNING id`, examID).Scan(&problemID))
	for i := 1; i <= tests; i++ {
		_, err := pool.Exec(ctx, `INSERT INTO test_cases (problem_id, input, output) VALUES ($1, $2, $3)`,
			problemID, fmt.Sprintf("problems/x/tests/%d.in", i), fmt.Sprintf("problems/x/tests/%d.out", i))
		must(err)
	}
	must(pool.QueryRow(ctx, `INSERT INTO exam_sessions (exam_id, name, starts_at, ends_at)
		VALUES ($1,'S', now() - interval '1 hour', now() + interval '1 hour') RETURNING id`, examID).Scan(&sessionID))
	for i := 0; i < n; i++ {
		var studentID, partID, subID, runID int64
		must(pool.QueryRow(ctx, `INSERT INTO users (code, fullname, role, password_hash) VALUES ($1,'S','student','x') RETURNING id`,
			fmt.Sprintf("SV%03d", i)).Scan(&studentID))
		must(pool.QueryRow(ctx, `INSERT INTO exam_participants (exam_session_id, student_id) VALUES ($1,$2) RETURNING id`,
			sessionID, studentID).Scan(&partID))
		must(pool.QueryRow(ctx, `INSERT INTO submissions (exam_participant_id, problem_id, source_path, received_at)
			VALUES ($1,$2,'submissions/x.cpp', now()) RETURNING id`, partID, problemID).Scan(&subID))
		must(pool.QueryRow(ctx, `INSERT INTO judge_runs (submission_id, problem_version) VALUES ($1, 1) RETURNING id`, subID).Scan(&runID))
		_, err := pool.Exec(ctx, `INSERT INTO judge_jobs (judge_run_id) VALUES ($1)`, runID)
		must(err)
	}
	return problemID
}

// acJudger returns AC for every test, counting calls per run.
type acJudger struct {
	mu    sync.Mutex
	calls map[int64]int
	delay time.Duration
}

func (j *acJudger) Judge(ctx context.Context, job Job) (Outcome, error) {
	j.mu.Lock()
	if j.calls == nil {
		j.calls = map[int64]int{}
	}
	j.calls[job.RunID]++
	j.mu.Unlock()
	select {
	case <-time.After(j.delay):
	case <-ctx.Done():
		return Outcome{}, ctx.Err()
	}
	return acOutcome(job), nil
}

func acOutcome(job Job) Outcome {
	o := Outcome{Verdict: VerdictAC, PassedTests: len(job.Tests), TotalTests: len(job.Tests), CompilerInfo: "fake"}
	for _, tc := range job.Tests {
		o.Tests = append(o.Tests, TestOutcome{TestCaseID: tc.ID, Verdict: VerdictAC, TimeMs: 1})
	}
	return o
}

func count(t *testing.T, pool *pgxpool.Pool, q string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestTwoWorkersJudgeEachJobExactlyOnce(t *testing.T) {
	pool := newTestDB(t)
	fixture(t, pool, 2, 20)
	j := &acJudger{delay: 20 * time.Millisecond}
	svc := NewService(NewRepository(pool), j, fastRetry, 10*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for _, id := range []string{"w1", "w2"} {
		w := NewWorker(svc, id, 4, 10*time.Millisecond, quiet)
		wg.Add(1)
		go func() { defer wg.Done(); _ = w.Run(ctx) }()
	}
	deadline := time.Now().Add(30 * time.Second)
	for count(t, pool, `SELECT count(*) FROM judge_jobs WHERE status = 'done'`) < 20 {
		if time.Now().After(deadline) {
			t.Fatal("timeout waiting for jobs")
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	wg.Wait()

	if len(j.calls) != 20 {
		t.Fatalf("judged %d distinct runs, want 20", len(j.calls))
	}
	for run, n := range j.calls {
		if n != 1 {
			t.Errorf("run %d judged %d times", run, n)
		}
	}
	if n := count(t, pool, `SELECT count(*) FROM judge_runs WHERE status = 'completed' AND verdict = 'AC' AND passed_tests = 2 AND total_tests = 2`); n != 20 {
		t.Errorf("completed AC runs = %d, want 20", n)
	}
	if n := count(t, pool, `SELECT count(*) FROM judge_jobs WHERE attempt_count = 1 AND worker_id IS NULL AND lease_token IS NULL`); n != 20 {
		t.Errorf("jobs with one attempt and cleared lease = %d, want 20", n)
	}
	if n := count(t, pool, `SELECT count(*) FROM submission_test_results`); n != 40 {
		t.Errorf("test results = %d, want 40", n)
	}
}

func TestExpiredLeaseRecoveredAndOldWorkerDropped(t *testing.T) {
	pool := newTestDB(t)
	fixture(t, pool, 2, 1)
	repo := NewRepository(pool)
	ctx := context.Background()

	old, err := repo.Claim(ctx, "dead-worker", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Claim(ctx, "w2", time.Second); !errors.Is(err, ErrNoJob) {
		t.Fatalf("second claim while leased: %v, want ErrNoJob", err)
	}
	if n, _ := repo.RecoverExpired(ctx, fastRetry); n != 0 {
		t.Fatalf("recovered %d before expiry", n)
	}
	time.Sleep(1200 * time.Millisecond)
	if err := repo.Heartbeat(ctx, old, time.Second); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("heartbeat after expiry: %v, want ErrLeaseLost", err)
	}
	n, err := repo.RecoverExpired(ctx, fastRetry)
	if err != nil || n != 1 {
		t.Fatalf("RecoverExpired = %d, %v", n, err)
	}
	if n := count(t, pool, `SELECT count(*) FROM judge_jobs WHERE status='queued' AND last_error LIKE 'lease expired%'`); n != 1 {
		t.Fatalf("job not requeued with last_error")
	}

	fresh, err := repo.Claim(ctx, "w2", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Attempt != 2 || fresh.RunID != old.RunID {
		t.Fatalf("fresh claim = %+v", fresh)
	}
	if err := repo.Complete(ctx, old, acOutcome(old)); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("old worker Complete: %v, want ErrLeaseLost", err)
	}
	if n := count(t, pool, `SELECT count(*) FROM submission_test_results`); n != 0 {
		t.Fatalf("old worker wrote %d results", n)
	}
	if err := repo.Complete(ctx, fresh, acOutcome(fresh)); err != nil {
		t.Fatalf("new worker Complete: %v", err)
	}
	if n := count(t, pool, `SELECT count(*) FROM submission_test_results`); n != 2 {
		t.Fatalf("results = %d, want 2 (no duplicates)", n)
	}
	if err := repo.Complete(ctx, fresh, acOutcome(fresh)); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("second Complete: %v, want ErrLeaseLost", err)
	}
}

func TestStaleVersionResultDropped(t *testing.T) {
	pool := newTestDB(t)
	problemID := fixture(t, pool, 2, 2)
	repo := NewRepository(pool)
	ctx := context.Background()

	job, err := repo.Claim(ctx, "w1", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	// A teacher changes the tests while the job runs.
	if _, err := pool.Exec(ctx, `UPDATE problems SET version = version + 1 WHERE id = $1`, problemID); err != nil {
		t.Fatal(err)
	}
	if err := repo.Complete(ctx, job, acOutcome(job)); !errors.Is(err, ErrStale) {
		t.Fatalf("Complete after version change: %v, want ErrStale", err)
	}
	if n := count(t, pool, `SELECT count(*) FROM judge_runs WHERE id = $1 AND status = 'obsolete' AND verdict IS NULL`, job.RunID); n != 1 {
		t.Fatal("stale run not obsolete")
	}
	if n := count(t, pool, `SELECT count(*) FROM submission_test_results`); n != 0 {
		t.Fatalf("stale results written: %d", n)
	}
	// The other queued run is at the old version too: claiming skips it and
	// marks it obsolete instead of judging it as current.
	if _, err := repo.Claim(ctx, "w1", 10*time.Second); !errors.Is(err, ErrNoJob) {
		t.Fatalf("claim of old-version run: %v, want ErrNoJob", err)
	}
	if n := count(t, pool, `SELECT count(*) FROM judge_jobs WHERE status = 'obsolete' AND lease_token IS NULL`); n != 2 {
		t.Fatalf("obsolete jobs = %d, want 2", n)
	}
}

type failingJudger struct{ calls atomic.Int32 }

func (f *failingJudger) Judge(context.Context, Job) (Outcome, error) {
	f.calls.Add(1)
	return Outcome{}, errors.New("compiler crashed")
}

func TestInfrastructureFailuresBecomeSystemError(t *testing.T) {
	pool := newTestDB(t)
	fixture(t, pool, 2, 1)
	j := &failingJudger{}
	svc := NewService(NewRepository(pool), j, fastRetry, 10*time.Second)
	w := NewWorker(svc, "w1", 1, time.Millisecond, quiet)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := w.ProcessOne(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if j.calls.Load() != 3 {
		t.Errorf("judge calls = %d, want 3 attempts", j.calls.Load())
	}
	if n := count(t, pool, `SELECT count(*) FROM judge_runs WHERE status = 'system_error' AND verdict IS NULL
		AND system_error = 'compiler crashed' AND finished_at IS NOT NULL`); n != 1 {
		t.Fatal("run is not system_error without verdict")
	}
	if n := count(t, pool, `SELECT count(*) FROM judge_jobs WHERE status = 'failed' AND attempt_count = 3 AND last_error = 'compiler crashed'`); n != 1 {
		t.Fatal("job is not failed after 3 attempts")
	}
}

func TestBackoffDelaysRetry(t *testing.T) {
	pool := newTestDB(t)
	fixture(t, pool, 1, 1)
	slow := RetryPolicy{MaxAttempts: 3, Backoff: []time.Duration{time.Hour}}
	svc := NewService(NewRepository(pool), &failingJudger{}, slow, 10*time.Second)
	w := NewWorker(svc, "w1", 1, time.Millisecond, quiet)
	ctx := context.Background()
	if worked, err := w.ProcessOne(ctx); !worked || err != nil {
		t.Fatalf("first attempt: worked=%v err=%v", worked, err)
	}
	if worked, _ := w.ProcessOne(ctx); worked {
		t.Fatal("job claimed again before its backoff elapsed")
	}
	if n := count(t, pool, `SELECT count(*) FROM judge_jobs WHERE status='queued' AND available_at > now() + interval '59 minutes'`); n != 1 {
		t.Fatal("available_at not pushed by the backoff")
	}
}

// TestNoDeadlockWithConcurrentRejudge runs claims/completions while another
// connection repeatedly does what a rejudge does, in the documented lock
// order (problem -> runs -> jobs).
func TestNoDeadlockWithConcurrentRejudge(t *testing.T) {
	pool := newTestDB(t)
	problemID := fixture(t, pool, 2, 10)
	j := &acJudger{delay: time.Millisecond}
	svc := NewService(NewRepository(pool), j, fastRetry, 10*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	var deadlocks atomic.Int32
	var wg sync.WaitGroup
	for _, id := range []string{"w1", "w2", "w3"} {
		w := NewWorker(svc, id, 2, time.Millisecond, quiet)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				if _, err := w.ProcessOne(ctx); err != nil && isDeadlock(err) {
					deadlocks.Add(1)
				}
			}
		}()
	}
	rejudges := 0
	for ctx.Err() == nil {
		err := pgx.BeginFunc(context.Background(), pool, func(tx pgx.Tx) error {
			var v int
			if err := tx.QueryRow(ctx, `SELECT version FROM problems WHERE id = $1 FOR UPDATE`, problemID).Scan(&v); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `SELECT id FROM judge_runs WHERE submission_id IN
				(SELECT id FROM submissions WHERE problem_id = $1) AND status <> 'obsolete' ORDER BY id FOR UPDATE`, problemID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `SELECT j.id FROM judge_jobs j JOIN judge_runs r ON r.id = j.judge_run_id
				JOIN submissions s ON s.id = r.submission_id WHERE s.problem_id = $1 AND j.status <> 'obsolete' ORDER BY j.id FOR UPDATE`, problemID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE judge_runs SET status = 'obsolete', verdict = NULL WHERE status <> 'obsolete'
				AND submission_id IN (SELECT id FROM submissions WHERE problem_id = $1)`, problemID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE judge_jobs SET status = 'obsolete', worker_id = NULL, lease_token = NULL, lease_expires_at = NULL
				WHERE status <> 'obsolete'`); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE problems SET version = version + 1 WHERE id = $1`, problemID); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `WITH r AS (INSERT INTO judge_runs (submission_id, problem_version)
				SELECT id, $2 FROM submissions WHERE problem_id = $1 RETURNING id)
				INSERT INTO judge_jobs (judge_run_id) SELECT id FROM r`, problemID, v+1)
			return err
		})
		if err != nil && ctx.Err() == nil {
			if isDeadlock(err) {
				deadlocks.Add(1)
			} else {
				t.Fatalf("rejudge: %v", err)
			}
		}
		rejudges++
		time.Sleep(20 * time.Millisecond)
	}
	wg.Wait()
	if deadlocks.Load() != 0 {
		t.Fatalf("%d deadlocks", deadlocks.Load())
	}
	// Whatever is completed must be at the current version.
	if n := count(t, pool, `SELECT count(*) FROM judge_runs r JOIN submissions s ON s.id = r.submission_id
		JOIN problems p ON p.id = s.problem_id WHERE r.status = 'completed' AND r.problem_version <> p.version`); n != 0 {
		t.Fatalf("%d completed runs at an old version are not obsolete", n)
	}
	t.Logf("rejudges=%d judged=%d", rejudges, len(j.calls))
}

func isDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40P01"
}
