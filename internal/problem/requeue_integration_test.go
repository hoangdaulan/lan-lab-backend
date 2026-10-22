//go:build integration

// Integration tests for requeueing on version changes and manual rejudge,
// with the real problem, submission and judge repositories on PostgreSQL
// (`make test-integration`).
package problem_test

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hoangdaulan/lan-lab-backend/internal/judge"
	"github.com/hoangdaulan/lan-lab-backend/internal/problem"
	"github.com/hoangdaulan/lan-lab-backend/internal/submission"
	"github.com/hoangdaulan/lan-lab-backend/internal/testdb"
)

func acOutcome(job judge.Job) judge.Outcome {
	o := judge.Outcome{Verdict: judge.VerdictAC, PassedTests: len(job.Tests), TotalTests: len(job.Tests)}
	for _, tc := range job.Tests {
		o.Tests = append(o.Tests, judge.TestOutcome{TestCaseID: tc.ID, Verdict: judge.VerdictAC})
	}
	return o
}

func waOutcome(job judge.Job) judge.Outcome {
	return judge.Outcome{Verdict: judge.VerdictWA, TotalTests: len(job.Tests),
		Tests: []judge.TestOutcome{{TestCaseID: job.Tests[0].ID, Verdict: judge.VerdictWA}}}
}

