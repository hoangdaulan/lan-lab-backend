package auth

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// fakeRepo is an in-memory Repository with the same semantics as the pgx one.
type fakeRepo struct {
	mu       sync.Mutex
	accounts map[Role]map[int64]*Account
	tokens   map[string]fakeToken // key: string(hash)
	nextID   int64
}

type fakeToken struct {
	role      Role
	accountID int64
	expiresAt time.Time
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		accounts: map[Role]map[int64]*Account{RoleTeacher: {}, RoleStudent: {}},
		tokens:   map[string]fakeToken{},
	}
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
	f.accounts[role][a.ID] = a
	return *a
}

func (f *fakeRepo) tokenCount(role Role, id int64) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, t := range f.tokens {
		if t.role == role && t.accountID == id {
			n++
		}
	}
	return n
}

func (f *fakeRepo) GetAccountByCode(_ context.Context, role Role, code string) (Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.accounts[role] {
		if a.Code == code {
			return *a, nil
		}
	}
	return Account{}, ErrNotFound
}

func (f *fakeRepo) GetAccountByID(_ context.Context, role Role, id int64) (Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.accounts[role][id]; ok {
		return *a, nil
	}
	return Account{}, ErrNotFound
}

func (f *fakeRepo) IssueRefreshToken(_ context.Context, role Role, id int64, expected string, hash []byte, exp time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.accounts[role][id]
	if !ok || a.PasswordHash != expected {
		return ErrInvalidCredentials
	}
	f.tokens[string(hash)] = fakeToken{role: role, accountID: id, expiresAt: exp}
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
	a, ok := f.accounts[t.role][t.accountID]
	if !ok {
		return Account{}, ErrInvalidRefreshToken
	}
	f.tokens[string(newHash)] = fakeToken{role: t.role, accountID: t.accountID, expiresAt: newExp}
	return *a, nil
}

func (f *fakeRepo) DeleteRefreshToken(_ context.Context, hash []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.tokens, string(hash))
	return nil
}

func (f *fakeRepo) ChangePassword(_ context.Context, role Role, id int64, expected, newHash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.accounts[role][id]
	if !ok {
		return ErrNotFound
	}
	if a.PasswordHash != expected {
		return ErrInvalidCredentials
	}
	a.PasswordHash = newHash
	for k, t := range f.tokens {
		if t.role == role && t.accountID == id {
			delete(f.tokens, k)
		}
	}
	return nil
}

func (f *fakeRepo) CreateTeacher(_ context.Context, code, fullname, hash string) (Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.accounts[RoleTeacher] {
		if a.Code == code {
			return Account{}, ErrAlreadyExists
		}
	}
	f.nextID++
	a := &Account{ID: f.nextID, Role: RoleTeacher, Code: code, Fullname: fullname, PasswordHash: hash}
	f.accounts[RoleTeacher][a.ID] = a
	return *a, nil
}

type testClock struct{ t time.Time }

func (c *testClock) now() time.Time          { return c.t }
func (c *testClock) advance(d time.Duration) { c.t = c.t.Add(d) }

const testSecret = "test-secret-test-secret-test-secret!"

func newTestService(repo Repository) (*Service, *testClock) {
	clock := &testClock{t: time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)}
	svc := NewService(repo, NewJWTSigner(testSecret, 15*time.Minute), 7*24*time.Hour,
		WithClock(clock.now), WithBcryptCost(bcrypt.MinCost))
	return svc, clock
}

