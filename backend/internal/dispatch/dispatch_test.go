package dispatch

import (
	"context"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/hoangdaulan/lan-lab-backend/internal/judge"
	"github.com/hoangdaulan/lan-lab-backend/internal/wire"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type fakeRepo struct {
	mu        sync.Mutex
	queue     []judge.Job
	claimedBy map[int64]string
	completed map[int64]judge.Outcome
	failed    map[int64]string
}

func newFakeRepo(n int) *fakeRepo {
	r := &fakeRepo{claimedBy: map[int64]string{}, completed: map[int64]judge.Outcome{}, failed: map[int64]string{}}
	for i := 1; i <= n; i++ {
		r.queue = append(r.queue, judge.Job{JobID: int64(i), RunID: int64(i), Tests: []judge.TestCase{{ID: 1}}})
	}
	return r
}

func (r *fakeRepo) Claim(_ context.Context, workerID string, _ time.Duration) (judge.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.queue) == 0 {
		return judge.Job{}, judge.ErrNoJob
	}
	j := r.queue[0]
	r.queue = r.queue[1:]
	r.claimedBy[j.JobID] = workerID
	return j, nil
}

func (r *fakeRepo) Complete(_ context.Context, job judge.Job, o judge.Outcome) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.completed[job.JobID] = o
	return nil
}

func (r *fakeRepo) Fail(_ context.Context, job judge.Job, cause string, _ judge.RetryPolicy) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failed[job.JobID] = cause
	return false, nil
}

func (r *fakeRepo) counts() (completed, failed int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.completed), len(r.failed)
}

// fakeExec judges instantly (or when release is closed) and records which
// jobs it had to write to the database itself.
type fakeExec struct {
	id       string
	slots    chan struct{}
	release  chan struct{} // nil: judge at once
	started  chan int64
	mu       sync.Mutex
	pushed   bool
	recorded []int64
}

func newFakeExec(id string, n int) *fakeExec {
	return &fakeExec{id: id, slots: make(chan struct{}, n), started: make(chan int64, 100)}
}

func (e *fakeExec) ID() string                    { return e.id }
func (e *fakeExec) Concurrency() int              { return cap(e.slots) }
func (e *fakeExec) SetPushed(on bool)             { e.mu.Lock(); e.pushed = on; e.mu.Unlock() }
func (e *fakeExec) Acquire(context.Context) error { e.slots <- struct{}{}; return nil }
func (e *fakeExec) Release()                      { <-e.slots }
func (e *fakeExec) JobLog(judge.Job) *slog.Logger { return quiet }
func (e *fakeExec) Execute(_ context.Context, job judge.Job, _ *slog.Logger) (judge.Outcome, error) {
	e.started <- job.JobID
	if e.release != nil {
		<-e.release
	}
	return judge.Outcome{Verdict: judge.VerdictAC, PassedTests: 1, TotalTests: 1,
		Tests: []judge.TestOutcome{{TestCaseID: 1, Verdict: judge.VerdictAC}}}, nil
}
func (e *fakeExec) Record(_ context.Context, job judge.Job, _ judge.Outcome, _ error, _ time.Duration, _ *slog.Logger) error {
	e.mu.Lock()
	e.recorded = append(e.recorded, job.JobID)
	e.mu.Unlock()
	return nil
}

func startServer(t *testing.T, repo Repository, ackTimeout ...time.Duration) (*Server, string, context.CancelFunc) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(repo, ServerConfig{Lease: time.Minute, PollInterval: 20 * time.Millisecond,
		Heartbeat: 200 * time.Millisecond, AckTimeout: append(ackTimeout, 0)[0]}, quiet)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{}, 2)
	go func() { _ = s.Serve(ctx, ln); done <- struct{}{} }()
	go func() { s.Run(ctx); done <- struct{}{} }()
	t.Cleanup(func() { cancel(); <-done; <-done })
	return s, ln.Addr().String(), cancel
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestJobsArePushedAndResultsStoredByDispatcher(t *testing.T) {
	repo := newFakeRepo(10)
	_, addr, _ := startServer(t, repo)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	execs := []*fakeExec{newFakeExec("w1", 2), newFakeExec("w2", 3)}
	for _, e := range execs {
		wg.Add(1)
		go func() { defer wg.Done(); NewClient(addr, e, quiet).Run(ctx) }()
	}
	waitFor(t, "10 results", func() bool { c, _ := repo.counts(); return c == 10 })
	cancel()
	wg.Wait()

	if _, f := repo.counts(); f != 0 {
		t.Fatalf("failed jobs: %v", repo.failed)
	}
	for id, w := range repo.claimedBy {
		if w != "w1" && w != "w2" {
			t.Fatalf("job %d claimed for %q", id, w)
		}
	}
	for _, e := range execs {
		if len(e.recorded) != 0 {
			t.Fatalf("%s wrote results itself: %v", e.id, e.recorded)
		}
		if e.pushed {
			t.Fatalf("%s still paused after disconnect", e.id)
		}
	}
}

// When the dispatcher goes away mid-job, the worker still finishes the job
// and writes the result to the database itself.
func TestWorkerFallsBackToDatabaseWhenDispatcherDies(t *testing.T) {
	repo := newFakeRepo(1)
	_, addr, stopServer := startServer(t, repo)

	e := newFakeExec("w1", 1)
	e.release = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { NewClient(addr, e, quiet).Run(ctx); close(done) }()

	<-e.started
	stopServer()
	waitFor(t, "worker to notice", func() bool { e.mu.Lock(); defer e.mu.Unlock(); return !e.pushed })
	close(e.release)
	waitFor(t, "fallback write", func() bool { e.mu.Lock(); defer e.mu.Unlock(); return len(e.recorded) == 1 })
	cancel()
	<-done
	if c, _ := repo.counts(); c != 0 {
		t.Fatal("dispatcher stored a result after it stopped")
	}
}

func TestHandshakeRejectsBadHello(t *testing.T) {
	_, addr, _ := startServer(t, newFakeRepo(0))
	for name, h := range map[string]helloMsg{
		"version":  {WorkerID: "w", Slots: 1, Version: 99},
		"token":    {WorkerID: "w", Slots: 1, Version: ProtocolVersion, Token: "guess"},
		"no id":    {Slots: 1, Version: ProtocolVersion},
		"no slots": {WorkerID: "w", Version: ProtocolVersion},
	} {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		if err := wire.Write(conn, wire.Hello, h); err != nil {
			t.Fatal(err)
		}
		typ, payload, err := wire.Read(conn)
		if err != nil || typ != wire.Error {
			t.Errorf("%s: got %s %q %v, want ERROR", name, typ, payload, err)
		}
		conn.Close()
	}
}

// A worker that never ACKs a JOB gets the job failed back to the queue and
// its connection closed.
func TestUnackedJobIsRequeued(t *testing.T) {
	repo := newFakeRepo(1)
	_, addr, _ := startServer(t, repo, 100*time.Millisecond)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = wire.Write(conn, wire.Hello, helloMsg{WorkerID: "silent", Slots: 1, Version: ProtocolVersion})
	if typ, _, err := wire.Read(conn); err != nil || typ != wire.Welcome {
		t.Fatalf("handshake: %s %v", typ, err)
	}
	if typ, _, err := wire.Read(conn); err != nil || typ != wire.JobMsg {
		t.Fatalf("expected JOB: %s %v", typ, err)
	}
	waitFor(t, "requeue", func() bool { _, f := repo.counts(); return f == 1 })
}
