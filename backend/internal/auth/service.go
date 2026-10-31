// Package auth handles teacher/student login, JWT access tokens and refresh
// token rotation (BACKEND_CONTEXT.md §4, §10, §17).
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Repository is the persistence boundary for auth. Methods that change auth
// state lock the account row first, then the refresh token
// (lock order account -> refresh_token, BACKEND_CONTEXT.md §10).
type Repository interface {
	// GetAccountByCode returns ErrNotFound if no user has code. The role comes
	// from the users row (one table for teachers and students).
	GetAccountByCode(ctx context.Context, code string) (Account, error)
	// GetAccountByID returns ErrNotFound if the account does not exist.
	GetAccountByID(ctx context.Context, id int64) (Account, error)

	// IssueRefreshToken stores tokenHash for the account, but only if its
	// password hash still equals expectedPasswordHash under the account lock;
	// otherwise it returns ErrInvalidCredentials.
	IssueRefreshToken(ctx context.Context, accountID int64, expectedPasswordHash string, tokenHash []byte, expiresAt time.Time) error

	// RotateRefreshToken atomically replaces oldHash with newHash and returns
	// the owning account. It returns ErrInvalidRefreshToken if oldHash does not
	// exist or expired at or before now; an expired token is deleted.
	RotateRefreshToken(ctx context.Context, oldHash, newHash []byte, newExpiresAt, now time.Time) (Account, error)

	// DeleteRefreshToken removes tokenHash; a missing token is not an error.
	DeleteRefreshToken(ctx context.Context, tokenHash []byte) error

	// ChangePassword sets newPasswordHash and deletes every refresh token of
	// the account, if the stored hash still equals expectedPasswordHash;
	// otherwise it returns ErrInvalidCredentials.
	ChangePassword(ctx context.Context, accountID int64, expectedPasswordHash, newPasswordHash string) error

	// CreateTeacher returns ErrAlreadyExists if the code is taken by any user.
	CreateTeacher(ctx context.Context, code, fullname, passwordHash string) (Account, error)
}

type Service struct {
	repo       Repository
	signer     *JWTSigner
	refreshTTL time.Duration
	bcryptCost int
	now        func() time.Time

	// dummyHash keeps login timing similar for unknown codes.
	dummyHash []byte
}

type Option func(*Service)

// WithClock overrides time.Now (tests).
func WithClock(now func() time.Time) Option {
	return func(s *Service) { s.now = now }
}

// WithBcryptCost overrides bcrypt.DefaultCost (tests use bcrypt.MinCost).
func WithBcryptCost(cost int) Option {
	return func(s *Service) { s.bcryptCost = cost }
}

