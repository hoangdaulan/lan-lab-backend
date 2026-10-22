package submission

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/hoangdaulan/lan-lab-backend/internal/apperr"
)

// Repository is the persistence boundary for submissions. Lookups return
// apperr.ErrNotFound for missing rows.
type Repository interface {
	// Create accepts a submission in one transaction, locking problem (SHARE)
	// -> exam_session (SHARE) -> exam_participant (FOR UPDATE), then checks:
	// problem belongs to the session's exam (else NotFound), the student
	// joined (else Conflict), status in_progress (else Conflict),
	// starts_at <= received_at < ends_at (else Conflict), at least one test
	// (else Conflict). It inserts submission + queued run at the current
	// problem version + queued job, and returns the new submission.
	Create(ctx context.Context, n NewSubmission) (Submission, error)
	Get(ctx context.Context, id int64) (Submission, error)
	// List returns a session's submissions, newest first.
	List(ctx context.Context, sessionID int64, f Filter) ([]Submission, error)
	ListTestResults(ctx context.Context, runID int64) ([]TestResult, error)
	// SessionOwner returns the teacher owning the session's exam.
	SessionOwner(ctx context.Context, sessionID int64) (int64, error)
}

type Service struct {
	repo    Repository
	storage Storage
	now     func() time.Time
	log     *slog.Logger
}

func NewService(repo Repository, storage Storage, now func() time.Time, log *slog.Logger) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{repo: repo, storage: storage, now: now, log: log}
}

// ValidateUpload checks the client file name and size (BACKEND_CONTEXT.md §19).
func ValidateUpload(filename string, size int) error {
	switch {
	case !strings.EqualFold(pathExt(filename), ".cpp"):
		return apperr.Invalid("only .cpp files are accepted")
	case size == 0:
		return apperr.Invalid("the file is empty")
	case size > MaxSourceBytes:
		return apperr.TooLarge("the file is larger than %d KB", MaxSourceBytes>>10)
	}
	return nil
}

func pathExt(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i:]
	}
	return ""
}

// Submit stores the file, then records submission + run + job. receivedAt
// is taken when the whole file has been read (the caller passes it), never
// from the client and never after waiting for locks.
//
// The file is written before the transaction so an acknowledged submission
// always has its source. If the transaction is rejected by a business rule
// the file is removed at once; on any other error it is left for the
// orphan cleanup, because the commit outcome may be unknown.
func (s *Service) Submit(ctx context.Context, studentID, sessionID, problemID int64, filename string, content []byte, receivedAt time.Time) (Submission, error) {
	if err := ValidateUpload(filename, len(content)); err != nil {
		return Submission{}, err
	}
	rel, err := s.storage.Save(content, receivedAt)
	if err != nil {
		return Submission{}, err
	}
	sub, err := s.repo.Create(ctx, NewSubmission{
		StudentID:  studentID,
		SessionID:  sessionID,
		ProblemID:  problemID,
		SourcePath: rel,
		ReceivedAt: receivedAt,
	})
	if err != nil {
		if isBusinessError(err) {
			if rmErr := s.storage.Remove(rel); rmErr != nil {
				s.log.Warn("remove rejected source", "path", rel, "err", rmErr)
			}
		} else {
			s.log.Error("submission not recorded; source left for orphan cleanup", "path", rel, "err", err)
		}
		return Submission{}, err
	}
	return sub, nil
}

func isBusinessError(err error) bool {
	return errors.Is(err, apperr.ErrNotFound) || errors.Is(err, apperr.ErrConflict) ||
		errors.Is(err, apperr.ErrInvalidInput) || errors.Is(err, apperr.ErrForbidden)
}

// Viewer is who asks: students see only their own submissions, teachers
// only those of exams they own. Others get NotFound.
type Viewer struct {
	ID        int64
	IsTeacher bool
}

func (v Viewer) canSee(sub Submission) bool {
	if v.IsTeacher {
		return sub.TeacherID == v.ID
	}
	return sub.StudentID == v.ID
}

func (s *Service) Get(ctx context.Context, v Viewer, id int64) (Submission, error) {
	sub, err := s.repo.Get(ctx, id)
	if err != nil {
		return Submission{}, err
	}
	if !v.canSee(sub) {
		return Submission{}, apperr.NotFound("submission")
	}
	return sub, nil
}

func (s *Service) Source(ctx context.Context, v Viewer, id int64) (Submission, []byte, error) {
	sub, err := s.Get(ctx, v, id)
	if err != nil {
		return Submission{}, nil, err
	}
	content, err := s.storage.Read(sub.SourcePath)
	if err != nil {
		return Submission{}, nil, err
	}
	return sub, content, nil
}

// List returns a session's submissions: a student's own, or any for the
// owning teacher (optionally filtered).
func (s *Service) List(ctx context.Context, v Viewer, sessionID int64, f Filter) ([]Submission, error) {
	if v.IsTeacher {
		owner, err := s.repo.SessionOwner(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		if owner != v.ID {
			return nil, apperr.NotFound("session")
		}
	} else {
		f.StudentID = v.ID
	}
	return s.repo.List(ctx, sessionID, f)
}

// TestResults returns per-test results of the current run (teachers only,
// for reviews).
func (s *Service) TestResults(ctx context.Context, teacherID, id int64) (Submission, []TestResult, error) {
	sub, err := s.Get(ctx, Viewer{ID: teacherID, IsTeacher: true}, id)
	if err != nil {
		return Submission{}, nil, err
	}
	if sub.Result == nil {
		return sub, []TestResult{}, nil
	}
	results, err := s.repo.ListTestResults(ctx, sub.Result.RunID)
	return sub, results, err
}