func TestLoginSuccessIssuesValidTokens(t *testing.T) {
	repo := newFakeRepo()
	student := repo.add(t, RoleStudent, "B21DCCN001", "B21DCCN001")
	svc, clock := newTestService(repo)

	// Code is normalized (trim + upper) before lookup.
	tokens, acct, err := svc.Login(context.Background(), RoleStudent, "  b21dccn001 ", "B21DCCN001")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if acct.ID != student.ID {
		t.Fatalf("account id = %d, want %d", acct.ID, student.ID)
	}
	if want := clock.t.Add(15 * time.Minute); !tokens.AccessExpiresAt.Equal(want) {
		t.Errorf("access exp = %v, want %v", tokens.AccessExpiresAt, want)
	}
	if want := clock.t.Add(7 * 24 * time.Hour); !tokens.RefreshExpiresAt.Equal(want) {
		t.Errorf("refresh exp = %v, want %v", tokens.RefreshExpiresAt, want)
	}
	p, err := svc.VerifyAccessToken(tokens.AccessToken)
	if err != nil {
		t.Fatalf("VerifyAccessToken: %v", err)
	}
	if p != (Principal{ID: student.ID, Role: RoleStudent}) {
		t.Errorf("principal = %+v", p)
	}
	if n := repo.tokenCount(RoleStudent, student.ID); n != 1 {
		t.Errorf("stored refresh tokens = %d, want 1", n)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	repo := newFakeRepo()
	student := repo.add(t, RoleStudent, "B21DCCN001", "B21DCCN001")
	svc, _ := newTestService(repo)

	_, _, err := svc.Login(context.Background(), RoleStudent, "B21DCCN001", "wrong")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
	if n := repo.tokenCount(RoleStudent, student.ID); n != 0 {
		t.Errorf("stored refresh tokens = %d, want 0", n)
	}
}

func TestLoginUnknownCode(t *testing.T) {
	svc, _ := newTestService(newFakeRepo())
	_, _, err := svc.Login(context.Background(), RoleTeacher, "NOPE", "x")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

func TestLoginRoleMismatch(t *testing.T) {
	repo := newFakeRepo()
	repo.add(t, RoleStudent, "SV01", "pw")
	svc, _ := newTestService(repo)

	// A student code and password presented with role=teacher must fail:
	// codes are only unique per table.
	_, _, err := svc.Login(context.Background(), RoleTeacher, "SV01", "pw")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

func TestLoginInvalidRole(t *testing.T) {
	svc, _ := newTestService(newFakeRepo())
	_, _, err := svc.Login(context.Background(), Role("admin"), "X", "y")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}

func TestRefreshRotatesAndOldTokenIsSingleUse(t *testing.T) {
	repo := newFakeRepo()
	teacher := repo.add(t, RoleTeacher, "GV01", "secret")
	svc, clock := newTestService(repo)
	ctx := context.Background()

	first, _, err := svc.Login(ctx, RoleTeacher, "GV01", "secret")
	if err != nil {
		t.Fatal(err)
	}
	clock.advance(time.Minute)

	second, acct, err := svc.Refresh(ctx, first.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if acct.ID != teacher.ID || acct.Role != RoleTeacher {
		t.Errorf("refresh account = %+v", acct)
	}
	if second.RefreshToken == first.RefreshToken {
		t.Error("refresh token was not rotated")
	}
	if n := repo.tokenCount(RoleTeacher, teacher.ID); n != 1 {
		t.Errorf("stored refresh tokens = %d, want 1 (old replaced)", n)
	}

	// Reusing the old token fails.
	if _, _, err := svc.Refresh(ctx, first.RefreshToken); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("reuse err = %v, want ErrInvalidRefreshToken", err)
	}
	// The new one still works once.
	if _, _, err := svc.Refresh(ctx, second.RefreshToken); err != nil {
		t.Fatalf("Refresh with new token: %v", err)
	}
}

func TestRefreshExpired(t *testing.T) {
	repo := newFakeRepo()
	repo.add(t, RoleStudent, "SV01", "pw")
	svc, clock := newTestService(repo)
	ctx := context.Background()

	tokens, _, err := svc.Login(ctx, RoleStudent, "SV01", "pw")
	if err != nil {
		t.Fatal(err)
	}
	clock.advance(7 * 24 * time.Hour) // exactly at expiry: not valid any more
	if _, _, err := svc.Refresh(ctx, tokens.RefreshToken); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("err = %v, want ErrInvalidRefreshToken", err)
	}
}

func TestRefreshGarbageToken(t *testing.T) {
	svc, _ := newTestService(newFakeRepo())
	for _, tok := range []string{"", "not-base64!!", "c2hvcnQ"} {
		if _, _, err := svc.Refresh(context.Background(), tok); !errors.Is(err, ErrInvalidRefreshToken) {
			t.Errorf("Refresh(%q) err = %v, want ErrInvalidRefreshToken", tok, err)
		}
	}
}

func TestLogoutRevokesRefreshToken(t *testing.T) {
	repo := newFakeRepo()
	repo.add(t, RoleStudent, "SV01", "pw")
	svc, _ := newTestService(repo)
	ctx := context.Background()

	tokens, _, err := svc.Login(ctx, RoleStudent, "SV01", "pw")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Logout(ctx, tokens.RefreshToken); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, _, err := svc.Refresh(ctx, tokens.RefreshToken); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("err = %v, want ErrInvalidRefreshToken", err)
	}
}

func TestChangePasswordRevokesAllRefreshTokens(t *testing.T) {
	repo := newFakeRepo()
	student := repo.add(t, RoleStudent, "SV01", "old")
	svc, _ := newTestService(repo)
	ctx := context.Background()
	p := Principal{ID: student.ID, Role: RoleStudent}

	a, _, _ := svc.Login(ctx, RoleStudent, "SV01", "old")
	b, _, _ := svc.Login(ctx, RoleStudent, "SV01", "old")

	if err := svc.ChangePassword(ctx, p, "wrong", "new"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong current password err = %v, want ErrInvalidCredentials", err)
	}
	if err := svc.ChangePassword(ctx, p, "old", "new"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	for _, tok := range []string{a.RefreshToken, b.RefreshToken} {
		if _, _, err := svc.Refresh(ctx, tok); !errors.Is(err, ErrInvalidRefreshToken) {
			t.Errorf("refresh after password change err = %v, want ErrInvalidRefreshToken", err)
		}
	}
	if _, _, err := svc.Login(ctx, RoleStudent, "SV01", "old"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("login with old password err = %v", err)
	}
	if _, _, err := svc.Login(ctx, RoleStudent, "SV01", "new"); err != nil {
		t.Errorf("login with new password: %v", err)
	}
}

func TestChangePasswordRejectsTooLong(t *testing.T) {
	repo := newFakeRepo()
	student := repo.add(t, RoleStudent, "SV01", "old")
	svc, _ := newTestService(repo)
	long := string(bytes.Repeat([]byte("a"), maxPasswordBytes+1))
	err := svc.ChangePassword(context.Background(), Principal{ID: student.ID, Role: RoleStudent}, "old", long)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}

func TestAccessTokenExpiresAndRejectsTampering(t *testing.T) {
	repo := newFakeRepo()
	repo.add(t, RoleStudent, "SV01", "pw")
	svc, clock := newTestService(repo)

	tokens, _, err := svc.Login(context.Background(), RoleStudent, "SV01", "pw")
	if err != nil {
		t.Fatal(err)
	}
	other := NewJWTSigner("another-secret-another-secret-123", 15*time.Minute)
	if _, err := other.Verify(tokens.AccessToken, clock.t); !errors.Is(err, ErrInvalidAccessToken) {
		t.Errorf("verify with other secret err = %v", err)
	}
	clock.advance(15*time.Minute + time.Second)
	if _, err := svc.VerifyAccessToken(tokens.AccessToken); !errors.Is(err, ErrInvalidAccessToken) {
		t.Errorf("expired token err = %v, want ErrInvalidAccessToken", err)
	}
}

func TestCreateTeacherIdempotent(t *testing.T) {
	svc, _ := newTestService(newFakeRepo())
	ctx := context.Background()
	if _, err := svc.CreateTeacher(ctx, "gv01", "Teacher", "pw"); err != nil {
		t.Fatalf("CreateTeacher: %v", err)
	}
	if _, err := svc.CreateTeacher(ctx, "GV01", "Teacher", "pw"); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("second CreateTeacher err = %v, want ErrAlreadyExists", err)
	}
	if _, _, err := svc.Login(ctx, RoleTeacher, "gv01", "pw"); err != nil {
		t.Fatalf("login seeded teacher: %v", err)
	}
}
