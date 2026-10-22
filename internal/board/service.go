package board

import (
	"context"
	"time"

	"github.com/hoangdaulan/lan-lab-backend/internal/apperr"
)

// Broadcaster pushes board changes to WebSocket clients (internal/realtime).
type Broadcaster interface {
	Announce(a Announcement)
	// OnlineStudents returns the students with an open connection
	// subscribed to the session.
	OnlineStudents(sessionID int64) map[int64]bool
}

type Service struct {
	repo  *Repository
	ann   *Announcements
	bcast Broadcaster
	now   func() time.Time
}

func NewService(repo *Repository, ann *Announcements) *Service {
	return &Service{repo: repo, ann: ann, now: time.Now}
}

// SetBroadcaster connects the hub (created after the service).
func (s *Service) SetBroadcaster(b Broadcaster) { s.bcast = b }

func (s *Service) Repo() *Repository { return s.repo }

// OwnedSession returns the session if teacherID owns its exam.
func (s *Service) OwnedSession(ctx context.Context, teacherID, sessionID int64) (SessionInfo, error) {
	si, err := s.repo.SessionInfo(ctx, sessionID)
	if err != nil {
		return SessionInfo{}, err
	}
	if si.TeacherID != teacherID {
		return SessionInfo{}, apperr.NotFound("session")
	}
	return si, nil
}

// TeacherSummary is the whole board for the owning teacher.
func (s *Service) TeacherSummary(ctx context.Context, teacherID, sessionID int64) (Summary, error) {
	si, err := s.OwnedSession(ctx, teacherID, sessionID)
	if err != nil {
		return Summary{}, err
	}
	return s.summary(ctx, sessionID, si.ExamID, 0)
}

// SummaryForBroadcast builds the teacher board without a permission check
// (the hub only sends it to the owning teacher).
func (s *Service) SummaryForBroadcast(ctx context.Context, sessionID int64) (Summary, error) {
	si, err := s.repo.SessionInfo(ctx, sessionID)
	if err != nil {
		return Summary{}, err
	}
	return s.summary(ctx, sessionID, si.ExamID, 0)
}

// StudentSummary is the student's own row; other students are never
// included (BACKEND_CONTEXT.md §17).
func (s *Service) StudentSummary(ctx context.Context, studentID, sessionID int64) (Summary, error) {
	si, err := s.repo.SessionInfo(ctx, sessionID)
	if err != nil {
		return Summary{}, err
	}
	joined, err := s.repo.IsParticipant(ctx, sessionID, studentID)
	if err != nil {
		return Summary{}, err
	}
	if !joined {
		return Summary{}, apperr.Forbidden("join the session first")
	}
	sum, err := s.summary(ctx, sessionID, si.ExamID, studentID)
	if err != nil {
		return Summary{}, err
	}
	sum.OnlineCount = 0 // presence is for teachers only
	if s.now().Before(si.StartsAt) {
		sum.Problems = []Problem{} // titles hidden before starts_at
		for i := range sum.Rows {
			sum.Rows[i].Cells = []Cell{}
		}
	}
	return sum, nil
}

func (s *Service) summary(ctx context.Context, sessionID, examID, studentID int64) (Summary, error) {
	sum, err := s.repo.Summary(ctx, sessionID, examID, studentID)
	if err != nil {
		return Summary{}, err
	}
	if s.bcast != nil {
		online := s.bcast.OnlineStudents(sessionID)
		sum.OnlineCount = len(online)
		for i := range sum.Rows {
			sum.Rows[i].Online = online[sum.Rows[i].StudentID]
		}
	}
	return sum, nil
}

// Announce stores a teacher announcement and pushes it to every client
// subscribed to the session.
func (s *Service) Announce(ctx context.Context, teacherID, sessionID int64, text string) (Announcement, error) {
	if _, err := s.OwnedSession(ctx, teacherID, sessionID); err != nil {
		return Announcement{}, err
	}
	a, err := s.ann.Add(sessionID, text, s.now())
	if err != nil {
		return Announcement{}, err
	}
	if s.bcast != nil {
		s.bcast.Announce(a)
	}
	return a, nil
}

// ListAnnouncements: teachers of the session, or students (all students may
// see every session, BACKEND_CONTEXT.md §17).
func (s *Service) ListAnnouncements(ctx context.Context, userID int64, teacher bool, sessionID int64) ([]Announcement, error) {
	si, err := s.repo.SessionInfo(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if teacher && si.TeacherID != userID {
		return nil, apperr.NotFound("session")
	}
	return s.ann.List(sessionID), nil
}
