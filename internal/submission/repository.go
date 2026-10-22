package submission

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hoangdaulan/lan-lab-backend/internal/apperr"
)

type pgRepository struct {
	pool *pgxpool.Pool
}

var _ Repository = (*pgRepository)(nil)

func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgRepository{pool: pool}
}

func (r *pgRepository) Create(ctx context.Context, n NewSubmission) (Submission, error) {
	var id int64
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// Lock order problem -> exam_session -> exam_participant
		// (BACKEND_CONTEXT.md §10). SHARE on problem keeps its version and
		// tests fixed while the run is created; a rejudge (FOR UPDATE on the
		// problem) waits for us or we wait for it.
		var examID int64
		var version int
		err := tx.QueryRow(ctx,
			`SELECT exam_id, version FROM problems WHERE id = $1 FOR SHARE`, n.ProblemID).Scan(&examID, &version)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound("problem")
		}
		if err != nil {
			return err
		}

		var sessionExamID int64
		var startsAt, endsAt time.Time
		err = tx.QueryRow(ctx,
			`SELECT exam_id, starts_at, ends_at FROM exam_sessions WHERE id = $1 FOR SHARE`, n.SessionID).
			Scan(&sessionExamID, &startsAt, &endsAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound("session")
		}
		if err != nil {
			return err
		}
		if sessionExamID != examID {
			return apperr.NotFound("problem")
		}

		// The participant row lock orders this submission with "finish":
		// whichever commits first wins (§10).
		var participantID int64
		var status string
		err = tx.QueryRow(ctx,
			`SELECT id, status FROM exam_participants
			 WHERE exam_session_id = $1 AND student_id = $2 FOR UPDATE`,
			n.SessionID, n.StudentID).Scan(&participantID, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Conflict("join the session before submitting")
		}
		if err != nil {
			return err
		}
		if status != "in_progress" {
			return apperr.Conflict("you have finished this session")
		}
		if n.ReceivedAt.Before(startsAt) {
			return apperr.Conflict("the session has not started")
		}
		if !n.ReceivedAt.Before(endsAt) {
			return apperr.Conflict("the session has ended")
		}

		var hasTests bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM test_cases WHERE problem_id = $1)`, n.ProblemID).Scan(&hasTests); err != nil {
			return err
		}
		if !hasTests {
			return apperr.Conflict("the problem has no tests yet")
		}

		if err := tx.QueryRow(ctx,
			`INSERT INTO submissions (exam_participant_id, problem_id, source_path, received_at)
			 VALUES ($1, $2, $3, $4) RETURNING id`,
			participantID, n.ProblemID, n.SourcePath, n.ReceivedAt).Scan(&id); err != nil {
			return err
		}
		var runID int64
		if err := tx.QueryRow(ctx,
			`INSERT INTO judge_runs (submission_id, problem_version) VALUES ($1, $2) RETURNING id`,
			id, version).Scan(&runID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO judge_jobs (judge_run_id) VALUES ($1)`, runID)
		return err
	})
	if err != nil {
		return Submission{}, err
	}
	return r.Get(ctx, id)
}

// submissionSelect joins the current run: same version as the problem now
// and not obsolete (BACKEND_CONTEXT.md §8).
const submissionSelect = `SELECT s.id, p.exam_session_id, e.id, e.teacher_id, s.problem_id, pr.code, pr.version,
	u.id, u.code, u.fullname, s.source_path, s.received_at,
	r.id, r.problem_version, r.status, r.verdict, r.passed_tests, r.total_tests,
	r.compiler_info, r.compile_output, r.system_error, r.created_at, r.started_at, r.finished_at
	FROM submissions s
	JOIN exam_participants p ON p.id = s.exam_participant_id
	JOIN users u ON u.id = p.student_id
	JOIN problems pr ON pr.id = s.problem_id
	JOIN exams e ON e.id = pr.exam_id
	LEFT JOIN judge_runs r ON r.submission_id = s.id AND r.problem_version = pr.version AND r.status <> 'obsolete'`

func scanSubmission(row pgx.Row) (Submission, error) {
	var s Submission
	var (
		runID, passed, total *int64
		version              *int
		status               *string
		createdAt            *time.Time
		res                  Result
	)
	err := row.Scan(&s.ID, &s.SessionID, &s.ExamID, &s.TeacherID, &s.ProblemID, &s.ProblemCode, &s.ProblemVersion,
		&s.StudentID, &s.StudentCode, &s.StudentFullname, &s.SourcePath, &s.ReceivedAt,
		&runID, &version, &status, &res.Verdict, &passed, &total,
		&res.CompilerInfo, &res.CompileOutput, &res.SystemError, &createdAt, &res.StartedAt, &res.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Submission{}, apperr.NotFound("submission")
	}
	if err != nil {
		return Submission{}, err
	}
	if runID != nil {
		res.RunID, res.ProblemVersion, res.Status = *runID, *version, *status
		res.PassedTests, res.TotalTests, res.CreatedAt = int(*passed), int(*total), *createdAt
		s.Result = &res
	}
	return s, nil
}

func (r *pgRepository) Get(ctx context.Context, id int64) (Submission, error) {
	return scanSubmission(r.pool.QueryRow(ctx, submissionSelect+` WHERE s.id = $1`, id))
}

func (r *pgRepository) List(ctx context.Context, sessionID int64, f Filter) ([]Submission, error) {
	rows, err := r.pool.Query(ctx, submissionSelect+`
		WHERE p.exam_session_id = $1 AND ($2 = 0 OR p.student_id = $2) AND ($3 = 0 OR s.problem_id = $3)
		ORDER BY s.received_at DESC, s.id DESC`, sessionID, f.StudentID, f.ProblemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Submission{}
	for rows.Next() {
		s, err := scanSubmission(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *pgRepository) ListTestResults(ctx context.Context, runID int64) ([]TestResult, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT test_case_id, verdict, execution_time_ms, memory_used_kb
		 FROM submission_test_results WHERE judge_run_id = $1 ORDER BY test_case_id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TestResult{}
	for rows.Next() {
		var t TestResult
		if err := rows.Scan(&t.TestCaseID, &t.Verdict, &t.ExecutionTimeMs, &t.MemoryUsedKB); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *pgRepository) SessionOwner(ctx context.Context, sessionID int64) (int64, error) {
	var owner int64
	err := r.pool.QueryRow(ctx,
		`SELECT e.teacher_id FROM exam_sessions s JOIN exams e ON e.id = s.exam_id WHERE s.id = $1`,
		sessionID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, apperr.NotFound("session")
	}
	return owner, err
}
