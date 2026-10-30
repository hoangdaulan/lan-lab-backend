//go:build integration

package dispatch

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/hoangdaulan/lan-lab-backend/internal/judge"
	"github.com/hoangdaulan/lan-lab-backend/internal/testdb"
)

type acJudger struct{}

func (acJudger) Judge(ctx context.Context, job judge.Job) (judge.Outcome, error) {
	time.Sleep(10 * time.Millisecond)
	o := judge.Outcome{Verdict: judge.VerdictAC, PassedTests: len(job.Tests), TotalTests: len(job.Tests), CompilerInfo: "fake"}
	for _, tc := range job.Tests {
		o.Tests = append(o.Tests, judge.TestOutcome{TestCaseID: tc.ID, Verdict: judge.VerdictAC, TimeMs: 1})
	}
	return o, nil
}

// Real queue, real judge.Worker: two workers get jobs pushed over TCP while
// a third only polls; every job is judged exactly once.
func TestDispatchAgainstRealQueue(t *testing.T) {
	pool := testdb.New(t)
	testdb.NewFixture(t, pool, 2, 30)
	repo := judge.NewRepository(pool)
	svc := judge.NewService(repo, acJudger{}, judge.RetryPolicy{MaxAttempts: 3}, 10*time.Second)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	srv := NewServer(repo, ServerConfig{Lease: 10 * time.Second, PollInterval: 20 * time.Millisecond}, quiet)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = srv.Serve(ctx, ln) }()
	go func() { defer wg.Done(); srv.Run(ctx) }()

	for _, id := range []string{"tcp1", "tcp2", "poller"} {
		w := judge.NewWorker(svc, id, 2, 20*time.Millisecond, quiet)
		wg.Add(1)
		go func() { defer wg.Done(); _ = w.Run(ctx) }()
		if id != "poller" {
			wg.Add(1)
			go func() { defer wg.Done(); NewClient(ln.Addr().String(), w, quiet).Run(ctx) }()
		}
	}

	waitFor(t, "all runs completed", func() bool {
		return testdb.Count(t, pool, `SELECT count(*) FROM judge_runs WHERE status = 'completed'`) == 30
	})
	cancel()
	wg.Wait()
	if n := testdb.Count(t, pool, `SELECT count(*) FROM judge_jobs WHERE status <> 'done' OR attempt_count <> 1`); n != 0 {
		t.Fatalf("%d jobs not done exactly once", n)
	}
	if n := testdb.Count(t, pool, `SELECT count(*) FROM submission_test_results`); n != 60 {
		t.Fatalf("test results = %d, want 60", n)
	}
}
