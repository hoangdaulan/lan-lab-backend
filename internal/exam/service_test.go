package exam

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hoangdaulan/lan-lab-backend/internal/apperr"
)

type fakeRepo struct {
	mu       sync.Mutex
	exams    map[int64]*Exam
	sessions map[int64]*Session
	parts    map[[2]int64]*Participant // (session, student)
	problems map[int64][]ProblemSummary
	nextID   int64
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		exams:    map[int64]*Exam{},
		sessions: map[int64]*Session{},
		parts:    map[[2]int64]*Participant{},
		problems: map[int64][]ProblemSummary{},
	}
}

func (f *fakeRepo) id() int64 { f.nextID++; return f.nextID }

func (f *fakeRepo) CreateExam(_ context.Context, teacherID int64, title, desc string) (Exam, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e := &Exam{ID: f.id(), TeacherID: teacherID, Title: title, Description: desc}
	f.exams[e.ID] = e
	return *e, nil
}

func (f *fakeRepo) ListExams(_ context.Context, teacherID int64) ([]Exam, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Exam
	for _, e := range f.exams {
		if e.TeacherID == teacherID {
			out = append(out, *e)
		}
	}
	return out, nil
}

func (f *fakeRepo) GetExam(_ context.Context, id int64) (Exam, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e, ok := f.exams[id]; ok {
		return *e, nil
	}
	return Exam{}, apperr.NotFound("exam")
}

func (f *fakeRepo) UpdateExam(_ context.Context, id int64, title, desc string) (Exam, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.exams[id]
	e.Title, e.Description = title, desc
	return *e, nil
}

func (f *fakeRepo) DeleteExam(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.sessions {
		if s.ExamID == id {
			return apperr.Conflict("has sessions")
		}
	}
	delete(f.exams, id)
	return nil
}

func (f *fakeRepo) CreateSession(_ context.Context, examID int64, name string, starts, ends time.Time) (Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.exams[examID]
	s := &Session{ID: f.id(), ExamID: examID, ExamTitle: e.Title, TeacherID: e.TeacherID, Name: name, StartsAt: starts, EndsAt: ends}
	f.sessions[s.ID] = s
	return *s, nil
}

func (f *fakeRepo) ListSessionsByExam(_ context.Context, examID int64) ([]Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Session
	for _, s := range f.sessions {
		if s.ExamID == examID {
			out = append(out, *s)
		}
	}
	return out, nil
}

func (f *fakeRepo) ListAllSessions(_ context.Context) ([]Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Session
	for _, s := range f.sessions {
		out = append(out, *s)
	}
	return out, nil
}

func (f *fakeRepo) GetSession(_ context.Context, id int64) (Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.sessions[id]; ok {
		return *s, nil
	}
	return Session{}, apperr.NotFound("session")
}

func (f *fakeRepo) UpdateSession(_ context.Context, id int64, name string, starts, ends time.Time) (Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.sessions[id]
	s.Name, s.StartsAt, s.EndsAt = name, starts, ends
	return *s, nil
}

func (f *fakeRepo) DeleteSession(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessions, id)
	return nil
}

func (f *fakeRepo) ListParticipants(_ context.Context, sessionID int64) ([]Participant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Participant
	for k, p := range f.parts {
		if k[0] == sessionID {
			out = append(out, *p)
		}
	}
	return out, nil
}

func (f *fakeRepo) GetParticipant(_ context.Context, sessionID, studentID int64) (Participant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.parts[[2]int64{sessionID, studentID}]; ok {
		return *p, nil
	}
	return Participant{}, apperr.NotFound("participant")
}

func (f *fakeRepo) ListParticipationsByStudent(_ context.Context, studentID int64) ([]Participant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Participant
	for k, p := range f.parts {
		if k[1] == studentID {
			out = append(out, *p)
		}
	}
	return out, nil
}

func (f *fakeRepo) CreateParticipant(_ context.Context, sessionID, studentID int64, joinedAt time.Time) (Participant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := [2]int64{sessionID, studentID}
	if p, ok := f.parts[k]; ok {
		return *p, nil
	}
	p := &Participant{ID: f.id(), SessionID: sessionID, StudentID: studentID, Status: ParticipantInProgress, JoinedAt: joinedAt}
	f.parts[k] = p
	return *p, nil
}

func (f *fakeRepo) FinishParticipant(_ context.Context, sessionID, studentID int64, at time.Time) (Participant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.parts[[2]int64{sessionID, studentID}]
	if !ok {
		return Participant{}, apperr.NotFound("participant")
	}
	if p.Status == ParticipantInProgress {
		p.Status, p.FinishedAt = ParticipantFinished, &at
	}
	return *p, nil
}

func (f *fakeRepo) ListProblems(_ context.Context, examID int64) ([]ProblemSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.problems[examID], nil
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

var t0 = time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)

const (
	teacherA = int64(100)
	teacherB = int64(200)
	student1 = int64(1)
)

