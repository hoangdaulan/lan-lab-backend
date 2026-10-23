package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Hand-written pgx queries: sqlc could not be downloaded in this environment
// (BACKEND_CONTEXT.md §17).

type pgRepository struct {
	pool *pgxpool.Pool
}

var _ Repository = (*pgRepository)(nil)

func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgRepository{pool: pool}
}

const accountColumns = `id, role, code, fullname, password_hash`

func (r *pgRepository) GetAccountByCode(ctx context.Context, code string) (Account, error) {
	return scanAccount(r.pool.QueryRow(ctx, `SELECT `+accountColumns+` FROM users WHERE code = $1`, code))
}

func (r *pgRepository) GetAccountByID(ctx context.Context, id int64) (Account, error) {
	return scanAccount(r.pool.QueryRow(ctx, `SELECT `+accountColumns+` FROM users WHERE id = $1`, id))
}

func (r *pgRepository) IssueRefreshToken(ctx context.Context, accountID int64, expectedPasswordHash string, tokenHash []byte, expiresAt time.Time) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		acct, err := lockAccount(ctx, tx, accountID)
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
			`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
			accountID, tokenHash, expiresAt)
		return err
	})
}

func (r *pgRepository) RotateRefreshToken(ctx context.Context, oldHash, newHash []byte, newExpiresAt, now time.Time) (Account, error) {
	// Find the owner without locking, so the account can be locked first.
	var accountID int64
	err := r.pool.QueryRow(ctx,
		`SELECT user_id FROM refresh_tokens WHERE token_hash = $1`, oldHash).Scan(&accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrInvalidRefreshToken
	}
	if err != nil {
		return Account{}, err
	}

	var acct Account
	expired := false
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		acct, err = lockAccount(ctx, tx, accountID)
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
			 WHERE token_hash = $1 AND user_id = $2 FOR UPDATE`,
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
			`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
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

func (r *pgRepository) ChangePassword(ctx context.Context, accountID int64, expectedPasswordHash, newPasswordHash string) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		acct, err := lockAccount(ctx, tx, accountID)
		if err != nil {
			return err
		}
		if acct.PasswordHash != expectedPasswordHash {
			return ErrInvalidCredentials
		}
		if _, err := tx.Exec(ctx,
			`UPDATE users SET password_hash = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`,
			newPasswordHash, accountID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM refresh_tokens WHERE user_id = $1`, accountID)
		return err
	})
}

func (r *pgRepository) CreateTeacher(ctx context.Context, code, fullname, passwordHash string) (Account, error) {
	acct, err := scanAccount(r.pool.QueryRow(ctx,
		`INSERT INTO users (code, fullname, role, password_hash) VALUES ($1, $2, 'teacher', $3)
		 ON CONFLICT (code) DO NOTHING RETURNING `+accountColumns,
		code, fullname, passwordHash))
	if errors.Is(err, ErrNotFound) {
		return Account{}, ErrAlreadyExists
	}
	return acct, err
}

func lockAccount(ctx context.Context, tx pgx.Tx, id int64) (Account, error) {
	return scanAccount(tx.QueryRow(ctx, `SELECT `+accountColumns+` FROM users WHERE id = $1 FOR UPDATE`, id))
}

func scanAccount(row pgx.Row) (Account, error) {
	var acct Account
	err := row.Scan(&acct.ID, &acct.Role, &acct.Code, &acct.Fullname, &acct.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, err
	}
	return acct, nil
}
