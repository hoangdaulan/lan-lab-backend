package exam

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

// collect scans every row with scan, mapping no rows to an empty slice.
func collect[T any](rows pgx.Rows, err error, scan func(pgx.Row) (T, error)) ([]T, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ---- exams ----

const examColumns = `id, teacher_id, title, description, created_at, updated_at`

func scanExam(row pgx.Row) (Exam, error) {
	var e Exam
	err := row.Scan(&e.ID, &e.TeacherID, &e.Title, &e.Description, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Exam{}, apperr.NotFound("exam")
	}
	return e, err
}

func (r *pgRepository) CreateExam(ctx context.Context, teacherID int64, title, description string) (Exam, error) {
	return scanExam(r.pool.QueryRow(ctx,
		`INSERT INTO exams (teacher_id, title, description) VALUES ($1, $2, $3) RETURNING `+examColumns,
		teacherID, title, description))
}

func (r *pgRepository) ListExams(ctx context.Context, teacherID int64) ([]Exam, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+examColumns+` FROM exams WHERE teacher_id = $1 ORDER BY id DESC`, teacherID)
	return collect(rows, err, scanExam)
}

func (r *pgRepository) GetExam(ctx context.Context, id int64) (Exam, error) {
	return scanExam(r.pool.QueryRow(ctx, `SELECT `+examColumns+` FROM exams WHERE id = $1`, id))
}

func (r *pgRepository) UpdateExam(ctx context.Context, id int64, title, description string) (Exam, error) {
	return scanExam(r.pool.QueryRow(ctx,
		`UPDATE exams SET title = $2, description = $3, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1 RETURNING `+examColumns, id, title, description))
}

func (r *pgRepository) DeleteExam(ctx context.Context, id int64) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := scanExam(tx.QueryRow(ctx,
			`SELECT `+examColumns+` FROM exams WHERE id = $1 FOR UPDATE`, id)); err != nil {
			return err
		}
		var hasChildren bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM problems WHERE exam_id = $1)
			     OR EXISTS (SELECT 1 FROM exam_sessions WHERE exam_id = $1)`, id).Scan(&hasChildren); err != nil {
			return err
		}
		if hasChildren {
			return apperr.Conflict("delete the exam's problems and sessions first")
		}
		_, err := tx.Exec(ctx, `DELETE FROM exams WHERE id = $1`, id)
		return err
	})
}

// ---- sessions ----

const sessionSelect = `SELECT s.id, s.exam_id, e.title, e.teacher_id, s.name, s.starts_at, s.ends_at,
	s.created_at, s.updated_at FROM exam_sessions s JOIN exams e ON e.id = s.exam_id`

func scanSession(row pgx.Row) (Session, error) {
	var s Session
	err := row.Scan(&s.ID, &s.ExamID, &s.ExamTitle, &s.TeacherID, &s.Name, &s.StartsAt, &s.EndsAt,
		&s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, apperr.NotFound("session")
	}
	return s, err
}

func (r *pgRepository) CreateSession(ctx context.Context, examID int64, name string, startsAt, endsAt time.Time) (Session, error) {
	var id int64
	if err := r.pool.QueryRow(ctx,
		`INSERT INTO exam_sessions (exam_id, name, starts_at, ends_at) VALUES ($1, $2, $3, $4) RETURNING id`,
		examID, name, startsAt, endsAt).Scan(&id); err != nil {
		return Session{}, err
	}
	return r.GetSession(ctx, id)
}

func (r *pgRepository) ListSessionsByExam(ctx context.Context, examID int64) ([]Session, error) {
	rows, err := r.pool.Query(ctx, sessionSelect+` WHERE s.exam_id = $1 ORDER BY s.starts_at DESC, s.id DESC`, examID)
	return collect(rows, err, scanSession)
}

func (r *pgRepository) ListAllSessions(ctx context.Context) ([]Session, error) {
	rows, err := r.pool.Query(ctx, sessionSelect+` ORDER BY s.starts_at DESC, s.id DESC`)
	return collect(rows, err, scanSession)
}

func (r *pgRepository) GetSession(ctx context.Context, id int64) (Session, error) {
	return scanSession(r.pool.QueryRow(ctx, sessionSelect+` WHERE s.id = $1`, id))
}

func (r *pgRepository) UpdateSession(ctx context.Context, id int64, name string, startsAt, endsAt time.Time) (Session, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE exam_sessions SET name = $2, starts_at = $3, ends_at = $4, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1`, id, name, startsAt, endsAt)
	if err != nil {
		return Session{}, err
	}
	if tag.RowsAffected() == 0 {
		return Session{}, apperr.NotFound("session")
	}
	return r.GetSession(ctx, id)
}

