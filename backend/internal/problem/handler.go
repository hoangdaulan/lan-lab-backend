package problem

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hoangdaulan/lan-lab-backend/internal/auth"
	"github.com/hoangdaulan/lan-lab-backend/internal/httpx"
)

type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// RegisterTeacher adds teacher-only routes (caller applies RequireRole).
func (h *Handler) RegisterTeacher(r chi.Router) {
	r.Get("/exams/{id}/problems", h.list)
	r.Post("/exams/{id}/problems", h.create)
	r.Get("/problems/{id}", h.get)
	r.Put("/problems/{id}", h.update)
	r.Delete("/problems/{id}", h.delete)
	r.Get("/problems/{id}/tests", h.listTests)
	r.Post("/problems/{id}/rejudge", h.rejudge)
}

// RegisterStudent adds student-only routes.
func (h *Handler) RegisterStudent(r chi.Router) {
	// {id} (not {sid}) so it shares the /sessions/{id} tree node in chi.
	r.Get("/sessions/{id}/problems/{pid}", h.studentGet)
}

type problemResponse struct {
	ID            int64     `json:"id"`
	ExamID        int64     `json:"exam_id"`
	Code          string    `json:"code"`
	Title         string    `json:"title"`
	Statement     string    `json:"statement"`
	TimeLimitMs   int       `json:"time_limit_ms"`
	MemoryLimitMb int       `json:"memory_limit_mb"`
	Version       int       `json:"version"`
	TestDir       string    `json:"test_dir"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func toResponse(p Problem) problemResponse {
	return problemResponse{ID: p.ID, ExamID: p.ExamID, Code: p.Code, Title: p.Title,
		Statement: p.Statement, TimeLimitMs: p.TimeLimitMs, MemoryLimitMb: p.MemoryLimitMb,
		Version: p.Version, TestDir: TestDir(p.ID), CreatedAt: p.CreatedAt.UTC(), UpdatedAt: p.UpdatedAt.UTC()}
}

type problemRequest struct {
	Code          string `json:"code"`
	Title         string `json:"title"`
	Statement     string `json:"statement"`
	TimeLimitMs   int    `json:"time_limit_ms"`
	MemoryLimitMb int    `json:"memory_limit_mb"`
}

func (q problemRequest) input() Input { return Input(q) }

func teacherID(r *http.Request) int64 {
	p, _ := auth.PrincipalFrom(r.Context())
	return p.ID
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	examID, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	ps, err := h.svc.List(r.Context(), teacherID(r), examID)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	items := make([]problemResponse, 0, len(ps))
	for _, p := range ps {
		items = append(items, toResponse(p))
	}
	httpx.WritePagedList(w, r, items, nil)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	examID, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	var req problemRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := h.svc.Create(r.Context(), teacherID(r), examID, req.input())
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, toResponse(p))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	p, err := h.svc.Owned(r.Context(), teacherID(r), id)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toResponse(p))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	var req problemRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := h.svc.Update(r.Context(), teacherID(r), id, req.input())
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"problem":              toResponse(res.Problem),
		"rejudge_queued":       res.Bumped,
		"requeued_submissions": res.Requeued,
	})
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	if err := h.svc.Delete(r.Context(), teacherID(r), id); err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type testCaseResponse struct {
	ID     int64  `json:"id"`
	Input  string `json:"input"`
	Output string `json:"output"`
}

// diskResponse tells the teacher whether the files on disk match the
// registered tests (they only take effect after POST /problems/{id}/rejudge).
type diskResponse struct {
	Tests  int    `json:"tests"`
	Error  string `json:"error,omitempty"`
	InSync bool   `json:"in_sync"`
}

func (h *Handler) listTests(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	tests, err := h.svc.ListTests(r.Context(), teacherID(r), id)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	disk, err := h.svc.DiskState(tests, id)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	items := make([]testCaseResponse, 0, len(tests))
	for _, t := range tests {
		items = append(items, testCaseResponse{ID: t.ID, Input: t.InputPath, Output: t.OutputPath})
	}
	httpx.WritePagedList(w, r, items, map[string]any{
		"test_dir": TestDir(id),
		"disk":     diskResponse{Tests: disk.Count, Error: disk.Error, InSync: disk.InSync},
	})
}

type testFileResponse struct {
	Name       string `json:"name"`
	Input      string `json:"input"`
	Output     string `json:"output"`
	InputSize  int64  `json:"input_size"`
	OutputSize int64  `json:"output_size"`
}

func (h *Handler) rejudge(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	p, tests, requeued, err := h.svc.Rejudge(r.Context(), teacherID(r), id)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	items := make([]testFileResponse, 0, len(tests))
	for _, t := range tests {
		items = append(items, testFileResponse{Name: t.Name, Input: t.InputPath, Output: t.OutputPath,
			InputSize: t.InputSize, OutputSize: t.OutputSize})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"problem":              toResponse(p),
		"tests":                items,
		"rejudge_queued":       true,
		"requeued_submissions": requeued,
	})
}

func (h *Handler) studentGet(w http.ResponseWriter, r *http.Request) {
	sid, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	pid, err := httpx.IDParam(r, "pid")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	student, _ := auth.PrincipalFrom(r.Context())
	p, err := h.svc.ForStudent(r.Context(), student.ID, sid, pid)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"id":              p.ID,
		"code":            p.Code,
		"title":           p.Title,
		"statement":       p.Statement,
		"time_limit_ms":   p.TimeLimitMs,
		"memory_limit_mb": p.MemoryLimitMb,
	})
}
