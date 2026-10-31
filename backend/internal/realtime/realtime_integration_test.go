//go:build integration

// End-to-end WebSocket tests: real PostgreSQL (NOTIFY/LISTEN), a real HTTP
// server and real WebSocket clients (`make test-integration`).
package realtime_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hoangdaulan/lan-lab-backend/internal/auth"
	"github.com/hoangdaulan/lan-lab-backend/internal/board"
	"github.com/hoangdaulan/lan-lab-backend/internal/judge"
	"github.com/hoangdaulan/lan-lab-backend/internal/problem"
	"github.com/hoangdaulan/lan-lab-backend/internal/realtime"
	"github.com/hoangdaulan/lan-lab-backend/internal/submission"
	"github.com/hoangdaulan/lan-lab-backend/internal/testdb"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

const secret = "integration-secret-integration-secret"

type verifier struct{ s *auth.JWTSigner }

func (v verifier) VerifyAccessTokenExpiry(tok string) (auth.Principal, time.Time, error) {
	return v.s.VerifyWithExpiry(tok, time.Now())
}

type env struct {
	pool     *pgxpool.Pool
	fx       testdb.Fixture
	students []int64 // user ids, same order as fx.Submissions
	board    *board.Service
	hub      *realtime.Hub
	url      string
	signer   *auth.JWTSigner
}

