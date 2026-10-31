// Package student lets teachers manage student accounts: create one, import
// a CSV roster, list/search and reset a password back to the initial one
// (BACKEND_CONTEXT.md §4, §17). The initial password is the date of birth as
// ddmmyyyy; students created before date_of_birth existed fall back to the code.
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
	ID       int64
	Code     string
	Fullname string
	// DateOfBirth is nil only for students created before the column existed.
	DateOfBirth *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// NewStudent is one validated row to insert; PasswordHash is
// bcrypt(InitialPassword).
type NewStudent struct {
	Code         string
	Fullname     string
	DateOfBirth  time.Time
	PasswordHash string
}

// ExistingCode is an import row skipped because a user already has the code.
type ExistingCode struct {
	Code string
	Role string // "student" or "teacher"
}

var (
	ErrInvalidInput  = errors.New("invalid input")
	ErrNotFound      = errors.New("student not found")
	ErrAlreadyExists = errors.New("code already used by another account")
)

const (
	maxCodeLen     = 50  // users.code varchar(50)
	maxFullnameLen = 200 // users.fullname varchar(200)
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

// dobLayouts are the accepted date-of-birth inputs: dd/mm/yyyy as written in
// Vietnam (also with - or .; day and month may be one digit) and ISO
// yyyy-mm-dd, which Excel uses when a cell is formatted as a date.
var dobLayouts = []string{"2/1/2006", "2-1-2006", "2.1.2006", "2006-1-2"}

// minBirthYear rejects obvious typos such as 01/01/0205.
const minBirthYear = 1900

// parseDateOfBirth parses s with dobLayouts and checks it is a real past date.
func parseDateOfBirth(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("%w: date_of_birth is required", ErrInvalidInput)
	}
	for _, layout := range dobLayouts {
		d, err := time.Parse(layout, s)
		if err != nil {
			continue
		}
		if d.Year() < minBirthYear || d.After(now) {
			return time.Time{}, fmt.Errorf("%w: date_of_birth %q is out of range", ErrInvalidInput, s)
		}
		return d, nil
	}
	return time.Time{}, fmt.Errorf("%w: date_of_birth %q must be dd/mm/yyyy or yyyy-mm-dd", ErrInvalidInput, s)
}

// InitialPassword is the date of birth as ddmmyyyy, e.g. 05/09/2004 -> "05092004".
func InitialPassword(dob time.Time) string {
	return dob.Format("02012006")
}
