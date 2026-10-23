package auth

import (
	"context"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// fakeRepo is an in-memory Repository with the same semantics as the pgx one:
// one users table, code unique across roles.
type fakeRepo struct {
	mu       sync.Mutex
	accounts map[int64]*Account
	tokens   map[string]fakeToken // key: string(hash)
	nextID   int64
}

type fakeToken struct {
	accountID int64
	expiresAt time.Time
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{accounts: map[int64]*Account{}, tokens: map[string]fakeToken{}}
}

func (f *fakeRepo) add(t *testing.T, role Role, code, password string) Account {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	a := &Account{ID: f.nextID, Role: role, Code: code, Fullname: "Test " + code, PasswordHash: string(hash)}
	f.accounts[a.ID] = a
	return *a
}

func (f *fakeRepo) tokenCount(id int64) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, t := range f.tokens {
		if t.accountID == id {
			n++
		}
	}
	return n
}

func (f *fakeRepo) GetAccountByCode(_ context.Context, code string) (Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.accounts {
		if a.Code == code {
			return *a, nil
		}
	}
	return Account{}, ErrNotFound
}

func (f *fakeRepo) GetAccountByID(_ context.Context, id int64) (Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.accounts[id]; ok {
		return *a, nil
	}
	return Account{}, ErrNotFound
}

func (f *fakeRepo) IssueRefreshToken(_ context.Context, id int64, expected string, hash []byte, exp time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.accounts[id]
	if !ok || a.PasswordHash != expected {
		return ErrInvalidCredentials
	}
	f.tokens[string(hash)] = fakeToken{accountID: id, expiresAt: exp}
	return nil
}

func (f *fakeRepo) RotateRefreshToken(_ context.Context, oldHash, newHash []byte, newExp, now time.Time) (Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tokens[string(oldHash)]
	if !ok {
		return Account{}, ErrInvalidRefreshToken
	}
	delete(f.tokens, string(oldHash))
	if !t.expiresAt.After(now) {
		return Account{}, ErrInvalidRefreshToken
	}
	a, ok := f.accounts[t.accountID]
	if !ok {
		return Account{}, ErrInvalidRefreshToken
	}
	f.tokens[string(newHash)] = fakeToken{accountID: t.accountID, expiresAt: newExp}
	return *a, nil
}

func (f *fakeRepo) DeleteRefreshToken(_ context.Context, hash []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.tokens, string(hash))
	return nil
}

func (f *fakeRepo) ChangePassword(_ context.Context, id int64, expected, newHash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.accounts[id]
	if !ok {
		return ErrNotFound
	}
	if a.PasswordHash != expected {
		return ErrInvalidCredentials
	}
	a.PasswordHash = newHash
	for k, t := range f.tokens {
		if t.accountID == id {
			delete(f.tokens, k)
		}
	}
	return nil
}

func (f *fakeRepo) CreateTeacher(_ context.Context, code, fullname, hash string) (Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.accounts {
		if a.Code == code {
			return Account{}, ErrAlreadyExists
		}
	}
	f.nextID++
	a := &Account{ID: f.nextID, Role: RoleTeacher, Code: code, Fullname: fullname, PasswordHash: hash}
	f.accounts[a.ID] = a
	return *a, nil
}