func setup(t *testing.T, students int) *env {
	t.Helper()
	pool := testdb.New(t)
	fx := testdb.NewFixture(t, pool, 2, students)
	e := &env{pool: pool, fx: fx, signer: auth.NewJWTSigner(secret, 15*time.Minute)}
	for i := 0; i < students; i++ {
		var id int64
		if err := pool.QueryRow(context.Background(), `SELECT id FROM users WHERE code = $1`, fmt.Sprintf("SV%03d", i)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		e.students = append(e.students, id)
	}
	e.board = board.NewService(board.NewRepository(pool), board.NewAnnouncements())
	e.hub = realtime.NewHub(realtime.Sources{Submissions: submission.NewRepository(pool), Board: e.board}, quiet)
	e.board.SetBroadcaster(e.hub)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go realtime.NewListener(pool.Config().ConnString(), e.hub, quiet).Run(ctx)
	go e.hub.RunSummaries(ctx)
	srv := httptest.NewServer(realtime.NewHandler(e.hub, verifier{e.signer}, nil, quiet))
	t.Cleanup(srv.Close)
	e.url = "ws" + strings.TrimPrefix(srv.URL, "http")
	waitListening(t, pool)
	return e
}

// waitListening waits until the listener's LISTEN is active.
func waitListening(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if testdb.Count(t, pool, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND query LIKE 'LISTEN%'`) > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("listener did not start")
}

func (e *env) token(t *testing.T, id int64, role auth.Role) string {
	t.Helper()
	tok, _, err := e.signer.Issue(auth.Principal{ID: id, Role: role}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

type msg struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type client struct {
	t    *testing.T
	conn *websocket.Conn
	in   chan msg
	err  chan error
}

func dial(t *testing.T, url string, header http.Header) (*client, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		return nil, resp, err
	}
	c := &client{t: t, conn: conn, in: make(chan msg, 512), err: make(chan error, 1)}
	go func() {
		for {
			_, b, err := conn.Read(context.Background())
			if err != nil {
				c.err <- err
				return
			}
			var m msg
			_ = json.Unmarshal(b, &m)
			c.in <- m
		}
	}()
	t.Cleanup(func() { conn.CloseNow() })
	return c, resp, nil
}

func (c *client) send(typ string, data any) {
	c.t.Helper()
	b, _ := json.Marshal(map[string]any{"type": typ, "data": data})
	if err := c.conn.Write(context.Background(), websocket.MessageText, b); err != nil {
		c.t.Fatal(err)
	}
}

// next returns the next message of type typ (skipping others) or fails.
func (c *client) next(typ string, within time.Duration) msg {
	c.t.Helper()
	deadline := time.After(within)
	for {
		select {
		case m := <-c.in:
			if m.Type == typ {
				return m
			}
		case err := <-c.err:
			c.t.Fatalf("connection closed waiting for %s: %v", typ, err)
		case <-deadline:
			c.t.Fatalf("no %s within %s", typ, within)
		}
	}
}

// none asserts no message of type typ arrives within d.
func (c *client) none(typ string, d time.Duration) {
	c.t.Helper()
	deadline := time.After(d)
	for {
		select {
		case m := <-c.in:
			if m.Type == typ {
				c.t.Fatalf("unexpected %s: %s", typ, m.Data)
			}
		case <-deadline:
			return
		}
	}
}

func (c *client) closedWith(within time.Duration) websocket.StatusCode {
	c.t.Helper()
	select {
	case err := <-c.err:
		return websocket.CloseStatus(err)
	case <-time.After(within):
		c.t.Fatalf("connection not closed within %s", within)
		return 0
	}
}

func (e *env) connect(t *testing.T, id int64, role auth.Role, session int64) *client {
	t.Helper()
	c, _, err := dial(t, e.url, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.send("auth", map[string]string{"token": e.token(t, id, role)})
	c.next("auth.ok", 2*time.Second)
	if session != 0 {
		c.send("subscribe", map[string]int64{"session_id": session})
		c.next("subscribed", 2*time.Second)
	}
	return c
}

func TestAuthenticationAndOrigin(t *testing.T) {
	e := setup(t, 1)
	old := realtime.AuthTimeout
	realtime.AuthTimeout = 300 * time.Millisecond
	t.Cleanup(func() { realtime.AuthTimeout = old })

	silent, _, _ := dial(t, e.url, nil)
	if code := silent.closedWith(2 * time.Second); code != 4001 {
		t.Errorf("no auth: close %d, want 4001", code)
	}
	bad, _, _ := dial(t, e.url, nil)
	bad.send("auth", map[string]string{"token": "not-a-jwt"})
	if code := bad.closedWith(2 * time.Second); code != 4001 {
		t.Errorf("bad token: close %d, want 4001", code)
	}
	wrong, _, _ := dial(t, e.url, nil)
	wrong.send("subscribe", map[string]int64{"session_id": e.fx.SessionID})
	if code := wrong.closedWith(2 * time.Second); code != 4001 {
		t.Errorf("subscribe before auth: close %d, want 4001", code)
	}

	_, resp, err := dial(t, e.url, http.Header{"Origin": {"http://evil.example"}})
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("foreign Origin: err=%v status=%v, want 403", err, resp)
	}

	ok := e.connect(t, e.students[0], auth.RoleStudent, e.fx.SessionID)
	ok.send("bogus", nil)
	ok.next("error", time.Second)
}

func TestTokenExpiryAndRefresh(t *testing.T) {
	e := setup(t, 1)
	short := auth.NewJWTSigner(secret, 1500*time.Millisecond)
	issue := func(s *auth.JWTSigner) string {
		tok, _, _ := s.Issue(auth.Principal{ID: e.students[0], Role: auth.RoleStudent}, time.Now())
		return tok
	}
	c, _, _ := dial(t, e.url, nil)
	c.send("auth", map[string]string{"token": issue(short)})
	c.next("auth.ok", time.Second)
	if code := c.closedWith(4 * time.Second); code != 4001 {
		t.Fatalf("expired token: close %d, want 4001", code)
	}

	// A fresh token sent before expiry keeps the connection open.
	c2, _, _ := dial(t, e.url, nil)
	c2.send("auth", map[string]string{"token": issue(short)})
	c2.next("auth.ok", time.Second)
	time.Sleep(700 * time.Millisecond)
	c2.send("auth", map[string]string{"token": issue(e.signer)})
	c2.next("auth.ok", time.Second)
	select {
	case err := <-c2.err:
		t.Fatalf("closed despite refresh: %v", err)
	case <-time.After(2 * time.Second):
	}
	// Someone else's token cannot take over the connection.
	c2.send("auth", map[string]string{"token": e.token(t, e.fx.TeacherID, auth.RoleTeacher)})
	c2.next("error", time.Second)
}

func claimAndComplete(t *testing.T, q judge.Repository, verdictAC bool) judge.Job {
	t.Helper()
	job, err := q.Claim(context.Background(), "w", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	o := judge.Outcome{Verdict: judge.VerdictAC, PassedTests: len(job.Tests), TotalTests: len(job.Tests)}
	for _, tc := range job.Tests {
		o.Tests = append(o.Tests, judge.TestOutcome{TestCaseID: tc.ID, Verdict: judge.VerdictAC})
	}
	if !verdictAC {
		o = judge.Outcome{Verdict: judge.VerdictWA, TotalTests: len(job.Tests),
			Tests: []judge.TestOutcome{{TestCaseID: job.Tests[0].ID, Verdict: judge.VerdictWA}}}
	}
	if err := q.Complete(context.Background(), job, o); err != nil {
		t.Fatal(err)
	}
	return job
}

type runData struct {
	ID             int64 `json:"id"`
	StudentID      int64 `json:"student_id"`
	ProblemVersion int   `json:"problem_version"`
	Result         *struct {
		Status         string  `json:"status"`
		Verdict        *string `json:"verdict"`
		PassedTests    int     `json:"passed_tests"`
		TotalTests     int     `json:"total_tests"`
		ProblemVersion int     `json:"problem_version"`
	} `json:"result"`
}

func run(t *testing.T, m msg) runData {
	t.Helper()
	var r runData
	if err := json.Unmarshal(m.Data, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestResultsBoardAndPrivacy(t *testing.T) {
	e := setup(t, 2)
	a := e.connect(t, e.students[0], auth.RoleStudent, e.fx.SessionID)
	b := e.connect(t, e.students[1], auth.RoleStudent, e.fx.SessionID)
	teacher := e.connect(t, e.fx.TeacherID, auth.RoleTeacher, e.fx.SessionID)

	// The teacher board shows both students online.
	var sum board.Summary
	if err := json.Unmarshal(teacher.next("summary.updated", 2*time.Second).Data, &sum); err != nil {
		t.Fatal(err)
	}
	if sum.OnlineCount != 2 || len(sum.Rows) != 2 || sum.Queue.Queued != 2 {
		t.Fatalf("initial board: online=%d rows=%d queue=%+v", sum.OnlineCount, len(sum.Rows), sum.Queue)
	}

	q := judge.NewRepository(e.pool)
	// A's submission (lowest id): claimed -> A sees "judging" ...
	job, err := q.Claim(context.Background(), "w", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	got := run(t, a.next("run.updated", 2*time.Second))
	if got.Result == nil || got.Result.Status != "judging" {
		t.Fatalf("A first update = %+v, want judging", got.Result)
	}
	// ... then the result.
	if err := q.Complete(context.Background(), job, judge.Outcome{Verdict: judge.VerdictAC, PassedTests: 2, TotalTests: 2,
		Tests: []judge.TestOutcome{{TestCaseID: job.Tests[0].ID, Verdict: "AC"}, {TestCaseID: job.Tests[1].ID, Verdict: "AC"}}}); err != nil {
		t.Fatal(err)
	}
	got = run(t, a.next("run.updated", 2*time.Second))
	if got.Result.Status != "completed" || *got.Result.Verdict != "AC" || got.Result.PassedTests != 2 || got.Result.TotalTests != 2 {
		t.Fatalf("A result = %+v, want 2/2 AC", got.Result)
	}
	b.none("run.updated", 500*time.Millisecond) // B never sees A's result

	// Teacher: both updates + a board with A at 1/1 AC.
	teacher.next("run.updated", time.Second)
	deadline := time.Now().Add(3 * time.Second)
	for {
		m := teacher.next("summary.updated", 3*time.Second)
		_ = json.Unmarshal(m.Data, &sum)
		if sum.Rows[0].ACCount == 1 || time.Now().After(deadline) {
			break
		}
	}
	if sum.Rows[0].ACCount != 1 || sum.Rows[0].Cells[0].State != board.CellAC || sum.Rows[1].Cells[0].State != board.CellPending {
		t.Fatalf("board after AC: %+v", sum.Rows)
	}

	// Another teacher cannot watch this session.
	var other int64
	_ = e.pool.QueryRow(context.Background(), `INSERT INTO users (code, fullname, role, password_hash) VALUES ('GV2','T2','teacher','x') RETURNING id`).Scan(&other)
	t2 := e.connect(t, other, auth.RoleTeacher, 0)
	t2.send("subscribe", map[string]int64{"session_id": e.fx.SessionID})
	t2.next("error", time.Second)
}

func TestAnnouncementAndRejudgeFanOut(t *testing.T) {
	e := setup(t, 3)
	var clients []*client
	for _, s := range e.students {
		clients = append(clients, e.connect(t, s, auth.RoleStudent, e.fx.SessionID))
	}
	q := judge.NewRepository(e.pool)
	for range e.students {
		claimAndComplete(t, q, true)
	}
	for _, c := range clients {
		for {
			if r := run(t, c.next("run.updated", 2*time.Second)); r.Result.Status == "completed" {
				break
			}
		}
	}

	if _, err := e.board.Announce(context.Background(), e.fx.TeacherID, e.fx.SessionID, "Còn 10 phút"); err != nil {
		t.Fatal(err)
	}
	for i, c := range clients {
		var a board.Announcement
		_ = json.Unmarshal(c.next("announcement", 2*time.Second).Data, &a)
		if a.Text != "Còn 10 phút" {
			t.Errorf("student %d announcement = %+v", i, a)
		}
	}

	// The teacher raises the time limit: every student sees "queued" at v2.
	if _, n, _, err := problem.NewRepository(e.pool).Update(context.Background(), e.fx.ProblemID,
		problem.Input{Code: "A", Title: "P", TimeLimitMs: 2000, MemoryLimitMb: 256}); err != nil || n != 3 {
		t.Fatalf("Update: n=%d err=%v", n, err)
	}
	for i, c := range clients {
		r := run(t, c.next("run.updated", 2*time.Second))
		if r.ProblemVersion != 2 || r.Result == nil || r.Result.Status != "queued" || r.Result.ProblemVersion != 2 {
			t.Errorf("student %d after rejudge: %+v", i, r)
		}
	}
	// ... then the new result for whichever submission is judged next.
	job := claimAndComplete(t, q, false)
	for i, c := range clients {
		if e.students[i] != studentOf(t, e, job.SubmissionID) {
			continue
		}
		r := run(t, c.next("run.updated", 2*time.Second))
		for r.Result.Status != "completed" {
			r = run(t, c.next("run.updated", 2*time.Second))
		}
		if *r.Result.Verdict != "WA" || r.Result.ProblemVersion != 2 {
			t.Errorf("new result = %+v, want WA at v2", r.Result)
		}
	}
}

func studentOf(t *testing.T, e *env, submissionID int64) int64 {
	t.Helper()
	var id int64
	if err := e.pool.QueryRow(context.Background(), `SELECT p.student_id FROM submissions s
		JOIN exam_participants p ON p.id = s.exam_participant_id WHERE s.id = $1`, submissionID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestListenerReconnectSendsResync(t *testing.T) {
	e := setup(t, 1)
	c := e.connect(t, e.students[0], auth.RoleStudent, e.fx.SessionID)
	// Kill the listener's database connection (as if PostgreSQL restarted).
	if _, err := e.pool.Exec(context.Background(), `SELECT pg_terminate_backend(pid) FROM pg_stat_activity
		WHERE datname = current_database() AND query LIKE 'LISTEN%'`); err != nil {
		t.Fatal(err)
	}
	c.next("resync", 5*time.Second)
	waitListening(t, e.pool)
	// Events flow again after the reconnect.
	claimAndComplete(t, judge.NewRepository(e.pool), true)
	c.next("run.updated", 2*time.Second)
}

func TestFiftyClientsFanOut(t *testing.T) {
	const n = 50
	e := setup(t, n)
	clients := make([]*client, n)
	for i, s := range e.students {
		clients[i] = e.connect(t, s, auth.RoleStudent, e.fx.SessionID)
	}
	if got := e.hub.ClientCount(); got != n {
		t.Fatalf("hub clients = %d", got)
	}

	// Broadcast latency: one announcement to 50 connections.
	start := time.Now()
	if _, err := e.board.Announce(context.Background(), e.fx.TeacherID, e.fx.SessionID, "ping"); err != nil {
		t.Fatal(err)
	}
	lat := make([]time.Duration, n)
	var wg sync.WaitGroup
	for i, c := range clients {
		wg.Add(1)
		go func(i int, c *client) {
			defer wg.Done()
			c.next("announcement", 5*time.Second)
			lat[i] = time.Since(start)
		}(i, c)
	}
	wg.Wait()
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	t.Logf("announcement to %d clients: p50=%s p95=%s max=%s", n, lat[n/2], lat[n*95/100], lat[n-1])

	// Result latency: commit -> NOTIFY -> LISTEN -> load -> push, per student.
	q := judge.NewRepository(e.pool)
	received := make(chan time.Duration, n)
	commitAt := sync.Map{}
	for i, c := range clients {
		wg.Add(1)
		go func(i int, c *client) {
			defer wg.Done()
			for {
				r := run(t, c.next("run.updated", 20*time.Second))
				if r.Result != nil && r.Result.Status == "completed" {
					v, _ := commitAt.Load(r.ID)
					received <- time.Since(v.(time.Time))
					return
				}
			}
		}(i, c)
	}
	for i := 0; i < n; i++ {
		job, err := q.Claim(context.Background(), "w", 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		commitAt.Store(job.SubmissionID, time.Now())
		o := judge.Outcome{Verdict: judge.VerdictAC, PassedTests: 2, TotalTests: 2,
			Tests: []judge.TestOutcome{{TestCaseID: job.Tests[0].ID, Verdict: "AC"}, {TestCaseID: job.Tests[1].ID, Verdict: "AC"}}}
		if err := q.Complete(context.Background(), job, o); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	close(received)
	var res []time.Duration
	for d := range received {
		res = append(res, d)
	}
	if len(res) != n {
		t.Fatalf("results delivered = %d, want %d", len(res), n)
	}
	sort.Slice(res, func(i, j int) bool { return res[i] < res[j] })
	t.Logf("result push to %d students: p50=%s p95=%s max=%s", n, res[n/2], res[n*95/100], res[n-1])
}
