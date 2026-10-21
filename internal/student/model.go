// Package student lets teachers manage student accounts: create one, import
// a CSV roster, list/search and reset a password back to the student code
// (BACKEND_CONTEXT.md §4, §17).
package student

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/hoangdaulan/lan-lab-backend/internal/auth"
)

type Student struct {
	ID        int64
	Code      string
	Fullname  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewStudent is one validated row to insert; PasswordHash is bcrypt(code).
type NewStudent struct {
	Code         string
	Fullname     string
	PasswordHash string
}

var (
	ErrInvalidInput  = errors.New("invalid input")
	ErrNotFound      = errors.New("student not found")
	ErrAlreadyExists = errors.New("student code already exists")
)

const (
	maxCodeLen     = 50  // students.code varchar(50)
	maxFullnameLen = 200 // students.fullname varchar(200)
)

// normalize trims/upper-cases the code (same rule as login) and trims the
// name, then checks the schema limits.
func normalize(code, fullname string) (string, string, error) {
	code = auth.NormalizeCode(code)
	fullname = strings.Join(strings.Fields(fullname), " ")
	switch {
	case code == "":
		return "", "", fmt.Errorf("%w: code is required", ErrInvalidInput)
	case utf8.RuneCountInString(code) > maxCodeLen:
		return "", "", fmt.Errorf("%w: code longer than %d characters", ErrInvalidInput, maxCodeLen)
	case strings.IndexFunc(code, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0:
		return "", "", fmt.Errorf("%w: code must not contain spaces", ErrInvalidInput)
	case fullname == "":
		return "", "", fmt.Errorf("%w: fullname is required", ErrInvalidInput)
	case utf8.RuneCountInString(fullname) > maxFullnameLen:
		return "", "", fmt.Errorf("%w: fullname longer than %d characters", ErrInvalidInput, maxFullnameLen)
	}
	return code, fullname, nil
}