// judgeAll claims and completes every claimable job with outcome.
func judgeAll(t *testing.T, q judge.Repository, outcome func(judge.Job) judge.Outcome) int {
	t.Helper()
	n := 0
	for {
		job, err := q.Claim(context.Background(), "w", 10*time.Second)
		if errors.Is(err, judge.ErrNoJob) {
			return n
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := q.Complete(context.Background(), job, outcome(job)); err != nil {
			t.Fatal(err)
		}
		n++
	}
}

func input(timeMs int) problem.Input {
	return problem.Input{Code: "A", Title: "P", TimeLimitMs: timeMs, MemoryLimitMb: 256}
}

func TestLimitChangeRequeuesEverySubmission(t *testing.T) {
	pool := testdb.New(t)
	f := testdb.NewFixture(t, pool, 2, 4)
	// A second session of the same exam also has a submission.
	other := testdb.AddSession(t, pool, f.ExamID)
	testdb.AddSubmission(t, pool, other, f.ProblemID, "SVX")
	q := judge.NewRepository(pool)
	probs := problem.NewRepository(pool)
	ctx := context.Background()

	// 4 judged AC, one still being judged when the teacher changes the limit.
	for i := 0; i < 4; i++ {
		job, err := q.Claim(ctx, "w", 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if err := q.Complete(ctx, job, acOutcome(job)); err != nil {
			t.Fatal(err)
		}
	}
	inflight, err := q.Claim(ctx, "w", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	p, requeued, bumped, err := probs.Update(ctx, f.ProblemID, input(2000))
	if err != nil || !bumped || requeued != 5 || p.Version != 2 {
		t.Fatalf("Update = v%d requeued=%d bumped=%v err=%v; want v2, 5", p.Version, requeued, bumped, err)
	}
	if n := testdb.Count(t, pool, `SELECT count(*) FROM judge_runs WHERE problem_version = 1 AND status = 'obsolete' AND verdict IS NULL`); n != 5 {
		t.Errorf("obsolete v1 runs = %d, want 5", n)
	}
	if n := testdb.Count(t, pool, `SELECT count(*) FROM judge_runs r JOIN judge_jobs j ON j.judge_run_id = r.id
		WHERE r.problem_version = 2 AND r.status = 'queued' AND j.status = 'queued'`); n != 5 {
		t.Errorf("queued v2 runs+jobs = %d, want 5 (both sessions)", n)
	}
	// The in-flight worker's v1 result is dropped.
	if err := q.Complete(ctx, inflight, acOutcome(inflight)); !errors.Is(err, judge.ErrLeaseLost) {
		t.Errorf("in-flight v1 Complete: %v, want ErrLeaseLost", err)
	}
	// Current results are pending until v2 is judged; then they are v2 results.
	subs := submission.NewRepository(pool)
	s, err := subs.Get(ctx, f.Submissions[0])
	if err != nil || s.Result == nil || s.Result.Status != "queued" || s.Result.ProblemVersion != 2 {
		t.Fatalf("current result after requeue = %+v, %v", s.Result, err)
	}
	if n := judgeAll(t, q, waOutcome); n != 5 {
		t.Fatalf("judged %d v2 jobs, want 5", n)
	}
	// Former AC is lost: the current result is the v2 WA.
	s, _ = subs.Get(ctx, f.Submissions[0])
	if s.Result == nil || s.Result.Verdict == nil || *s.Result.Verdict != "WA" || s.Result.ProblemVersion != 2 {
		t.Fatalf("current result = %+v, want v2 WA", s.Result)
	}
	// A text-only edit requeues nothing.
	if _, requeued, bumped, _ := probs.Update(ctx, f.ProblemID, problem.Input{Code: "A", Title: "Renamed", TimeLimitMs: 2000, MemoryLimitMb: 256}); bumped || requeued != 0 {
		t.Errorf("text edit: bumped=%v requeued=%d", bumped, requeued)
	}
}

func TestReplaceTestsRequeues(t *testing.T) {
	pool := testdb.New(t)
	f := testdb.NewFixture(t, pool, 2, 3)
	q := judge.NewRepository(pool)
	judgeAll(t, q, acOutcome)
	p, requeued, err := problem.NewRepository(pool).ReplaceTests(context.Background(), f.ProblemID, []problem.TestFile{
		{InputPath: "problems/x/tests/1.in", OutputPath: "problems/x/tests/1.out"},
	})
	if err != nil || requeued != 3 || p.Version != 2 {
		t.Fatalf("ReplaceTests = v%d requeued=%d err=%v", p.Version, requeued, err)
	}
	// Old per-test results went with the old test rows (ON DELETE CASCADE).
	if n := testdb.Count(t, pool, `SELECT count(*) FROM submission_test_results`); n != 0 {
		t.Errorf("old per-test results = %d, want 0", n)
	}
	if n := judgeAll(t, q, acOutcome); n != 3 {
		t.Errorf("judged %d, want 3", n)
	}
	if n := testdb.Count(t, pool, `SELECT count(*) FROM judge_runs WHERE problem_version = 2 AND status = 'completed' AND total_tests = 1`); n != 3 {
		t.Errorf("v2 completed runs with the new single test = %d, want 3", n)
	}
}

func TestManualRejudgeOnlyThatSubmission(t *testing.T) {
	pool := testdb.New(t)
	f := testdb.NewFixture(t, pool, 2, 3)
	q := judge.NewRepository(pool)
	judgeAll(t, q, acOutcome)
	subs := submission.NewRepository(pool)
	ctx := context.Background()
	before := map[int64]int64{}
	for _, id := range f.Submissions {
		s, _ := subs.Get(ctx, id)
		before[id] = s.Result.RunID
	}

	target := f.Submissions[1]
	s, err := subs.Rejudge(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if s.Result == nil || s.Result.Status != "queued" || s.Result.RunID == before[target] || s.Result.ProblemVersion != 1 {
		t.Fatalf("after rejudge result = %+v", s.Result)
	}
	if n := testdb.Count(t, pool, `SELECT count(*) FROM judge_runs WHERE id = $1 AND status = 'obsolete'`, before[target]); n != 1 {
		t.Error("old run not obsolete")
	}
	for _, id := range f.Submissions {
		if id == target {
			continue
		}
		s, _ := subs.Get(ctx, id)
		if s.Result.RunID != before[id] || s.Result.Status != "completed" {
			t.Errorf("submission %d changed: %+v", id, s.Result)
		}
	}
	if n := judgeAll(t, q, waOutcome); n != 1 {
		t.Fatalf("judged %d, want only the rejudged one", n)
	}
	// Rejudging twice in a row keeps a single current run.
	if _, err := subs.Rejudge(ctx, target); err != nil {
		t.Fatal(err)
	}
	if _, err := subs.Rejudge(ctx, target); err != nil {
		t.Fatal(err)
	}
	if n := testdb.Count(t, pool, `SELECT count(*) FROM judge_runs WHERE submission_id = $1 AND status <> 'obsolete'`, target); n != 1 {
		t.Errorf("current runs = %d, want 1", n)
	}
}

// TestNoDeadlockRequeueVsWorkers: workers claim/complete while the teacher
// keeps changing the limit and rejudging single submissions.
func TestNoDeadlockRequeueVsWorkers(t *testing.T) {
	pool := testdb.New(t)
	f := testdb.NewFixture(t, pool, 2, 12)
	q := judge.NewRepository(pool)
	probs := problem.NewRepository(pool)
	subs := submission.NewRepository(pool)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	var deadlocks, judged, dropped atomic.Int32
	isDeadlock := func(err error) bool {
		var pgErr *pgconn.PgError
		return errors.As(err, &pgErr) && pgErr.Code == "40P01"
	}
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				job, err := q.Claim(ctx, "w", 10*time.Second)
				if errors.Is(err, judge.ErrNoJob) {
					time.Sleep(2 * time.Millisecond)
					continue
				}
				if err != nil {
					if isDeadlock(err) {
						deadlocks.Add(1)
					}
					continue
				}
				time.Sleep(time.Millisecond)
				switch err := q.Complete(context.Background(), job, acOutcome(job)); {
				case err == nil:
					judged.Add(1)
				case errors.Is(err, judge.ErrLeaseLost), errors.Is(err, judge.ErrStale):
					dropped.Add(1)
				case isDeadlock(err):
					deadlocks.Add(1)
				default:
					t.Errorf("Complete: %v", err)
				}
			}
		}()
	}
	edits := 0
	for ctx.Err() == nil {
		var err error
		if edits%2 == 0 {
			_, _, _, err = probs.Update(context.Background(), f.ProblemID, input(1000+edits%7))
		} else {
			_, err = subs.Rejudge(context.Background(), f.Submissions[rand.IntN(len(f.Submissions))])
		}
		if err != nil {
			if isDeadlock(err) {
				deadlocks.Add(1)
			} else {
				t.Fatalf("edit: %v", err)
			}
		}
		edits++
		time.Sleep(10 * time.Millisecond)
	}
	wg.Wait()
	if deadlocks.Load() != 0 {
		t.Fatalf("%d deadlocks", deadlocks.Load())
	}
	// Invariant: exactly one current run per submission, at the current version.
	if n := testdb.Count(t, pool, `SELECT count(*) FROM submissions s JOIN problems p ON p.id = s.problem_id
		WHERE (SELECT count(*) FROM judge_runs r WHERE r.submission_id = s.id AND r.status <> 'obsolete' AND r.problem_version = p.version) <> 1`); n != 0 {
		t.Errorf("%d submissions without exactly one current run", n)
	}
	if n := testdb.Count(t, pool, `SELECT count(*) FROM judge_runs r JOIN submissions s ON s.id = r.submission_id
		JOIN problems p ON p.id = s.problem_id WHERE r.status <> 'obsolete' AND r.problem_version <> p.version`); n != 0 {
		t.Errorf("%d non-obsolete runs at an old version", n)
	}
	t.Logf("edits=%d judged=%d dropped=%d", edits, judged.Load(), dropped.Load())
}
