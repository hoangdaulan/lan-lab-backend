package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Hand-written pgx queries: sqlc could not be downloaded in this environment
// (BACKEND_CONTEXT.md §17). Table and column names below come only from the
// constants in tableFor/ownerColumn, never from input.

type pgRepository struct {
	pool *pgxpool.Pool
}

var _ Repository = (*pgRepository)(nil)

func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgRepository{pool: pool}
}

func tableFor(role Role) (string, error) {
	switch role {
	case RoleTeacher:
		return "teachers", nil
	case RoleStudent:
		return "students", nil
	}
	return "", fmt.Errorf("%w: unknown role %q", ErrInvalidInput, role)
}

func ownerColumn(role Role) string {
	if role == RoleTeacher {
		return "teacher_id"
	}
	return "student_id"
}

func (r *pgRepository) GetAccountByCode(ctx context.Context, role Role, code string) (Account, error) {
	table, err := tableFor(role)
	if err != nil {
		return Account{}, err
	}
	return scanAccount(r.pool.QueryRow(ctx,
		`SELECT id, code, fullname, password_hash FROM `+table+` WHERE code = $1`, code), role)
}

func (r *pgRepository) GetAccountByID(ctx context.Context, role Role, id int64) (Account, error) {
	table, err := tableFor(role)
	if err != nil {
		return Account{}, err
	}
	return scanAccount(r.pool.QueryRow(ctx,
		`SELECT id, code, fullname, password_hash FROM `+table+` WHERE id = $1`, id), role)
}

func (r *pgRepository) IssueRefreshToken(ctx context.Context, role Role, accountID int64, expectedPasswordHash string, tokenHash []byte, expiresAt time.Time) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		acct, err := lockAccount(ctx, tx, role, accountID)
		if errors.Is(err, ErrNotFound) {
			return ErrInvalidCredentials
		}
		if err != nil {
			return err
		}
		if acct.PasswordHash != expectedPasswordHash {
			// Password changed between verification and issuing.
			return ErrInvalidCredentials
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO refresh_tokens (`+ownerColumn(role)+`, token_hash, expires_at) VALUES ($1, $2, $3)`,
			accountID, tokenHash, expiresAt)
		return err
	})
}

func (r *pgRepository) RotateRefreshToken(ctx context.Context, oldHash, newHash []byte, newExpiresAt, now time.Time) (Account, error) {
	// Find the owner without locking, so the account can be locked first.
	var teacherID, studentID *int64
	err := r.pool.QueryRow(ctx,
		`SELECT teacher_id, student_id FROM refresh_tokens WHERE token_hash = $1`, oldHash).
		Scan(&teacherID, &studentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrInvalidRefreshToken
	}
	if err != nil {
		return Account{}, err
	}
	role, accountID := RoleStudent, int64(0)
	if teacherID != nil {
		role, accountID = RoleTeacher, *teacherID
	} else if studentID != nil {
		accountID = *studentID
	}

	var acct Account
	expired := false
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		acct, err = lockAccount(ctx, tx, role, accountID)
		if errors.Is(err, ErrNotFound) {
			return ErrInvalidRefreshToken
		}
		if err != nil {
			return err
		}

		// Re-read the token under lock: a concurrent refresh, logout or
		// password change may have deleted it.
		var tokenID int64
		var expiresAt time.Time
		err = tx.QueryRow(ctx,
			`SELECT id, expires_at FROM refresh_tokens
			 WHERE token_hash = $1 AND `+ownerColumn(role)+` = $2 FOR UPDATE`,
			oldHash, accountID).Scan(&tokenID, &expiresAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidRefreshToken
		}
		if err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `DELETE FROM refresh_tokens WHERE id = $1`, tokenID); err != nil {
			return err
		}
		if !expiresAt.After(now) {
			expired = true
			return nil // commit the cleanup, then report invalid
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO refresh_tokens (`+ownerColumn(role)+`, token_hash, expires_at) VALUES ($1, $2, $3)`,
			accountID, newHash, newExpiresAt)
		return err
	})
	if err != nil {
		return Account{}, err
	}
	if expired {
		return Account{}, ErrInvalidRefreshToken
	}
	return acct, nil
}

func (r *pgRepository) DeleteRefreshToken(ctx context.Context, tokenHash []byte) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM refresh_tokens WHERE token_hash = $1`, tokenHash)
	return err
}

func (r *pgRepository) ChangePassword(ctx context.Context, role Role, accountID int64, expectedPasswordHash, newPasswordHash string) error {
	table, err := tableFor(role)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		acct, err := lockAccount(ctx, tx, role, accountID)
		if err != nil {
			return err
		}
		if acct.PasswordHash != expectedPasswordHash {
			return ErrInvalidCredentials
		}
		if _, err := tx.Exec(ctx,
			`UPDATE `+table+` SET password_hash = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`,
			newPasswordHash, accountID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx,
			`DELETE FROM refresh_tokens WHERE `+ownerColumn(role)+` = $1`, accountID)
		return err
	})
}

func (r *pgRepository) CreateTeacher(ctx context.Context, code, fullname, passwordHash string) (Account, error) {
	acct := Account{Role: RoleTeacher, Code: code, Fullname: fullname, PasswordHash: passwordHash}
	err := r.pool.QueryRow(ctx,
		`INSERT INTO teachers (code, fullname, password_hash) VALUES ($1, $2, $3)
		 ON CONFLICT (code) DO NOTHING RETURNING id`,
		code, fullname, passwordHash).Scan(&acct.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrAlreadyExists
	}
	if err != nil {
		return Account{}, err
	}
	return acct, nil
}

func lockAccount(ctx context.Context, tx pgx.Tx, role Role, id int64) (Account, error) {
	table, err := tableFor(role)
	if err != nil {
		return Account{}, err
	}
	return scanAccount(tx.QueryRow(ctx,
		`SELECT id, code, fullname, password_hash FROM `+table+` WHERE id = $1 FOR UPDATE`, id), role)
}

func scanAccount(row pgx.Row, role Role) (Account, error) {
	acct := Account{Role: role}
	err := row.Scan(&acct.ID, &acct.Code, &acct.Fullname, &acct.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, err
	}
	return acct, nil
}
