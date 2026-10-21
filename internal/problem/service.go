package problem

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/hoangdaulan/lan-lab-backend/internal/apperr"
)

// Repository is the persistence boundary for problems. Writes that change
// what is judged lock the problem row first (lock order problem ->
// exam_session -> exam_participant -> judge_run -> judge_job,
// BACKEND_CONTEXT.md §10). Lookups return apperr.ErrNotFound.
type Repository interface {
	// ExamOwner returns the teacher that owns examID.
	ExamOwner(ctx context.Context, examID int64) (int64, error)
	// Create returns apperr.ErrConflict if the code is taken in the exam.
	Create(ctx context.Context, examID int64, in Input) (Problem, error)
	ListByExam(ctx context.Context, examID int64) ([]Problem, error)
	Get(ctx context.Context, id int64) (Problem, error)
	// Update saves in; if time/memory limits differ from the locked row it
	// bumps version once and requeues judging in the same transaction.
	Update(ctx context.Context, id int64, in Input) (p Problem, versionBumped bool, err error)
	// Delete removes the problem and its test rows, or returns
	// apperr.ErrConflict if it has submissions.
	Delete(ctx context.Context, id int64) error
	ListTests(ctx context.Context, problemID int64) ([]TestCase, error)
	// ReplaceTests swaps test_cases for tests, bumps version once and
	// requeues judging of every submission of the problem, in one transaction.
	ReplaceTests(ctx context.Context, problemID int64, tests []TestFile) (Problem, error)

	SessionWindow(ctx context.Context, sessionID int64) (SessionWindow, error)
	IsParticipant(ctx context.Context, sessionID, studentID int64) (bool, error)
}

type Service struct {
	repo    Repository
	dataDir string
	now     func() time.Time
	log     *slog.Logger
}

func NewService(repo Repository, dataDir string, now func() time.Time, log *slog.Logger) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{repo: repo, dataDir: dataDir, now: now, log: log}
}

func (s *Service) checkExamOwner(ctx context.Context, teacherID, examID int64) error {
	owner, err := s.repo.ExamOwner(ctx, examID)
	if err != nil {
		return err
	}
	if owner != teacherID {
		return apperr.NotFound("exam")
	}
	return nil
}

// Owned returns the problem if teacherID owns its exam.
func (s *Service) Owned(ctx context.Context, teacherID, problemID int64) (Problem, error) {
	p, err := s.repo.Get(ctx, problemID)
	if err != nil {
		return Problem{}, err
	}
	if p.TeacherID != teacherID {
		return Problem{}, apperr.NotFound("problem")
	}
	return p, nil
}

// Create inserts the problem and creates its empty test directory. A failed
// mkdir is logged, not fatal: the teacher can create the directory by hand
// and rejudge reports it as missing.
func (s *Service) Create(ctx context.Context, teacherID, examID int64, in Input) (Problem, error) {
	if err := s.checkExamOwner(ctx, teacherID, examID); err != nil {
		return Problem{}, err
	}
	in, err := in.normalize()
	if err != nil {
		return Problem{}, err
	}
	p, err := s.repo.Create(ctx, examID, in)
	if err != nil {
		return Problem{}, err
	}
	dir := filepath.Join(s.dataDir, filepath.FromSlash(TestDir(p.ID)))
	if err := os.MkdirAll(dir, 0o775); err != nil {
		s.log.Error("create test directory", "problem_id", p.ID, "dir", dir, "err", err)
	}
	return p, nil
}

func (s *Service) List(ctx context.Context, teacherID, examID int64) ([]Problem, error) {
	if err := s.checkExamOwner(ctx, teacherID, examID); err != nil {
		return nil, err
	}
	return s.repo.ListByExam(ctx, examID)
}

func (s *Service) Update(ctx context.Context, teacherID, problemID int64, in Input) (Problem, bool, error) {
	if _, err := s.Owned(ctx, teacherID, problemID); err != nil {
		return Problem{}, false, err
	}
	in, err := in.normalize()
	if err != nil {
		return Problem{}, false, err
	}
	return s.repo.Update(ctx, problemID, in)
}

func (s *Service) Delete(ctx context.Context, teacherID, problemID int64) error {
	if _, err := s.Owned(ctx, teacherID, problemID); err != nil {
		return err
	}
	// Test files on disk are left in place; the teacher removes them.
	return s.repo.Delete(ctx, problemID)
}

func (s *Service) ListTests(ctx context.Context, teacherID, problemID int64) ([]TestCase, error) {
	if _, err := s.Owned(ctx, teacherID, problemID); err != nil {
		return nil, err
	}
	return s.repo.ListTests(ctx, problemID)
}

// Rejudge re-reads the test directory, replaces test_cases, bumps the
// version and requeues every submission of the problem in all sessions.
func (s *Service) Rejudge(ctx context.Context, teacherID, problemID int64) (Problem, []TestFile, error) {
	if _, err := s.Owned(ctx, teacherID, problemID); err != nil {
		return Problem{}, nil, err
	}
	tests, err := ScanTests(s.dataDir, TestDir(problemID))
	if err != nil {
		return Problem{}, nil, err
	}
	p, err := s.repo.ReplaceTests(ctx, problemID, tests)
	if err != nil {
		return Problem{}, nil, err
	}
	return p, tests, nil
}

// ForStudent returns the statement only once the session has started, to a
// student who joined it, for a problem of the session's exam.
func (s *Service) ForStudent(ctx context.Context, studentID, sessionID, problemID int64) (Problem, error) {
	win, err := s.repo.SessionWindow(ctx, sessionID)
	if err != nil {
		return Problem{}, err
	}
	p, err := s.repo.Get(ctx, problemID)
	if err != nil {
		return Problem{}, err
	}
	if p.ExamID != win.ExamID {
		return Problem{}, apperr.NotFound("problem")
	}
	if s.now().Before(win.StartsAt) {
		return Problem{}, apperr.Forbidden("the statement is available from the session start time")
	}
	joined, err := s.repo.IsParticipant(ctx, sessionID, studentID)
	if err != nil {
		return Problem{}, err
	}
	if !joined {
		return Problem{}, apperr.Forbidden("join the session first")
	}
	return p, nil
}
