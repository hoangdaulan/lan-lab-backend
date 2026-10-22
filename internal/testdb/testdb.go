// Package testdb gives integration tests a fresh PostgreSQL database built
// from the embedded migrations, plus a small exam fixture. Tests skip when
// TEST_DATABASE_URL is not set (see `make test-integration`).
package testdb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver for goose
	"github.com/pressly/goose/v3"

	"github.com/hoangdaulan/lan-lab-backend/migrations"
)

// New creates a database, migrates it and drops it when the test ends.
func New(t testing.TB) *pgxpool.Pool {
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

	u, err := url.Parse(admin)
	if err != nil {
		t.Fatal(err)
	}
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

// Fixture is one exam with one problem (version 1) and a running session.
type Fixture struct {
	TeacherID   int64
	ExamID      int64
	ProblemID   int64
	SessionID   int64
	Submissions []int64
	Runs        []int64
}

// NewFixture creates the exam, `tests` test cases and `n` submissions by
// different students, each with a queued run + job at version 1.
func NewFixture(t testing.TB, pool *pgxpool.Pool, tests, n int) Fixture {
	t.Helper()
	ctx := context.Background()
	var f Fixture
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO users (code, fullname, role, password_hash) VALUES ('GV','T','teacher','x') RETURNING id`).Scan(&f.TeacherID))
	must(pool.QueryRow(ctx, `INSERT INTO exams (teacher_id, title) VALUES ($1,'E') RETURNING id`, f.TeacherID).Scan(&f.ExamID))
	must(pool.QueryRow(ctx, `INSERT INTO problems (exam_id, code, title, time_limit_ms, memory_limit_mb) VALUES ($1,'A','P',1000,256) RETURNING id`, f.ExamID).Scan(&f.ProblemID))
	for i := 1; i <= tests; i++ {
		_, err := pool.Exec(ctx, `INSERT INTO test_cases (problem_id, input, output) VALUES ($1, $2, $3)`,
			f.ProblemID, fmt.Sprintf("problems/x/tests/%d.in", i), fmt.Sprintf("problems/x/tests/%d.out", i))
		must(err)
	}
	f.SessionID = AddSession(t, pool, f.ExamID)
	for i := 0; i < n; i++ {
		sub, run := AddSubmission(t, pool, f.SessionID, f.ProblemID, fmt.Sprintf("SV%03d", i))
		f.Submissions = append(f.Submissions, sub)
		f.Runs = append(f.Runs, run)
	}
	return f
}

// AddSession adds a session running from an hour ago to an hour from now.
func AddSession(t testing.TB, pool *pgxpool.Pool, examID int64) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `INSERT INTO exam_sessions (exam_id, name, starts_at, ends_at)
		VALUES ($1,'S', now() - interval '1 hour', now() + interval '1 hour') RETURNING id`, examID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// AddSubmission adds a student (code), joins the session and submits, with
// a queued run + job at the problem's current version.
func AddSubmission(t testing.TB, pool *pgxpool.Pool, sessionID, problemID int64, code string) (submissionID, runID int64) {
	t.Helper()
	ctx := context.Background()
	var studentID, partID int64
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO users (code, fullname, role, password_hash) VALUES ($1,'S','student','x') RETURNING id`, code).Scan(&studentID))
	must(pool.QueryRow(ctx, `INSERT INTO exam_participants (exam_session_id, student_id) VALUES ($1,$2) RETURNING id`, sessionID, studentID).Scan(&partID))
	must(pool.QueryRow(ctx, `INSERT INTO submissions (exam_participant_id, problem_id, source_path, received_at)
		VALUES ($1,$2,'submissions/x.cpp', now()) RETURNING id`, partID, problemID).Scan(&submissionID))
	must(pool.QueryRow(ctx, `INSERT INTO judge_runs (submission_id, problem_version)
		SELECT $1, version FROM problems WHERE id = $2 RETURNING id`, submissionID, problemID).Scan(&runID))
	_, err := pool.Exec(ctx, `INSERT INTO judge_jobs (judge_run_id) VALUES ($1)`, runID)
	must(err)
	return submissionID, runID
}

// Count runs a SELECT count(*)-style query.
func Count(t testing.TB, pool *pgxpool.Pool, q string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
