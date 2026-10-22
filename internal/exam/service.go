package exam

import (
	"context"
	"errors"
	"time"

	"github.com/hoangdaulan/lan-lab-backend/internal/apperr"
)

// Repository is the persistence boundary for exams and sessions. Lookups
// return apperr.ErrNotFound for missing rows.
type Repository interface {
	CreateExam(ctx context.Context, teacherID int64, title, description string) (Exam, error)
	ListExams(ctx context.Context, teacherID int64) ([]Exam, error)
	GetExam(ctx context.Context, id int64) (Exam, error)
	UpdateExam(ctx context.Context, id int64, title, description string) (Exam, error)
	// DeleteExam returns apperr.ErrConflict while the exam still has
	// problems or sessions.
	DeleteExam(ctx context.Context, id int64) error

	CreateSession(ctx context.Context, examID int64, name string, startsAt, endsAt time.Time) (Session, error)
	ListSessionsByExam(ctx context.Context, examID int64) ([]Session, error)
	ListAllSessions(ctx context.Context) ([]Session, error)
	GetSession(ctx context.Context, id int64) (Session, error)
	UpdateSession(ctx context.Context, id int64, name string, startsAt, endsAt time.Time) (Session, error)
	// DeleteSession removes the session and its participants, or returns
	// apperr.ErrConflict if any participant has submitted.
	DeleteSession(ctx context.Context, id int64) error

	ListParticipants(ctx context.Context, sessionID int64) ([]Participant, error)
	GetParticipant(ctx context.Context, sessionID, studentID int64) (Participant, error)
	ListParticipationsByStudent(ctx context.Context, studentID int64) ([]Participant, error)
	// CreateParticipant inserts (session, student) or returns the existing row.
	CreateParticipant(ctx context.Context, sessionID, studentID int64, joinedAt time.Time) (Participant, error)
	// FinishParticipant moves in_progress -> finished at finishedAt (no-op if
	// already finished) and returns the row.
	FinishParticipant(ctx context.Context, sessionID, studentID int64, finishedAt time.Time) (Participant, error)

	ListProblems(ctx context.Context, examID int64) ([]ProblemSummary, error)
}

type Service struct {
	repo Repository
	now  func() time.Time
}

func NewService(repo Repository, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{repo: repo, now: now}
}

// ---- exams (teacher) ----

func (s *Service) CreateExam(ctx context.Context, teacherID int64, title, description string) (Exam, error) {
	title, err := cleanTitle("title", title)
	if err != nil {
		return Exam{}, err
	}
	return s.repo.CreateExam(ctx, teacherID, title, description)
}

func (s *Service) ListExams(ctx context.Context, teacherID int64) ([]Exam, error) {
	return s.repo.ListExams(ctx, teacherID)
}

// OwnedExam returns the exam if teacherID owns it; other teachers get
// ErrNotFound.
func (s *Service) OwnedExam(ctx context.Context, teacherID, examID int64) (Exam, error) {
	e, err := s.repo.GetExam(ctx, examID)
	if err != nil {
		return Exam{}, err
	}
	if e.TeacherID != teacherID {
		return Exam{}, apperr.NotFound("exam")
	}
	return e, nil
}

// ExamDetail checks ownership before reading the exam's problem summaries.
// ListProblems returns them ordered by code, then id.
func (s *Service) ExamDetail(ctx context.Context, teacherID, examID int64) (ExamDetail, error) {
	e, err := s.OwnedExam(ctx, teacherID, examID)
	if err != nil {
		return ExamDetail{}, err
	}
	problems, err := s.repo.ListProblems(ctx, examID)
	if err != nil {
		return ExamDetail{}, err
	}
	return ExamDetail{Exam: e, Problems: problems}, nil
}

func (s *Service) UpdateExam(ctx context.Context, teacherID, examID int64, title, description string) (Exam, error) {
	if _, err := s.OwnedExam(ctx, teacherID, examID); err != nil {
		return Exam{}, err
	}
	title, err := cleanTitle("title", title)
	if err != nil {
		return Exam{}, err
	}
	return s.repo.UpdateExam(ctx, examID, title, description)
}

func (s *Service) DeleteExam(ctx context.Context, teacherID, examID int64) error {
	if _, err := s.OwnedExam(ctx, teacherID, examID); err != nil {
		return err
	}
	return s.repo.DeleteExam(ctx, examID)
}

// ---- sessions (teacher) ----

func (s *Service) CreateSession(ctx context.Context, teacherID, examID int64, name string, startsAt, endsAt time.Time) (Session, error) {
	if _, err := s.OwnedExam(ctx, teacherID, examID); err != nil {
		return Session{}, err
	}
	name, err := cleanTitle("name", name)
	if err != nil {
		return Session{}, err
	}
	if err := validateWindow(startsAt, endsAt); err != nil {
		return Session{}, err
	}
	return s.repo.CreateSession(ctx, examID, name, startsAt, endsAt)
}

func (s *Service) ListSessions(ctx context.Context, teacherID, examID int64) ([]Session, error) {
	if _, err := s.OwnedExam(ctx, teacherID, examID); err != nil {
		return nil, err
	}
	return s.repo.ListSessionsByExam(ctx, examID)
}

