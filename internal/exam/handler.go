package exam

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
	r.Get("/exams", h.listExams)
	r.Post("/exams", h.createExam)
	r.Get("/exams/{id}", h.getExam)
	r.Put("/exams/{id}", h.updateExam)
	r.Delete("/exams/{id}", h.deleteExam)
	r.Get("/exams/{id}/sessions", h.listSessions)
	r.Post("/exams/{id}/sessions", h.createSession)
	r.Put("/sessions/{id}", h.updateSession)
	r.Delete("/sessions/{id}", h.deleteSession)
	r.Get("/sessions/{id}/participants", h.listParticipants)
}

// RegisterStudent adds student-only routes.
func (h *Handler) RegisterStudent(r chi.Router) {
	r.Get("/me/sessions", h.mySessions)
	r.Post("/sessions/{id}/join", h.join)
	r.Post("/sessions/{id}/finish", h.finish)
}

// RegisterShared adds routes open to both roles (authenticated).
func (h *Handler) RegisterShared(r chi.Router) {
	r.Get("/sessions/{id}", h.getSession)
}

// ---- responses ----

type examResponse struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func toExam(e Exam) examResponse {
	return examResponse{ID: e.ID, Title: e.Title, Description: e.Description,
		CreatedAt: e.CreatedAt.UTC(), UpdatedAt: e.UpdatedAt.UTC()}
}

type examDetailResponse struct {
	examResponse
	Problems []problemSummaryResponse `json:"problems"`
}

type sessionResponse struct {
	ID        int64     `json:"id"`
	ExamID    int64     `json:"exam_id"`
	ExamTitle string    `json:"exam_title"`
	Name      string    `json:"name"`
	StartsAt  time.Time `json:"starts_at"`
	EndsAt    time.Time `json:"ends_at"`
	State     string    `json:"state"`
}

func toSession(s Session, now time.Time) sessionResponse {
	return sessionResponse{ID: s.ID, ExamID: s.ExamID, ExamTitle: s.ExamTitle, Name: s.Name,
		StartsAt: s.StartsAt.UTC(), EndsAt: s.EndsAt.UTC(), State: string(s.State(now))}
}

type participantResponse struct {
	ID              int64      `json:"id"`
	SessionID       int64      `json:"session_id"`
	StudentID       int64      `json:"student_id"`
	StudentCode     string     `json:"student_code"`
	StudentFullname string     `json:"student_fullname"`
	Status          string     `json:"status"`
	JoinedAt        time.Time  `json:"joined_at"`
	FinishedAt      *time.Time `json:"finished_at"`
}

func toParticipant(p Participant) participantResponse {
	var fin *time.Time
	if p.FinishedAt != nil {
		t := p.FinishedAt.UTC()
		fin = &t
	}
	return participantResponse{ID: p.ID, SessionID: p.SessionID, StudentID: p.StudentID,
		StudentCode: p.StudentCode, StudentFullname: p.StudentFullname, Status: p.Status,
		JoinedAt: p.JoinedAt.UTC(), FinishedAt: fin}
}

func toParticipantPtr(p *Participant) *participantResponse {
	if p == nil {
		return nil
	}
	r := toParticipant(*p)
	return &r
}

type problemSummaryResponse struct {
	ID    int64  `json:"id"`
	Code  string `json:"code"`
	Title string `json:"title"`
}

func mapSlice[T, R any](in []T, f func(T) R) []R {
	out := make([]R, 0, len(in))
	for _, v := range in {
		out = append(out, f(v))
	}
	return out
}

func principal(r *http.Request) auth.Principal {
	p, _ := auth.PrincipalFrom(r.Context())
	return p
}

// ---- exams ----

type examRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

func (h *Handler) listExams(w http.ResponseWriter, r *http.Request) {
	exams, err := h.svc.ListExams(r.Context(), principal(r).ID)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	httpx.WritePagedList(w, r, mapSlice(exams, toExam), nil)
}

func (h *Handler) createExam(w http.ResponseWriter, r *http.Request) {
	var req examRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	e, err := h.svc.CreateExam(r.Context(), principal(r).ID, req.Title, req.Description)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, toExam(e))
}

func (h *Handler) getExam(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	e, err := h.svc.ExamDetail(r.Context(), principal(r).ID, id)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	httpx.JSON(w, http.StatusOK, examDetailResponse{
		examResponse: toExam(e.Exam),
		Problems:     mapSlice(e.Problems, func(p ProblemSummary) problemSummaryResponse { return problemSummaryResponse(p) }),
	})
}

func (h *Handler) updateExam(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	var req examRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	e, err := h.svc.UpdateExam(r.Context(), principal(r).ID, id, req.Title, req.Description)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toExam(e))
}

func (h *Handler) deleteExam(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	if err := h.svc.DeleteExam(r.Context(), principal(r).ID, id); err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- sessions ----

type sessionRequest struct {
	Name     string    `json:"name"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
}

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	examID, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	sessions, err := h.svc.ListSessions(r.Context(), principal(r).ID, examID)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	now := time.Now()
	httpx.WritePagedList(w, r, mapSlice(sessions, func(s Session) sessionResponse { return toSession(s, now) }), nil)
}

func (h *Handler) createSession(w http.ResponseWriter, r *http.Request) {
	examID, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	var req sessionRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	s, err := h.svc.CreateSession(r.Context(), principal(r).ID, examID, req.Name, req.StartsAt, req.EndsAt)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, toSession(s, time.Now()))
}

func (h *Handler) updateSession(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	var req sessionRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	s, err := h.svc.UpdateSession(r.Context(), principal(r).ID, id, req.Name, req.StartsAt, req.EndsAt)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toSession(s, time.Now()))
}

func (h *Handler) deleteSession(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	if err := h.svc.DeleteSession(r.Context(), principal(r).ID, id); err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listParticipants(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	parts, err := h.svc.ListParticipants(r.Context(), principal(r).ID, id)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	httpx.WritePagedList(w, r, mapSlice(parts, toParticipant), nil)
}

func (h *Handler) getSession(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	p := principal(r)
	var view SessionView
	if p.Role == auth.RoleTeacher {
		view, err = h.svc.TeacherSessionView(r.Context(), p.ID, id)
	} else {
		view, err = h.svc.StudentSessionView(r.Context(), p.ID, id)
	}
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	resp := map[string]any{
		"session":         toSession(view.Session, view.ServerTime),
		"server_time":     view.ServerTime.UTC(),
		"problems":        mapSlice(view.Problems, func(p ProblemSummary) problemSummaryResponse { return problemSummaryResponse(p) }),
		"problems_hidden": view.ProblemsHidden,
	}
	if p.Role == auth.RoleStudent {
		resp["participation"] = toParticipantPtr(view.Participation)
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// ---- students ----

func (h *Handler) mySessions(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.StudentSessions(r.Context(), principal(r).ID)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	now := time.Now()
	type item struct {
		sessionResponse
		Participation *participantResponse `json:"participation"`
	}
	httpx.WritePagedList(w, r, mapSlice(items, func(s StudentSession) item {
		return item{sessionResponse: toSession(s.Session, now), Participation: toParticipantPtr(s.Participation)}
	}), map[string]any{"server_time": now.UTC()})
}

func (h *Handler) join(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	p, err := h.svc.Join(r.Context(), principal(r).ID, id)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toParticipant(p))
}

func (h *Handler) finish(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	p, err := h.svc.Finish(r.Context(), principal(r).ID, id)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toParticipant(p))
}