func NewService(repo Repository, signer *JWTSigner, refreshTTL time.Duration, opts ...Option) *Service {
	s := &Service{
		repo:       repo,
		signer:     signer,
		refreshTTL: refreshTTL,
		bcryptCost: bcrypt.DefaultCost,
		now:        time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password"), s.bcryptCost)
	return s
}

// Login checks code + password; the account's role (and so the JWT role
// claim) comes from the users row, not from the client.
func (s *Service) Login(ctx context.Context, code, password string) (Tokens, Account, error) {
	code = NormalizeCode(code)
	if code == "" || password == "" {
		return Tokens{}, Account{}, fmt.Errorf("%w: code and password are required", ErrInvalidInput)
	}

	acct, err := s.repo.GetAccountByCode(ctx, code)
	if errors.Is(err, ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		return Tokens{}, Account{}, ErrInvalidCredentials
	}
	if err != nil {
		return Tokens{}, Account{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(acct.PasswordHash), []byte(password)) != nil {
		return Tokens{}, Account{}, ErrInvalidCredentials
	}

	now := s.now()
	refresh, refreshHash, err := newRefreshToken()
	if err != nil {
		return Tokens{}, Account{}, err
	}
	refreshExp := now.Add(s.refreshTTL)
	if err := s.repo.IssueRefreshToken(ctx, acct.ID, acct.PasswordHash, refreshHash, refreshExp); err != nil {
		return Tokens{}, Account{}, err
	}
	tokens, err := s.tokens(acct, refresh, refreshExp, now)
	return tokens, acct, err
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (Tokens, Account, error) {
	oldHash, ok := hashRefreshToken(refreshToken)
	if !ok {
		return Tokens{}, Account{}, ErrInvalidRefreshToken
	}
	now := s.now()
	refresh, newHash, err := newRefreshToken()
	if err != nil {
		return Tokens{}, Account{}, err
	}
	refreshExp := now.Add(s.refreshTTL)
	acct, err := s.repo.RotateRefreshToken(ctx, oldHash, newHash, refreshExp, now)
	if err != nil {
		return Tokens{}, Account{}, err
	}
	tokens, err := s.tokens(acct, refresh, refreshExp, now)
	return tokens, acct, err
}

func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	hash, ok := hashRefreshToken(refreshToken)
	if !ok {
		return nil
	}
	return s.repo.DeleteRefreshToken(ctx, hash)
}

func (s *Service) Me(ctx context.Context, p Principal) (Account, error) {
	return s.repo.GetAccountByID(ctx, p.ID)
}

func (s *Service) ChangePassword(ctx context.Context, p Principal, current, next string) error {
	if err := validatePassword(next); err != nil {
		return err
	}
	acct, err := s.repo.GetAccountByID(ctx, p.ID)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(acct.PasswordHash), []byte(current)) != nil {
		return ErrInvalidCredentials
	}
	newHash, err := s.HashPassword(next)
	if err != nil {
		return err
	}
	return s.repo.ChangePassword(ctx, p.ID, acct.PasswordHash, newHash)
}

// VerifyAccessToken implements TokenVerifier for the HTTP middleware.
// VerifyAccessTokenExpiry is VerifyAccessToken plus the token's expiry.
func (s *Service) VerifyAccessTokenExpiry(token string) (Principal, time.Time, error) {
	return s.signer.VerifyWithExpiry(token, s.now())
}

func (s *Service) VerifyAccessToken(token string) (Principal, error) {
	return s.signer.Verify(token, s.now())
}

// CreateTeacher is used by cmd/seed; teachers are not created over HTTP.
func (s *Service) CreateTeacher(ctx context.Context, code, fullname, password string) (Account, error) {
	code = NormalizeCode(code)
	if code == "" || len(code) > 50 {
		return Account{}, fmt.Errorf("%w: code must be 1-50 characters", ErrInvalidInput)
	}
	if fullname == "" {
		return Account{}, fmt.Errorf("%w: fullname is required", ErrInvalidInput)
	}
	if err := validatePassword(password); err != nil {
		return Account{}, err
	}
	hash, err := s.HashPassword(password)
	if err != nil {
		return Account{}, err
	}
	return s.repo.CreateTeacher(ctx, code, fullname, hash)
}

func (s *Service) HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

func (s *Service) tokens(acct Account, refresh string, refreshExp, now time.Time) (Tokens, error) {
	access, accessExp, err := s.signer.Issue(Principal{ID: acct.ID, Role: acct.Role}, now)
	if err != nil {
		return Tokens{}, err
	}
	return Tokens{
		AccessToken:      access,
		AccessExpiresAt:  accessExp,
		RefreshToken:     refresh,
		RefreshExpiresAt: refreshExp,
	}, nil
}

func validatePassword(p string) error {
	if p == "" || len(p) > maxPasswordBytes {
		return fmt.Errorf("%w: password must be 1-%d bytes", ErrInvalidInput, maxPasswordBytes)
	}
	return nil
}

const refreshTokenBytes = 32

// newRefreshToken returns a random token for the client and the SHA-256 hash
// stored in refresh_tokens.token_hash.
func newRefreshToken() (string, []byte, error) {
	b := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("generate refresh token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(token))
	return token, sum[:], nil
}

func hashRefreshToken(token string) ([]byte, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != refreshTokenBytes {
		return nil, false
	}
	sum := sha256.Sum256([]byte(token))
	return sum[:], true
}