func (s *Service) ownedSession(ctx context.Context, teacherID, sessionID int64) (Session, error) {
	ses, err := s.repo.GetSession(ctx, sessionID)
	if err != nil {
		return Session{}, err
	}
	if ses.TeacherID != teacherID {
		return Session{}, apperr.NotFound("session")
	}
	return ses, nil
}

func (s *Service) UpdateSession(ctx context.Context, teacherID, sessionID int64, name string, startsAt, endsAt time.Time) (Session, error) {
	if _, err := s.ownedSession(ctx, teacherID, sessionID); err != nil {
		return Session{}, err
	}
	name, err := cleanTitle("name", name)
	if err != nil {
		return Session{}, err
	}
	if err := validateWindow(startsAt, endsAt); err != nil {
		return Session{}, err
	}
	return s.repo.UpdateSession(ctx, sessionID, name, startsAt, endsAt)
}

func (s *Service) DeleteSession(ctx context.Context, teacherID, sessionID int64) error {
	if _, err := s.ownedSession(ctx, teacherID, sessionID); err != nil {
		return err
	}
	return s.repo.DeleteSession(ctx, sessionID)
}

func (s *Service) ListParticipants(ctx context.Context, teacherID, sessionID int64) ([]Participant, error) {
	if _, err := s.ownedSession(ctx, teacherID, sessionID); err != nil {
		return nil, err
	}
	return s.repo.ListParticipants(ctx, sessionID)
}

// TeacherSessionView returns the session with its exam's problems.
func (s *Service) TeacherSessionView(ctx context.Context, teacherID, sessionID int64) (SessionView, error) {
	ses, err := s.ownedSession(ctx, teacherID, sessionID)
	if err != nil {
		return SessionView{}, err
	}
	problems, err := s.repo.ListProblems(ctx, ses.ExamID)
	if err != nil {
		return SessionView{}, err
	}
	now := s.now()
	return SessionView{Session: ses, ServerTime: now, State: ses.State(now), Problems: problems}, nil
}

// ---- students ----

// StudentSessions lists every session (all students see all sessions,
// BACKEND_CONTEXT.md §17) with the caller's participation.
func (s *Service) StudentSessions(ctx context.Context, studentID int64) ([]StudentSession, error) {
	sessions, err := s.repo.ListAllSessions(ctx)
	if err != nil {
		return nil, err
	}
	parts, err := s.repo.ListParticipationsByStudent(ctx, studentID)
	if err != nil {
		return nil, err
	}
	bySession := make(map[int64]Participant, len(parts))
	for _, p := range parts {
		bySession[p.SessionID] = p
	}
	out := make([]StudentSession, 0, len(sessions))
	for _, ses := range sessions {
		item := StudentSession{Session: ses}
		if p, ok := bySession[ses.ID]; ok {
			item.Participation = &p
		}
		out = append(out, item)
	}
	return out, nil
}

// StudentSessionView hides the problem list until starts_at.
func (s *Service) StudentSessionView(ctx context.Context, studentID, sessionID int64) (SessionView, error) {
	ses, err := s.repo.GetSession(ctx, sessionID)
	if err != nil {
		return SessionView{}, err
	}
	now := s.now()
	view := SessionView{Session: ses, ServerTime: now, State: ses.State(now)}
	p, err := s.repo.GetParticipant(ctx, sessionID, studentID)
	switch {
	case err == nil:
		view.Participation = &p
	case !errors.Is(err, apperr.ErrNotFound):
		return SessionView{}, err
	}
	if view.State == StateUpcoming {
		view.ProblemsHidden = true
		return view, nil
	}
	if view.Problems, err = s.repo.ListProblems(ctx, ses.ExamID); err != nil {
		return SessionView{}, err
	}
	return view, nil
}

// Join creates the student's participation while the session is running.
// Re-joining returns the existing row in any state, so a student who lost
// their connection continues where they were (BACKEND_CONTEXT.md §5).
func (s *Service) Join(ctx context.Context, studentID, sessionID int64) (Participant, error) {
	ses, err := s.repo.GetSession(ctx, sessionID)
	if err != nil {
		return Participant{}, err
	}
	p, err := s.repo.GetParticipant(ctx, sessionID, studentID)
	if err == nil {
		return p, nil
	}
	if !errors.Is(err, apperr.ErrNotFound) {
		return Participant{}, err
	}
	now := s.now()
	switch ses.State(now) {
	case StateUpcoming:
		return Participant{}, apperr.Conflict("session has not started")
	case StateEnded:
		return Participant{}, apperr.Conflict("session has ended")
	}
	return s.repo.CreateParticipant(ctx, sessionID, studentID, now)
}

// Finish ends the student's attempt; it is idempotent and cannot be undone
// through the student API.
func (s *Service) Finish(ctx context.Context, studentID, sessionID int64) (Participant, error) {
	if _, err := s.repo.GetSession(ctx, sessionID); err != nil {
		return Participant{}, err
	}
	p, err := s.repo.FinishParticipant(ctx, sessionID, studentID, s.now())
	if errors.Is(err, apperr.ErrNotFound) {
		return Participant{}, apperr.Conflict("you have not joined this session")
	}
	return p, err
}