func (r *pgRepository) DeleteSession(ctx context.Context, id int64) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var locked int64
		err := tx.QueryRow(ctx, `SELECT id FROM exam_sessions WHERE id = $1 FOR UPDATE`, id).Scan(&locked)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound("session")
		}
		if err != nil {
			return err
		}
		var hasSubmissions bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM submissions sub
			   JOIN exam_participants p ON p.id = sub.exam_participant_id
			  WHERE p.exam_session_id = $1)`, id).Scan(&hasSubmissions); err != nil {
			return err
		}
		if hasSubmissions {
			return apperr.Conflict("session has submissions and cannot be deleted")
		}
		if _, err := tx.Exec(ctx, `DELETE FROM exam_participants WHERE exam_session_id = $1`, id); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM exam_sessions WHERE id = $1`, id)
		return err
	})
}

// ---- participants ----

const participantSelect = `SELECT p.id, p.exam_session_id, p.student_id, st.code, st.fullname,
	p.status, p.joined_at, p.finished_at FROM exam_participants p JOIN users st ON st.id = p.student_id`

func scanParticipant(row pgx.Row) (Participant, error) {
	var p Participant
	err := row.Scan(&p.ID, &p.SessionID, &p.StudentID, &p.StudentCode, &p.StudentFullname,
		&p.Status, &p.JoinedAt, &p.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Participant{}, apperr.NotFound("participant")
	}
	return p, err
}

func (r *pgRepository) ListParticipants(ctx context.Context, sessionID int64) ([]Participant, error) {
	rows, err := r.pool.Query(ctx, participantSelect+` WHERE p.exam_session_id = $1 ORDER BY st.code`, sessionID)
	return collect(rows, err, scanParticipant)
}

func (r *pgRepository) GetParticipant(ctx context.Context, sessionID, studentID int64) (Participant, error) {
	return scanParticipant(r.pool.QueryRow(ctx,
		participantSelect+` WHERE p.exam_session_id = $1 AND p.student_id = $2`, sessionID, studentID))
}

func (r *pgRepository) ListParticipationsByStudent(ctx context.Context, studentID int64) ([]Participant, error) {
	rows, err := r.pool.Query(ctx, participantSelect+` WHERE p.student_id = $1`, studentID)
	return collect(rows, err, scanParticipant)
}

func (r *pgRepository) CreateParticipant(ctx context.Context, sessionID, studentID int64, joinedAt time.Time) (Participant, error) {
	// UNIQUE (exam_session_id, student_id): a concurrent second join keeps
	// the first row.
	if _, err := r.pool.Exec(ctx,
		`INSERT INTO exam_participants (exam_session_id, student_id, joined_at) VALUES ($1, $2, $3)
		 ON CONFLICT (exam_session_id, student_id) DO NOTHING`,
		sessionID, studentID, joinedAt); err != nil {
		return Participant{}, err
	}
	return r.GetParticipant(ctx, sessionID, studentID)
}

func (r *pgRepository) FinishParticipant(ctx context.Context, sessionID, studentID int64, finishedAt time.Time) (Participant, error) {
	// A single UPDATE takes the participant row lock, which orders it with a
	// concurrent submission that locks the same row (BACKEND_CONTEXT.md §10).
	// GREATEST keeps the finished_at >= joined_at CHECK under clock skew.
	if _, err := r.pool.Exec(ctx,
		`UPDATE exam_participants SET status = 'finished', finished_at = GREATEST($3, joined_at)
		 WHERE exam_session_id = $1 AND student_id = $2 AND status = 'in_progress'`,
		sessionID, studentID, finishedAt); err != nil {
		return Participant{}, err
	}
	return r.GetParticipant(ctx, sessionID, studentID)
}

// ---- problems (read-only here) ----

func (r *pgRepository) ListProblems(ctx context.Context, examID int64) ([]ProblemSummary, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, code, title FROM problems WHERE exam_id = $1 ORDER BY code, id`, examID)
	return collect(rows, err, func(row pgx.Row) (ProblemSummary, error) {
		var p ProblemSummary
		return p, row.Scan(&p.ID, &p.Code, &p.Title)
	})
}
