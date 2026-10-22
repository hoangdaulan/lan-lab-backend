package submission

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

type fakeStorage struct {
	mu      sync.Mutex
	files   map[string][]byte
	n       int
	saveErr error
}

func newFakeStorage() *fakeStorage { return &fakeStorage{files: map[string][]byte{}} }

func (f *fakeStorage) Save(content []byte, _ time.Time) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saveErr != nil {
		return "", f.saveErr
	}
	f.n++
	p := "submissions/2026/10/f" + string(rune('0'+f.n)) + ".cpp"
	f.files[p] = content
	return p, nil
}

func (f *fakeStorage) Read(p string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.files[p]
	if !ok {
		return nil, os.ErrNotExist
	}
	return c, nil
}

func (f *fakeStorage) Remove(p string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.files, p)
	return nil
}

type fakeRepo struct {
	createErr error
	created   []NewSubmission
	subs      map[int64]Submission
	owner     map[int64]int64 // session -> teacher
	listed    []Filter
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{subs: map[int64]Submission{}, owner: map[int64]int64{}}
}

func (f *fakeRepo) Create(_ context.Context, n NewSubmission) (Submission, error) {
	if f.createErr != nil {
		return Submission{}, f.createErr
	}
	f.created = append(f.created, n)
	s := Submission{ID: int64(len(f.created)), SessionID: n.SessionID, ProblemID: n.ProblemID,
		StudentID: n.StudentID, SourcePath: n.SourcePath, ReceivedAt: n.ReceivedAt}
	f.subs[s.ID] = s
	return s, nil
}

func (f *fakeRepo) Get(_ context.Context, id int64) (Submission, error) {
	if s, ok := f.subs[id]; ok {
		return s, nil
	}
	return Submission{}, apperr.NotFound("submission")
}

func (f *fakeRepo) List(_ context.Context, _ int64, flt Filter) ([]Submission, error) {
	f.listed = append(f.listed, flt)
	return nil, nil
}

func (f *fakeRepo) ListTestResults(context.Context, int64) ([]TestResult, error) {
	return []TestResult{{TestCaseID: 1, Verdict: "AC"}}, nil
}

func (f *fakeRepo) SessionOwner(_ context.Context, id int64) (int64, error) {
	if o, ok := f.owner[id]; ok {
		return o, nil
	}
	return 0, apperr.NotFound("session")
}

var (
	t0      = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	discard = slog.New(slog.NewTextHandler(io.Discard, nil))
)

func newTestService() (*Service, *fakeRepo, *fakeStorage) {
	repo, st := newFakeRepo(), newFakeStorage()
	return NewService(repo, st, func() time.Time { return t0 }, discard), repo, st
}

func TestValidateUpload(t *testing.T) {
	cases := []struct {
		name string
		size int
		kind error
	}{
		{"a.cpp", 1, nil},
		{"MAIN.CPP", MaxSourceBytes, nil},
		{"a.c", 10, apperr.ErrInvalidInput},
		{"a.cpp.txt", 10, apperr.ErrInvalidInput},
		{"cpp", 10, apperr.ErrInvalidInput},
		{"a.cpp", 0, apperr.ErrInvalidInput},
		{"a.cpp", MaxSourceBytes + 1, apperr.ErrTooLarge},
	}
	for _, c := range cases {
		err := ValidateUpload(c.name, c.size)
		if (c.kind == nil) != (err == nil) || (c.kind != nil && !errors.Is(err, c.kind)) {
			t.Errorf("ValidateUpload(%q, %d) = %v, want %v", c.name, c.size, err, c.kind)
		}
	}
}

func TestSubmitStoresFileThenRecords(t *testing.T) {
	svc, repo, st := newTestService()
	sub, err := svc.Submit(context.Background(), 7, 3, 9, "main.cpp", []byte("int main(){}"), t0)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if len(repo.created) != 1 {
		t.Fatalf("created = %d", len(repo.created))
	}
	n := repo.created[0]
	if n.StudentID != 7 || n.SessionID != 3 || n.ProblemID != 9 || !n.ReceivedAt.Equal(t0) {
		t.Errorf("NewSubmission = %+v", n)
	}
	if got, _ := st.Read(n.SourcePath); string(got) != "int main(){}" || sub.SourcePath != n.SourcePath {
		t.Errorf("stored %q at %q", got, n.SourcePath)
	}
}

func TestSubmitRejectedByRuleRemovesFile(t *testing.T) {
	for _, rule := range []error{apperr.Conflict("the session has ended"), apperr.NotFound("problem")} {
		svc, repo, st := newTestService()
		repo.createErr = rule
		if _, err := svc.Submit(context.Background(), 7, 3, 9, "a.cpp", []byte("x"), t0); !errors.Is(err, rule) {
			t.Fatalf("err = %v, want %v", err, rule)
		}
		if len(st.files) != 0 {
			t.Errorf("%v: files left = %v, want removed", rule, st.files)
		}
	}
}

