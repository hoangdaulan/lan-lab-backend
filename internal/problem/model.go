// Package problem manages problems, their on-disk test sets and versioning
// (BACKEND_CONTEXT.md §6, §8, §17).
package problem

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/hoangdaulan/lan-lab-backend/internal/apperr"
)

type Problem struct {
	ID            int64
	ExamID        int64
	TeacherID     int64 // owner of the exam, for permission checks
	Code          string
	Title         string
	Statement     string
	TimeLimitMs   int
	MemoryLimitMb int
	Version       int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Input is the editable part of a problem (create and full update).
type Input struct {
	Code          string
	Title         string
	Statement     string
	TimeLimitMs   int
	MemoryLimitMb int
}

// TestCase is a test_cases row; paths are relative to DATA_DIR.
type TestCase struct {
	ID         int64
	InputPath  string
	OutputPath string
}

// TestFile is one .in/.out pair found on disk.
type TestFile struct {
	Name       string // base name without extension, e.g. "1"
	InputPath  string // relative to DATA_DIR
	OutputPath string
	InputSize  int64
	OutputSize int64
}

// SessionWindow is what the student view needs from exam_sessions.
type SessionWindow struct {
	ExamID   int64
	StartsAt time.Time
	EndsAt   time.Time
}

const (
	maxCodeLen  = 20  // problems.code varchar(20)
	maxTitleLen = 255 // problems.title varchar(255)

	// Limits are not fixed by the course; these bounds only reject typos.
	MaxTimeLimitMs   = 10_000
	MaxMemoryLimitMb = 1024
)

func (in Input) normalize() (Input, error) {
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.Title = strings.TrimSpace(in.Title)
	switch {
	case in.Code == "":
		return Input{}, apperr.Invalid("code is required")
	case utf8.RuneCountInString(in.Code) > maxCodeLen:
		return Input{}, apperr.Invalid("code longer than %d characters", maxCodeLen)
	case strings.IndexFunc(in.Code, unicode.IsSpace) >= 0:
		return Input{}, apperr.Invalid("code must not contain spaces")
	case in.Title == "":
		return Input{}, apperr.Invalid("title is required")
	case utf8.RuneCountInString(in.Title) > maxTitleLen:
		return Input{}, apperr.Invalid("title longer than %d characters", maxTitleLen)
	case in.TimeLimitMs < 1 || in.TimeLimitMs > MaxTimeLimitMs:
		return Input{}, apperr.Invalid("time_limit_ms must be 1-%d", MaxTimeLimitMs)
	case in.MemoryLimitMb < 1 || in.MemoryLimitMb > MaxMemoryLimitMb:
		return Input{}, apperr.Invalid("memory_limit_mb must be 1-%d", MaxMemoryLimitMb)
	}
	return in, nil
}

// LimitsChanged reports whether an update must bump the version and
// rejudge (BACKEND_CONTEXT.md §17).
func LimitsChanged(old Problem, in Input) bool {
	return old.TimeLimitMs != in.TimeLimitMs || old.MemoryLimitMb != in.MemoryLimitMb
}
