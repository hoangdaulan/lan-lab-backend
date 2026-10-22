package auth

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

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
	tokens, acct, err := svc.Login(context.Background(), "  b21dccn001 ", "B21DCCN001")
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
	if n := repo.tokenCount(student.ID); n != 1 {
		t.Errorf("stored refresh tokens = %d, want 1", n)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	repo := newFakeRepo()
	student := repo.add(t, RoleStudent, "B21DCCN001", "B21DCCN001")
	svc, _ := newTestService(repo)

	_, _, err := svc.Login(context.Background(), "B21DCCN001", "wrong")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
	if n := repo.tokenCount(student.ID); n != 0 {
		t.Errorf("stored refresh tokens = %d, want 0", n)
	}
}

func TestLoginUnknownCode(t *testing.T) {
	svc, _ := newTestService(newFakeRepo())
	_, _, err := svc.Login(context.Background(), "NOPE", "x")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

func TestLoginRoleComesFromAccount(t *testing.T) {
	repo := newFakeRepo()
	repo.add(t, RoleStudent, "SV01", "pw")
	repo.add(t, RoleTeacher, "GV01", "pw")
	svc, _ := newTestService(repo)

	for code, want := range map[string]Role{"SV01": RoleStudent, "GV01": RoleTeacher} {
		tokens, acct, err := svc.Login(context.Background(), code, "pw")
		if err != nil {
			t.Fatalf("Login(%s): %v", code, err)
		}
		if acct.Role != want {
			t.Errorf("Login(%s) role = %s, want %s", code, acct.Role, want)
		}
		p, err := svc.VerifyAccessToken(tokens.AccessToken)
		if err != nil || p.Role != want {
			t.Errorf("Login(%s) JWT role = %s (%v), want %s", code, p.Role, err, want)
		}
	}
}

func TestLoginMissingFields(t *testing.T) {
	svc, _ := newTestService(newFakeRepo())
	for _, c := range [][2]string{{"", "pw"}, {"  ", "pw"}, {"SV01", ""}} {
		if _, _, err := svc.Login(context.Background(), c[0], c[1]); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("Login(%q, %q) err = %v, want ErrInvalidInput", c[0], c[1], err)
		}
	}
}

func TestRefreshRotatesAndOldTokenIsSingleUse(t *testing.T) {
	repo := newFakeRepo()
	teacher := repo.add(t, RoleTeacher, "GV01", "secret")
	svc, clock := newTestService(repo)
	ctx := context.Background()

	first, _, err := svc.Login(ctx, "GV01", "secret")
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
	if n := repo.tokenCount(teacher.ID); n != 1 {
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

	tokens, _, err := svc.Login(ctx, "SV01", "pw")
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

	tokens, _, err := svc.Login(ctx, "SV01", "pw")
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

	a, _, _ := svc.Login(ctx, "SV01", "old")
	b, _, _ := svc.Login(ctx, "SV01", "old")

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
	if _, _, err := svc.Login(ctx, "SV01", "old"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("login with old password err = %v", err)
	}
	if _, _, err := svc.Login(ctx, "SV01", "new"); err != nil {
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

	tokens, _, err := svc.Login(context.Background(), "SV01", "pw")
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
	if _, _, err := svc.Login(ctx, "gv01", "pw"); err != nil {
		t.Fatalf("login seeded teacher: %v", err)
	}
}

func TestCreateTeacherRejectsStudentCode(t *testing.T) {
	repo := newFakeRepo()
	repo.add(t, RoleStudent, "SV01", "pw")
	svc, _ := newTestService(repo)
	if _, err := svc.CreateTeacher(context.Background(), "SV01", "Teacher", "pw"); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("err = %v, want ErrAlreadyExists (codes are unique across roles)", err)
	}
}