// setup creates an exam owned by teacherA and a session [t0+1h, t0+2h).
func setup(t *testing.T) (*Service, *fakeRepo, *clock, Session) {
	t.Helper()
	repo := newFakeRepo()
	c := &clock{t: t0}
	svc := NewService(repo, c.now)
	ctx := context.Background()
	e, err := svc.CreateExam(ctx, teacherA, " Midterm ", "")
	if err != nil {
		t.Fatal(err)
	}
	repo.problems[e.ID] = []ProblemSummary{{ID: 9, Code: "A", Title: "Sum"}}
	s, err := svc.CreateSession(ctx, teacherA, e.ID, "Ca 1", t0.Add(time.Hour), t0.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return svc, repo, c, s
}

func TestCreateExamValidatesTitle(t *testing.T) {
	svc := NewService(newFakeRepo(), nil)
	if _, err := svc.CreateExam(context.Background(), teacherA, "  ", ""); !errors.Is(err, apperr.ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}

func TestOtherTeacherCannotSeeOrChangeExam(t *testing.T) {
	svc, _, _, s := setup(t)
	ctx := context.Background()
	if _, err := svc.OwnedExam(ctx, teacherB, s.ExamID); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("OwnedExam err = %v, want ErrNotFound", err)
	}
	if _, err := svc.UpdateExam(ctx, teacherB, s.ExamID, "x", ""); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("UpdateExam err = %v, want ErrNotFound", err)
	}
	if _, err := svc.CreateSession(ctx, teacherB, s.ExamID, "x", t0, t0.Add(time.Hour)); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("CreateSession err = %v, want ErrNotFound", err)
	}
	if err := svc.DeleteSession(ctx, teacherB, s.ID); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("DeleteSession err = %v, want ErrNotFound", err)
	}
}

func TestSessionWindowValidation(t *testing.T) {
	svc, _, _, s := setup(t)
	ctx := context.Background()
	cases := []struct {
		name         string
		starts, ends time.Time
	}{
		{"missing", time.Time{}, t0},
		{"equal", t0, t0},
		{"reversed", t0.Add(time.Hour), t0},
	}
	for _, c := range cases {
		if _, err := svc.CreateSession(ctx, teacherA, s.ExamID, "x", c.starts, c.ends); !errors.Is(err, apperr.ErrInvalidInput) {
			t.Errorf("%s: err = %v, want ErrInvalidInput", c.name, err)
		}
	}
	if _, err := svc.UpdateSession(ctx, teacherA, s.ID, "x", t0.Add(time.Hour), t0); !errors.Is(err, apperr.ErrInvalidInput) {
		t.Errorf("update reversed err = %v", err)
	}
}

func TestJoinOnlyWhileRunning(t *testing.T) {
	svc, _, c, s := setup(t)
	ctx := context.Background()

	if _, err := svc.Join(ctx, student1, s.ID); !errors.Is(err, apperr.ErrConflict) {
		t.Fatalf("before start err = %v, want ErrConflict", err)
	}
	c.t = s.StartsAt // starts_at is inclusive
	p, err := svc.Join(ctx, student1, s.ID)
	if err != nil {
		t.Fatalf("Join at start: %v", err)
	}
	if p.Status != ParticipantInProgress || !p.JoinedAt.Equal(s.StartsAt) {
		t.Errorf("participant = %+v", p)
	}

	// Re-join returns the same row, even after the end.
	c.t = s.EndsAt.Add(time.Minute)
	again, err := svc.Join(ctx, student1, s.ID)
	if err != nil || again.ID != p.ID {
		t.Fatalf("rejoin = %+v, %v; want same participant", again, err)
	}
	// A new student cannot join once ended (ends_at is exclusive).
	c.t = s.EndsAt
	if _, err := svc.Join(ctx, 2, s.ID); !errors.Is(err, apperr.ErrConflict) {
		t.Fatalf("at end err = %v, want ErrConflict", err)
	}
}

func TestFinishIsIdempotent(t *testing.T) {
	svc, _, c, s := setup(t)
	ctx := context.Background()

	if _, err := svc.Finish(ctx, student1, s.ID); !errors.Is(err, apperr.ErrConflict) {
		t.Fatalf("finish before join err = %v, want ErrConflict", err)
	}
	c.t = s.StartsAt.Add(time.Minute)
	if _, err := svc.Join(ctx, student1, s.ID); err != nil {
		t.Fatal(err)
	}
	c.t = c.t.Add(10 * time.Minute)
	first, err := svc.Finish(ctx, student1, s.ID)
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if first.Status != ParticipantFinished || first.FinishedAt == nil || !first.FinishedAt.Equal(c.t) {
		t.Fatalf("finished participant = %+v", first)
	}
	c.t = c.t.Add(time.Minute)
	second, err := svc.Finish(ctx, student1, s.ID)
	if err != nil || !second.FinishedAt.Equal(*first.FinishedAt) {
		t.Fatalf("second finish = %+v, %v; want unchanged", second, err)
	}
}

func TestStudentViewHidesProblemsBeforeStart(t *testing.T) {
	svc, _, c, s := setup(t)
	ctx := context.Background()

	v, err := svc.StudentSessionView(ctx, student1, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.State != StateUpcoming || !v.ProblemsHidden || len(v.Problems) != 0 {
		t.Fatalf("before start view = %+v", v)
	}
	c.t = s.StartsAt
	v, err = svc.StudentSessionView(ctx, student1, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.State != StateRunning || v.ProblemsHidden || len(v.Problems) != 1 {
		t.Fatalf("running view = %+v", v)
	}
	if v.Participation != nil {
		t.Errorf("participation = %+v, want nil before join", v.Participation)
	}
}

func TestStudentSessionsIncludesParticipation(t *testing.T) {
	svc, _, c, s := setup(t)
	ctx := context.Background()
	c.t = s.StartsAt
	if _, err := svc.Join(ctx, student1, s.ID); err != nil {
		t.Fatal(err)
	}
	list, err := svc.StudentSessions(ctx, student1)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Participation == nil || list[0].Participation.StudentID != student1 {
		t.Fatalf("list = %+v", list)
	}
	other, _ := svc.StudentSessions(ctx, 2)
	if len(other) != 1 || other[0].Participation != nil {
		t.Fatalf("other student list = %+v", other)
	}
}
