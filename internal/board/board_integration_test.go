//go:build integration

package board_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hoangdaulan/lan-lab-backend/internal/apperr"
	"github.com/hoangdaulan/lan-lab-backend/internal/board"
	"github.com/hoangdaulan/lan-lab-backend/internal/judge"
	"github.com/hoangdaulan/lan-lab-backend/internal/testdb"
)

func complete(t *testing.T, q judge.Repository, verdict string) {
	t.Helper()
	job, err := q.Claim(context.Background(), "w", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	o := judge.Outcome{Verdict: judge.VerdictAC, PassedTests: len(job.Tests), TotalTests: len(job.Tests)}
	for _, tc := range job.Tests {
		o.Tests = append(o.Tests, judge.TestOutcome{TestCaseID: tc.ID, Verdict: "AC"})
	}
	if verdict == judge.VerdictWA {
		o = judge.Outcome{Verdict: judge.VerdictWA, TotalTests: len(job.Tests),
			Tests: []judge.TestOutcome{{TestCaseID: job.Tests[0].ID, Verdict: "WA"}}}
	}
	if err := q.Complete(context.Background(), job, o); err != nil {
		t.Fatal(err)
	}
}

func TestSummaryStates(t *testing.T) {
	pool := testdb.New(t)
	fx := testdb.NewFixture(t, pool, 2, 3) // SV000, SV001, SV002 on problem A
	ctx := context.Background()
	var probB int64
	if err := pool.QueryRow(ctx, `INSERT INTO problems (exam_id, code, title, time_limit_ms, memory_limit_mb)
		VALUES ($1,'B','Q',1000,256) RETURNING id`, fx.ExamID).Scan(&probB); err != nil {
		t.Fatal(err)
	}
	q := judge.NewRepository(pool)
	complete(t, q, judge.VerdictAC)                              // SV000: AC
	if _, err := q.Claim(ctx, "w", 10*time.Second); err != nil { // SV001: system error
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE judge_jobs SET status='failed', worker_id=NULL, lease_token=NULL, lease_expires_at=NULL
		WHERE judge_run_id = $1`, fx.Runs[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE judge_runs SET status='system_error', finished_at=now() WHERE id = $1`, fx.Runs[1]); err != nil {
		t.Fatal(err)
	}
	// SV000 submits again (newer) and gets WA: the AC must stay.
	var part0 int64
	_ = pool.QueryRow(ctx, `SELECT exam_participant_id FROM submissions WHERE id = $1`, fx.Submissions[0]).Scan(&part0)
	var sub, run int64
	_ = pool.QueryRow(ctx, `INSERT INTO submissions (exam_participant_id, problem_id, source_path, received_at)
		VALUES ($1,$2,'submissions/y.cpp', now() + interval '1 second') RETURNING id`, part0, fx.ProblemID).Scan(&sub)
	_ = pool.QueryRow(ctx, `INSERT INTO judge_runs (submission_id, problem_version) VALUES ($1, 1) RETURNING id`, sub).Scan(&run)
	_, _ = pool.Exec(ctx, `INSERT INTO judge_jobs (judge_run_id) VALUES ($1)`, run)
	// Remaining queued jobs: SV002 and SV000's second; judge SV002? Claim order is by id: SV002 first.
	if _, err := pool.Exec(ctx, `UPDATE judge_jobs SET available_at = now() + interval '1 hour' WHERE judge_run_id = $1`, fx.Runs[2]); err != nil {
		t.Fatal(err)
	}
	complete(t, q, judge.VerdictWA) // SV000's second submission: WA

	svc := board.NewService(board.NewRepository(pool), board.NewAnnouncements())
	sum, err := svc.TeacherSummary(ctx, fx.TeacherID, fx.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if sum.ProblemsTotal != 2 || len(sum.Rows) != 3 || sum.Problems[0].Code != "A" || sum.Problems[1].Code != "B" {
		t.Fatalf("summary shape: %+v", sum)
	}
	r0, r1, r2 := sum.Rows[0], sum.Rows[1], sum.Rows[2]
	if r0.ACCount != 1 || r0.Cells[0].State != board.CellAC || r0.Cells[0].Submissions != 2 ||
		r0.Cells[0].Latest == nil || *r0.Cells[0].Latest.Verdict != "WA" || r0.Cells[1].State != board.CellNone {
		t.Errorf("SV000 = %+v / %+v", r0, r0.Cells[0].Latest)
	}
	if r1.ACCount != 0 || r1.Cells[0].State != board.CellAttempted || *r1.Cells[0].Latest.Status != "system_error" {
		t.Errorf("SV001 = %+v", r1)
	}
	if r2.Cells[0].State != board.CellPending || *r2.Cells[0].Latest.Status != "queued" {
		t.Errorf("SV002 = %+v", r2)
	}
	if sum.Queue.Queued != 1 || sum.Queue.Running != 0 {
		t.Errorf("queue = %+v", sum.Queue)
	}
	// Independent count of AC cells.
	if n := testdb.Count(t, pool, `SELECT count(DISTINCT (s.exam_participant_id, s.problem_id)) FROM submissions s
		JOIN problems p ON p.id = s.problem_id JOIN judge_runs r ON r.submission_id = s.id
		WHERE r.problem_version = p.version AND r.status = 'completed' AND r.verdict = 'AC'`); n != r0.ACCount+r1.ACCount+r2.ACCount {
		t.Errorf("AC cells: board %d, SQL %d", r0.ACCount+r1.ACCount+r2.ACCount, n)
	}

	// Students: only their own row; not before joining; other teachers: 404.
	var sv1 int64
	_ = pool.QueryRow(ctx, `SELECT id FROM users WHERE code = 'SV001'`).Scan(&sv1)
	mine, err := svc.StudentSummary(ctx, sv1, fx.SessionID)
	if err != nil || len(mine.Rows) != 1 || mine.Rows[0].StudentID != sv1 {
		t.Fatalf("student summary = %+v, %v", mine.Rows, err)
	}
	var outsider int64
	_ = pool.QueryRow(ctx, `INSERT INTO users (code, fullname, role, password_hash) VALUES ('X','X','student','x') RETURNING id`).Scan(&outsider)
	if _, err := svc.StudentSummary(ctx, outsider, fx.SessionID); !errors.Is(err, apperr.ErrForbidden) {
		t.Errorf("not joined: %v", err)
	}
	if _, err := svc.TeacherSummary(ctx, outsider+1000, fx.SessionID); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("other teacher: %v", err)
	}
}

func TestAnnouncements(t *testing.T) {
	a := board.NewAnnouncements()
	if _, err := a.Add(1, "   ", time.Now()); !errors.Is(err, apperr.ErrInvalidInput) {
		t.Errorf("blank: %v", err)
	}
	for i := 0; i < 60; i++ {
		if _, err := a.Add(1, "x", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	list := a.List(1)
	if len(list) != 50 || list[0].ID != 60 || list[49].ID != 11 {
		t.Errorf("kept %d, newest %d, oldest %d", len(list), list[0].ID, list[49].ID)
	}
	if len(a.List(2)) != 0 {
		t.Error("other session has announcements")
	}
}

func TestQueueStatsMatchSQL(t *testing.T) {
	pool := testdb.New(t)
	testdb.NewFixture(t, pool, 2, 5)
	q := judge.NewRepository(pool)
	complete(t, q, judge.VerdictAC)
	complete(t, q, judge.VerdictWA)
	if _, err := q.Claim(context.Background(), "worker-a", time.Minute); err != nil {
		t.Fatal(err)
	}
	st, err := board.NewRepository(pool).QueueStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"queued", "running", "done", "failed", "obsolete"} {
		if n := testdb.Count(t, pool, `SELECT count(*) FROM judge_jobs WHERE status = $1`, s); st.Jobs[s] != n {
			t.Errorf("%s: stats %d, SQL %d", s, st.Jobs[s], n)
		}
	}
	if st.Jobs["queued"] != 2 || st.Jobs["running"] != 1 || st.Jobs["done"] != 2 {
		t.Errorf("jobs = %v", st.Jobs)
	}
	if len(st.ActiveWorkers) != 1 || st.ActiveWorkers[0] != "worker-a" {
		t.Errorf("active workers = %v", st.ActiveWorkers)
	}
	if st.RecentRuns != 3 || st.OldestQueuedMs <= 0 {
		t.Errorf("recent=%d oldest=%d", st.RecentRuns, st.OldestQueuedMs)
	}
}
