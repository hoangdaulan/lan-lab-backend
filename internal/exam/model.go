// Package exam manages exams, exam sessions and student participation
// (BACKEND_CONTEXT.md §5, §17).
package exam

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hoangdaulan/lan-lab-backend/internal/apperr"
)

type Exam struct {
	ID          int64
	TeacherID   int64
	Title       string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ExamDetail includes the complete problem summary list for the owning teacher.
type ExamDetail struct {
	Exam
	Problems []ProblemSummary
}

type Session struct {
	ID        int64
	ExamID    int64
	ExamTitle string
	TeacherID int64 // owner of the exam, for permission checks
	Name      string
	StartsAt  time.Time
	EndsAt    time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SessionState is derived from server time; submissions are accepted only
// while running (starts_at <= now < ends_at).
type SessionState string

const (
	StateUpcoming SessionState = "upcoming"
	StateRunning  SessionState = "running"
	StateEnded    SessionState = "ended"
)

func (s Session) State(now time.Time) SessionState {
	switch {
	case now.Before(s.StartsAt):
		return StateUpcoming
	case now.Before(s.EndsAt):
		return StateRunning
	default:
		return StateEnded
	}
}

const (
	ParticipantInProgress = "in_progress"
	ParticipantFinished   = "finished"
)

type Participant struct {
	ID              int64
	SessionID       int64
	StudentID       int64
	StudentCode     string
	StudentFullname string
	Status          string
	JoinedAt        time.Time
	FinishedAt      *time.Time
}

// ProblemSummary is the problem list shown with a session.
type ProblemSummary struct {
	ID    int64
	Code  string
	Title string
}

// StudentSession is a session as listed for a student, with their
// participation if they joined.
type StudentSession struct {
	Session
	Participation *Participant
}

// SessionView is GET /sessions/{id}.
type SessionView struct {
	Session       Session
	ServerTime    time.Time
	State         SessionState
	Participation *Participant // students only
	Problems      []ProblemSummary
	// ProblemsHidden is true when a student asks before starts_at.
	ProblemsHidden bool
}

const (
	maxTitleLen = 255 // exams.title, exam_sessions.name varchar(255)
)

func cleanTitle(field, v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", apperr.Invalid("%s is required", field)
	}
	if utf8.RuneCountInString(v) > maxTitleLen {
		return "", apperr.Invalid("%s longer than %d characters", field, maxTitleLen)
	}
	return v, nil
}

func validateWindow(startsAt, endsAt time.Time) error {
	if startsAt.IsZero() || endsAt.IsZero() {
		return apperr.Invalid("starts_at and ends_at are required")
	}
	if !endsAt.After(startsAt) {
		return apperr.Invalid("ends_at must be after starts_at")
	}
	return nil
}