func TestSubmitDBErrorLeavesOrphanAndFails(t *testing.T) {
	svc, repo, st := newTestService()
	repo.createErr = errors.New("connection reset")
	if _, err := svc.Submit(context.Background(), 7, 3, 9, "a.cpp", []byte("x"), t0); err == nil {
		t.Fatal("expected error")
	}
	// Commit outcome unknown: keep the file for the orphan cleanup.
	if len(st.files) != 1 {
		t.Errorf("files = %d, want 1 orphan", len(st.files))
	}
}

func TestSubmitValidatesBeforeStoring(t *testing.T) {
	svc, repo, st := newTestService()
	if _, err := svc.Submit(context.Background(), 7, 3, 9, "a.py", []byte("x"), t0); !errors.Is(err, apperr.ErrInvalidInput) {
		t.Fatalf("err = %v", err)
	}
	st.saveErr = errors.New("disk full")
	if _, err := svc.Submit(context.Background(), 7, 3, 9, "a.cpp", []byte("x"), t0); err == nil {
		t.Fatal("expected storage error")
	}
	if len(repo.created) != 0 || len(st.files) != 0 {
		t.Errorf("created=%d files=%d, want nothing", len(repo.created), len(st.files))
	}
}

func TestVisibility(t *testing.T) {
	svc, repo, _ := newTestService()
	repo.subs[1] = Submission{ID: 1, StudentID: 7, TeacherID: 100, SourcePath: "submissions/x.cpp"}
	ctx := context.Background()
	for _, v := range []Viewer{{ID: 7}, {ID: 100, IsTeacher: true}} {
		if _, err := svc.Get(ctx, v, 1); err != nil {
			t.Errorf("%+v: %v", v, err)
		}
	}
	for _, v := range []Viewer{{ID: 8}, {ID: 200, IsTeacher: true}, {ID: 100}} {
		if _, err := svc.Get(ctx, v, 1); !errors.Is(err, apperr.ErrNotFound) {
			t.Errorf("%+v: err = %v, want ErrNotFound", v, err)
		}
		if _, _, err := svc.Source(ctx, v, 1); !errors.Is(err, apperr.ErrNotFound) {
			t.Errorf("source %+v: err = %v, want ErrNotFound", v, err)
		}
	}
	if _, _, err := svc.TestResults(ctx, 200, 1); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("other teacher test results err = %v", err)
	}
}

func TestListScopesStudentsToThemselves(t *testing.T) {
	svc, repo, _ := newTestService()
	repo.owner[3] = 100
	ctx := context.Background()
	if _, err := svc.List(ctx, Viewer{ID: 7}, 3, Filter{StudentID: 8, ProblemID: 9}); err != nil {
		t.Fatal(err)
	}
	if got := repo.listed[0]; got.StudentID != 7 || got.ProblemID != 9 {
		t.Errorf("student filter = %+v, want own id", got)
	}
	if _, err := svc.List(ctx, Viewer{ID: 100, IsTeacher: true}, 3, Filter{StudentID: 8}); err != nil {
		t.Fatal(err)
	}
	if got := repo.listed[1]; got.StudentID != 8 {
		t.Errorf("teacher filter = %+v", got)
	}
	if _, err := svc.List(ctx, Viewer{ID: 200, IsTeacher: true}, 3, Filter{}); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("other teacher err = %v", err)
	}
}

func TestFSStorage(t *testing.T) {
	dir := t.TempDir()
	st := NewStorage(dir)
	rel, err := st.Save([]byte("int main(){}"), t0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rel, "submissions/2026/10/") || !strings.HasSuffix(rel, ".cpp") {
		t.Errorf("path = %q", rel)
	}
	if b, err := os.ReadFile(filepath.Join(dir, rel)); err != nil || string(b) != "int main(){}" {
		t.Errorf("file = %q, %v", b, err)
	}
	rel2, _ := st.Save([]byte("x"), t0)
	if rel2 == rel {
		t.Error("two saves produced the same name")
	}
	for _, bad := range []string{"../etc/passwd", "submissions/../../etc/passwd", "/etc/passwd", "problems/1/tests/1.in", "submissions/./a.cpp"} {
		if _, err := st.Read(bad); err == nil {
			t.Errorf("Read(%q) allowed", bad)
		}
	}
	if err := st.Remove(rel); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Read(rel); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("after remove: %v", err)
	}
}
