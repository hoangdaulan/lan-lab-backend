package problem

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangdaulan/lan-lab-backend/internal/apperr"
)

type fakeRepo struct {
	mu          sync.Mutex
	examOwner   map[int64]int64
	problems    map[int64]*Problem
	tests       map[int64][]TestCase
	sessions    map[int64]SessionWindow
	participant map[[2]int64]bool
	requeued    []int // versions requeued
	submissions int   // submissions of the problem (requeue count)
	nextID      int64
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		examOwner:   map[int64]int64{},
		problems:    map[int64]*Problem{},
		tests:       map[int64][]TestCase{},
		sessions:    map[int64]SessionWindow{},
		participant: map[[2]int64]bool{},
	}
}

func (f *fakeRepo) ExamOwner(_ context.Context, examID int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if o, ok := f.examOwner[examID]; ok {
		return o, nil
	}
	return 0, apperr.NotFound("exam")
}

func (f *fakeRepo) Create(_ context.Context, examID int64, in Input) (Problem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.problems {
		if p.ExamID == examID && p.Code == in.Code {
			return Problem{}, apperr.Conflict("dup")
		}
	}
	f.nextID++
	p := &Problem{ID: f.nextID, ExamID: examID, TeacherID: f.examOwner[examID], Code: in.Code, Title: in.Title,
		Statement: in.Statement, TimeLimitMs: in.TimeLimitMs, MemoryLimitMb: in.MemoryLimitMb, Version: 1}
	f.problems[p.ID] = p
	return *p, nil
}

func (f *fakeRepo) ListByExam(_ context.Context, examID int64) ([]Problem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Problem
	for _, p := range f.problems {
		if p.ExamID == examID {
			out = append(out, *p)
		}
	}
	return out, nil
}

func (f *fakeRepo) Get(_ context.Context, id int64) (Problem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.problems[id]; ok {
		return *p, nil
	}
	return Problem{}, apperr.NotFound("problem")
}

func (f *fakeRepo) Update(_ context.Context, id int64, in Input) (Problem, int, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.problems[id]
	bumped := LimitsChanged(*p, in)
	p.Code, p.Title, p.Statement, p.TimeLimitMs, p.MemoryLimitMb = in.Code, in.Title, in.Statement, in.TimeLimitMs, in.MemoryLimitMb
	n := 0
	if bumped {
		p.Version++
		f.requeued = append(f.requeued, p.Version)
		n = f.submissions
	}
	return *p, n, bumped, nil
}

func (f *fakeRepo) Delete(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.problems, id)
	return nil
}

func (f *fakeRepo) ListTests(_ context.Context, id int64) ([]TestCase, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tests[id], nil
}

func (f *fakeRepo) ReplaceTests(_ context.Context, id int64, tests []TestFile) (Problem, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var rows []TestCase
	for i, t := range tests {
		rows = append(rows, TestCase{ID: int64(i + 1), InputPath: t.InputPath, OutputPath: t.OutputPath})
	}
	f.tests[id] = rows
	p := f.problems[id]
	p.Version++
	f.requeued = append(f.requeued, p.Version)
	return *p, f.submissions, nil
}

func (f *fakeRepo) SessionWindow(_ context.Context, id int64) (SessionWindow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if w, ok := f.sessions[id]; ok {
		return w, nil
	}
	return SessionWindow{}, apperr.NotFound("session")
}

func (f *fakeRepo) IsParticipant(_ context.Context, sessionID, studentID int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.participant[[2]int64{sessionID, studentID}], nil
}

const (
	teacherA = int64(100)
	teacherB = int64(200)
	examID   = int64(10)
)

var (
	t0      = time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	discard = slog.New(slog.NewTextHandler(io.Discard, nil))
)

func newTestService(t *testing.T) (*Service, *fakeRepo, string, *time.Time) {
	t.Helper()
	repo := newFakeRepo()
	repo.examOwner[examID] = teacherA
	dataDir := t.TempDir()
	now := t0
	svc := NewService(repo, dataDir, func() time.Time { return now }, discard)
	return svc, repo, dataDir, &now
}

func validInput() Input {
	return Input{Code: " a ", Title: "Sum", Statement: "a+b", TimeLimitMs: 1000, MemoryLimitMb: 256}
}

