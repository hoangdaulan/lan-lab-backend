package problem

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

const problemSelect = `SELECT p.id, p.exam_id, e.teacher_id, p.code, p.title, p.statement,
	p.time_limit_ms, p.memory_limit_mb, p.version, p.created_at, p.updated_at
	FROM problems p JOIN exams e ON e.id = p.exam_id`

func scanProblem(row pgx.Row) (Problem, error) {
	var p Problem
	err := row.Scan(&p.ID, &p.ExamID, &p.TeacherID, &p.Code, &p.Title, &p.Statement,
		&p.TimeLimitMs, &p.MemoryLimitMb, &p.Version, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Problem{}, apperr.NotFound("problem")
	}
	return p, err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (r *pgRepository) ExamOwner(ctx context.Context, examID int64) (int64, error) {
	var owner int64
	err := r.pool.QueryRow(ctx, `SELECT teacher_id FROM exams WHERE id = $1`, examID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, apperr.NotFound("exam")
	}
	return owner, err
}

func (r *pgRepository) Create(ctx context.Context, examID int64, in Input) (Problem, error) {
	var id int64
	err := r.pool.QueryRow(ctx,
		`INSERT INTO problems (exam_id, code, title, statement, time_limit_ms, memory_limit_mb)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		examID, in.Code, in.Title, in.Statement, in.TimeLimitMs, in.MemoryLimitMb).Scan(&id)
	if isUniqueViolation(err) {
		return Problem{}, apperr.Conflict("problem code %s already exists in this exam", in.Code)
	}
	if err != nil {
		return Problem{}, err
	}
	return r.Get(ctx, id)
}

func (r *pgRepository) ListByExam(ctx context.Context, examID int64) ([]Problem, error) {
	rows, err := r.pool.Query(ctx, problemSelect+` WHERE p.exam_id = $1 ORDER BY p.code, p.id`, examID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Problem{}
	for rows.Next() {
		p, err := scanProblem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *pgRepository) Get(ctx context.Context, id int64) (Problem, error) {
	return scanProblem(r.pool.QueryRow(ctx, problemSelect+` WHERE p.id = $1`, id))
}

func lockProblem(ctx context.Context, tx pgx.Tx, id int64) (Problem, error) {
	return scanProblem(tx.QueryRow(ctx, problemSelect+` WHERE p.id = $1 FOR UPDATE OF p`, id))
}

func (r *pgRepository) Update(ctx context.Context, id int64, in Input) (Problem, bool, error) {
	bumped := false
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		old, err := lockProblem(ctx, tx, id)
		if err != nil {
			return err
		}
		bumped = LimitsChanged(old, in)
		_, err = tx.Exec(ctx,
			`UPDATE problems SET code = $2, title = $3, statement = $4, time_limit_ms = $5,
			   memory_limit_mb = $6, version = version + $7, updated_at = CURRENT_TIMESTAMP
			 WHERE id = $1`,
			id, in.Code, in.Title, in.Statement, in.TimeLimitMs, in.MemoryLimitMb, boolToInt(bumped))
		if isUniqueViolation(err) {
			return apperr.Conflict("problem code %s already exists in this exam", in.Code)
		}
		if err != nil {
			return err
		}
		if bumped {
			return requeueProblem(ctx, tx, id, old.Version+1)
		}
		return nil
	})
	if err != nil {
		return Problem{}, false, err
	}
	p, err := r.Get(ctx, id)
	return p, bumped, err
}

func (r *pgRepository) Delete(ctx context.Context, id int64) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := lockProblem(ctx, tx, id); err != nil {
			return err
		}
		var hasSubmissions bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM submissions WHERE problem_id = $1)`, id).Scan(&hasSubmissions); err != nil {
			return err
		}
		if hasSubmissions {
			return apperr.Conflict("problem has submissions and cannot be deleted")
		}
		if _, err := tx.Exec(ctx, `DELETE FROM test_cases WHERE problem_id = $1`, id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM problems WHERE id = $1`, id)
		return err
	})
}

func (r *pgRepository) ListTests(ctx context.Context, problemID int64) ([]TestCase, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, input, output FROM test_cases WHERE problem_id = $1 ORDER BY id`, problemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TestCase{}
	for rows.Next() {
		var t TestCase
		if err := rows.Scan(&t.ID, &t.InputPath, &t.OutputPath); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *pgRepository) ReplaceTests(ctx context.Context, problemID int64, tests []TestFile) (Problem, error) {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		old, err := lockProblem(ctx, tx, problemID)
		if err != nil {
			return err
		}
		// CASCADE also removes per-test results of the old set (§8).
		if _, err := tx.Exec(ctx, `DELETE FROM test_cases WHERE problem_id = $1`, problemID); err != nil {
			return err
		}
		// Inserted in scan order, so ORDER BY id is the test order.
		for _, t := range tests {
			if _, err := tx.Exec(ctx,
				`INSERT INTO test_cases (problem_id, input, output) VALUES ($1, $2, $3)`,
				problemID, t.InputPath, t.OutputPath); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx,
			`UPDATE problems SET version = version + 1, updated_at = CURRENT_TIMESTAMP WHERE id = $1`,
			problemID); err != nil {
			return err
		}
		return requeueProblem(ctx, tx, problemID, old.Version+1)
	})
	if err != nil {
		return Problem{}, err
	}
	return r.Get(ctx, problemID)
}

// requeueProblem must mark the problem's current runs/jobs obsolete and
// create a queued run + job at newVersion for every submission of the
// problem, inside the caller's transaction (BACKEND_CONTEXT.md §8, §10).
//
// Stub until the submission/judge step: there are no submissions yet, and
// the queue SQL lands together with the worker that consumes it.
func requeueProblem(_ context.Context, _ pgx.Tx, _ int64, _ int) error {
	return nil
}

func (r *pgRepository) SessionWindow(ctx context.Context, sessionID int64) (SessionWindow, error) {
	var w SessionWindow
	err := r.pool.QueryRow(ctx,
		`SELECT exam_id, starts_at, ends_at FROM exam_sessions WHERE id = $1`, sessionID).
		Scan(&w.ExamID, &w.StartsAt, &w.EndsAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionWindow{}, apperr.NotFound("session")
	}
	return w, err
}

func (r *pgRepository) IsParticipant(ctx context.Context, sessionID, studentID int64) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM exam_participants WHERE exam_session_id = $1 AND student_id = $2)`,
		sessionID, studentID).Scan(&ok)
	return ok, err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
