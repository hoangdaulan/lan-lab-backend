package auth

import (
	"errors"
	"strings"
	"time"
)

// Role is the account type, stored in users.role. Login reads it from the DB
// and puts it in the JWT (BACKEND_CONTEXT.md §18).
type Role string

const (
	RoleTeacher Role = "teacher"
	RoleStudent Role = "student"
)

func (r Role) Valid() bool {
	return r == RoleTeacher || r == RoleStudent
}

// Account is the auth view of a teacher or student row.
type Account struct {
	ID           int64
	Role         Role
	Code         string
	Fullname     string
	PasswordHash string
}

// Principal is the authenticated caller, taken from a verified access token.
type Principal struct {
	ID   int64
	Role Role
}

// Tokens is what login and refresh return to the client.
type Tokens struct {
	AccessToken      string
	AccessExpiresAt  time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
}

var (
	// ErrInvalidCredentials covers unknown code and wrong password alike, so
	// the response does not reveal which accounts exist.
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrInvalidRefreshToken = errors.New("invalid refresh token")
	ErrInvalidAccessToken  = errors.New("invalid access token")
	ErrInvalidInput        = errors.New("invalid input")
	ErrNotFound            = errors.New("not found")
	ErrAlreadyExists       = errors.New("already exists")
)

// maxPasswordBytes is bcrypt's input limit; longer passwords are rejected
// instead of being silently truncated.
const maxPasswordBytes = 72

// NormalizeCode trims and upper-cases an account code. Codes are
// case-sensitive in the DB, so every write and lookup goes through this.
func NormalizeCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}