func TestCreateNormalizesAndMakesTestDir(t *testing.T) {
	svc, _, dataDir, _ := newTestService(t)
	p, err := svc.Create(context.Background(), teacherA, examID, validInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p.Code != "A" || p.Version != 1 {
		t.Errorf("problem = %+v", p)
	}
	if fi, err := os.Stat(filepath.Join(dataDir, TestDir(p.ID))); err != nil || !fi.IsDir() {
		t.Errorf("test dir not created: %v", err)
	}
}

func TestCreateValidationAndOwnership(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	ctx := context.Background()
	bad := []func(*Input){
		func(in *Input) { in.Code = "" },
		func(in *Input) { in.Code = "A B" },
		func(in *Input) { in.Title = " " },
		func(in *Input) { in.TimeLimitMs = 0 },
		func(in *Input) { in.MemoryLimitMb = MaxMemoryLimitMb + 1 },
	}
	for i, mutate := range bad {
		in := validInput()
		mutate(&in)
		if _, err := svc.Create(ctx, teacherA, examID, in); !errors.Is(err, apperr.ErrInvalidInput) {
			t.Errorf("case %d: err = %v, want ErrInvalidInput", i, err)
		}
	}
	if _, err := svc.Create(ctx, teacherB, examID, validInput()); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("other teacher err = %v, want ErrNotFound", err)
	}
}

func TestUpdateBumpsVersionOnlyWhenLimitsChange(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	ctx := context.Background()
	p, _ := svc.Create(ctx, teacherA, examID, validInput())

	in := validInput()
	in.Title, in.Statement = "Sum of two", "new text"
	repo.submissions = 4
	res, err := svc.Update(ctx, teacherA, p.ID, in)
	if err != nil || res.Bumped || res.Problem.Version != 1 || res.Requeued != 0 {
		t.Fatalf("text-only update = %+v err=%v; want version 1, no bump", res, err)
	}

	in.TimeLimitMs = 2000
	res, err = svc.Update(ctx, teacherA, p.ID, in)
	if err != nil || !res.Bumped || res.Problem.Version != 2 || res.Requeued != 4 {
		t.Fatalf("time limit update = %+v err=%v; want version 2, 4 requeued", res, err)
	}
	in.MemoryLimitMb = 128
	res, _ = svc.Update(ctx, teacherA, p.ID, in)
	if !res.Bumped || res.Problem.Version != 3 {
		t.Fatalf("memory update = %+v; want version 3", res)
	}
	if len(repo.requeued) != 2 {
		t.Errorf("requeued = %v, want 2 entries", repo.requeued)
	}
	if _, err := svc.Update(ctx, teacherB, p.ID, in); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("other teacher err = %v", err)
	}
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestScanTestsOrdersAndValidates(t *testing.T) {
	dataDir := t.TempDir()
	rel := TestDir(7)
	writeFiles(t, filepath.Join(dataDir, rel), map[string]string{
		"10.in": "1", "10.out": "1",
		"2.in": "", "2.out": "", // empty files are valid tests
		"1.in": "3 4", "1.out": "7",
		"sample.in": "x", "sample.out": "x",
		"README.txt": "ignored", ".hidden.in": "ignored",
	})
	tests, err := ScanTests(dataDir, rel)
	if err != nil {
		t.Fatalf("ScanTests: %v", err)
	}
	var names []string
	for _, tf := range tests {
		names = append(names, tf.Name)
	}
	if got := strings.Join(names, ","); got != "1,2,10,sample" {
		t.Errorf("order = %s, want 1,2,10,sample", got)
	}
	if tests[0].InputPath != "problems/7/tests/1.in" || tests[0].InputSize != 3 {
		t.Errorf("first test = %+v", tests[0])
	}
}

func TestScanTestsErrors(t *testing.T) {
	dataDir := t.TempDir()
	if _, err := ScanTests(dataDir, TestDir(1)); !errors.Is(err, apperr.ErrConflict) {
		t.Errorf("missing dir err = %v, want ErrConflict", err)
	}

	writeFiles(t, filepath.Join(dataDir, TestDir(2)), map[string]string{"notes.txt": ""})
	if _, err := ScanTests(dataDir, TestDir(2)); err == nil || !strings.Contains(err.Error(), "no tests") {
		t.Errorf("empty dir err = %v", err)
	}

	writeFiles(t, filepath.Join(dataDir, TestDir(3)), map[string]string{"1.in": "", "2.out": ""})
	_, err := ScanTests(dataDir, TestDir(3))
	if err == nil || !strings.Contains(err.Error(), "1.in has no 1.out") || !strings.Contains(err.Error(), "2.out has no 2.in") {
		t.Errorf("unpaired err = %v", err)
	}

	dir := filepath.Join(dataDir, TestDir(4))
	writeFiles(t, dir, map[string]string{"1.out": "x"})
	if err := os.Symlink("/etc/passwd", filepath.Join(dir, "1.in")); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	if _, err := ScanTests(dataDir, TestDir(4)); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("symlink err = %v", err)
	}
}

