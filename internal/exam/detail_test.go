package exam

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/hoangdaulan/lan-lab-backend/internal/auth"
)

func TestExamDetailIncludesOnlyItsProblemsAndChecksOwner(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, nil)
	e, err := svc.CreateExam(context.Background(), teacherA, "Exam", "Description")
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.CreateExam(context.Background(), teacherB, "Other", "")
	if err != nil {
		t.Fatal(err)
	}
	repo.problems[e.ID] = []ProblemSummary{{ID: 11, Code: "A", Title: "Sum"}, {ID: 12, Code: "B", Title: "Sort"}}
	repo.problems[other.ID] = []ProblemSummary{{ID: 13, Code: "C", Title: "Hidden"}}
	h := NewHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	router := chi.NewRouter()
	router.Get("/exams/{id}", h.getExam)
	request := func(teacher int64, id int64) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/exams/"+strconv.FormatInt(id, 10), nil)
		r = r.WithContext(auth.WithPrincipal(r.Context(), auth.Principal{ID: teacher, Role: auth.RoleTeacher}))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	w := request(teacherA, e.ID)
	var got examDetailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || got.ID != e.ID || got.Title != e.Title || len(got.Problems) != 2 || got.Problems[0].Code != "A" || got.Problems[1].Code != "B" {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if w = request(teacherB, e.ID); w.Code != 404 {
		t.Fatalf("other teacher: status=%d body=%s", w.Code, w.Body.String())
	}
	if w = request(teacherA, 999); w.Code != 404 {
		t.Fatalf("missing exam: status=%d", w.Code)
	}
	repo.problems[e.ID] = nil
	w = request(teacherA, e.ID)
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || got.Problems == nil || len(got.Problems) != 0 {
		t.Fatalf("empty exam: status=%d body=%s", w.Code, w.Body.String())
	}
}
