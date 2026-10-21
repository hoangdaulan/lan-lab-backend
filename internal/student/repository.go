package student

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgRepository struct {
	pool *pgxpool.Pool
}

var _ Repository = (*pgRepository)(nil)

func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgRepository{pool: pool}
}

const studentColumns = `id, code, fullname, created_at, updated_at`

func scanStudent(row pgx.Row) (Student, error) {
	var s Student
	err := row.Scan(&s.ID, &s.Code, &s.Fullname, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Student{}, ErrNotFound
	}
	return s, err
}

const insertStudent = `INSERT INTO students (code, fullname, password_hash) VALUES ($1, $2, $3)
	ON CONFLICT (code) DO NOTHING RETURNING ` + studentColumns

func (r *pgRepository) Create(ctx context.Context, n NewStudent) (Student, error) {
	s, err := scanStudent(r.pool.QueryRow(ctx, insertStudent, n.Code, n.Fullname, n.PasswordHash))
	if errors.Is(err, ErrNotFound) {
		return Student{}, ErrAlreadyExists
	}
	return s, err
}

func (r *pgRepository) Get(ctx context.Context, id int64) (Student, error) {
	return scanStudent(r.pool.QueryRow(ctx, `SELECT `+studentColumns+` FROM students WHERE id = $1`, id))
}

func (r *pgRepository) List(ctx context.Context, query string, limit, offset int) ([]Student, int, error) {
	// Escape LIKE wildcards so the search is a plain substring match.
	pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query) + "%"
	const where = ` WHERE code ILIKE $1 OR fullname ILIKE $1`

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM students`+where, pattern).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+studentColumns+` FROM students`+where+` ORDER BY code, id LIMIT $2 OFFSET $3`,
		pattern, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Student
	for rows.Next() {
		s, err := scanStudent(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, s)
	}
	return out, total, rows.Err()
}

func (r *pgRepository) Import(ctx context.Context, in []NewStudent) ([]Student, []string, error) {
	var created []Student
	var skipped []string
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		created, skipped = nil, nil
		for _, n := range in {
			s, err := scanStudent(tx.QueryRow(ctx, insertStudent, n.Code, n.Fullname, n.PasswordHash))
			if errors.Is(err, ErrNotFound) {
				skipped = append(skipped, n.Code)
				continue
			}
			if err != nil {
				return err
			}
			created = append(created, s)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return created, skipped, nil
}

func (r *pgRepository) ResetPassword(ctx context.Context, id int64, passwordHash string) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var locked int64
		err := tx.QueryRow(ctx, `SELECT id FROM students WHERE id = $1 FOR UPDATE`, id).Scan(&locked)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE students SET password_hash = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`,
			passwordHash, id); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM refresh_tokens WHERE student_id = $1`, id)
		return err
	})
}