func TestRejudgeReplacesTestsAndBumpsVersion(t *testing.T) {
	svc, repo, dataDir, _ := newTestService(t)
	ctx := context.Background()
	p, _ := svc.Create(ctx, teacherA, examID, validInput())

	if _, _, _, err := svc.Rejudge(ctx, teacherA, p.ID); !errors.Is(err, apperr.ErrConflict) {
		t.Fatalf("rejudge with no tests err = %v, want ErrConflict", err)
	}
	if p2, _ := repo.Get(ctx, p.ID); p2.Version != 1 {
		t.Fatalf("version changed on failed rejudge: %d", p2.Version)
	}

	writeFiles(t, filepath.Join(dataDir, TestDir(p.ID)), map[string]string{"1.in": "1 2", "1.out": "3", "2.in": "", "2.out": "0"})
	repo.submissions = 3
	got, tests, requeued, err := svc.Rejudge(ctx, teacherA, p.ID)
	if err != nil {
		t.Fatalf("Rejudge: %v", err)
	}
	if got.Version != 2 || len(tests) != 2 || len(repo.tests[p.ID]) != 2 || requeued != 3 {
		t.Fatalf("version=%d tests=%d rows=%d requeued=%d", got.Version, len(tests), len(repo.tests[p.ID]), requeued)
	}
	if _, _, _, err := svc.Rejudge(ctx, teacherB, p.ID); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("other teacher err = %v", err)
	}
}

func TestForStudentVisibility(t *testing.T) {
	svc, repo, _, now := newTestService(t)
	ctx := context.Background()
	p, _ := svc.Create(ctx, teacherA, examID, validInput())
	const sessionID, otherSession, studentID = 50, 51, 1
	repo.sessions[sessionID] = SessionWindow{ExamID: examID, StartsAt: t0.Add(time.Hour), EndsAt: t0.Add(2 * time.Hour)}
	repo.sessions[otherSession] = SessionWindow{ExamID: 999, StartsAt: t0, EndsAt: t0.Add(time.Hour)}

	if _, err := svc.ForStudent(ctx, studentID, sessionID, p.ID); !errors.Is(err, apperr.ErrForbidden) {
		t.Fatalf("before start err = %v, want ErrForbidden", err)
	}
	*now = t0.Add(time.Hour)
	if _, err := svc.ForStudent(ctx, studentID, sessionID, p.ID); !errors.Is(err, apperr.ErrForbidden) {
		t.Fatalf("not joined err = %v, want ErrForbidden", err)
	}
	repo.participant[[2]int64{sessionID, studentID}] = true
	got, err := svc.ForStudent(ctx, studentID, sessionID, p.ID)
	if err != nil || got.Statement != "a+b" {
		t.Fatalf("joined = %+v, %v", got, err)
	}
	// Still readable after the session ends (history).
	*now = t0.Add(3 * time.Hour)
	if _, err := svc.ForStudent(ctx, studentID, sessionID, p.ID); err != nil {
		t.Fatalf("after end: %v", err)
	}
	if _, err := svc.ForStudent(ctx, studentID, otherSession, p.ID); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("problem of another exam err = %v, want ErrNotFound", err)
	}
}

func TestDiskStateReportsUnscannedFiles(t *testing.T) {
	svc, _, dataDir, _ := newTestService(t)
	ctx := context.Background()
	p, err := svc.Create(ctx, teacherA, examID, validInput())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(dataDir, TestDir(p.ID))
	state := func() DiskTests {
		t.Helper()
		reg, err := svc.ListTests(ctx, teacherA, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		d, err := svc.DiskState(reg, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}

	if d := state(); d.Count != 0 || d.Error == "" || d.InSync {
		t.Errorf("empty dir: %+v", d)
	}
	writeFiles(t, dir, map[string]string{"1.in": "1", "1.out": "1"})
	if d := state(); d.Count != 1 || d.Error != "" || d.InSync {
		t.Errorf("copied, not scanned: %+v", d)
	}
	if _, _, _, err := svc.Rejudge(ctx, teacherA, p.ID); err != nil {
		t.Fatal(err)
	}
	if d := state(); d.Count != 1 || !d.InSync {
		t.Errorf("after scan: %+v", d)
	}
	writeFiles(t, dir, map[string]string{"2.in": "2"})
	if d := state(); d.Error == "" || d.InSync {
		t.Errorf("2.in without 2.out: %+v", d)
	}
	writeFiles(t, dir, map[string]string{"2.out": "2"})
	if d := state(); d.Count != 2 || d.InSync {
		t.Errorf("new pair not scanned: %+v", d)
	}
}
