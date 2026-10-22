package board

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/hoangdaulan/lan-lab-backend/internal/auth"
	"github.com/hoangdaulan/lan-lab-backend/internal/httpx"
)

type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// RegisterTeacher adds teacher-only routes.
func (h *Handler) RegisterTeacher(r chi.Router) {
	r.Get("/sessions/{id}/summary", h.teacherSummary)
	r.Post("/sessions/{id}/announcements", h.announce)
}

// RegisterStudent adds student-only routes.
func (h *Handler) RegisterStudent(r chi.Router) {
	r.Get("/sessions/{id}/summary/me", h.studentSummary)
}

// RegisterShared adds routes for both roles.
func (h *Handler) RegisterShared(r chi.Router) {
	r.Get("/sessions/{id}/announcements", h.listAnnouncements)
}

func (h *Handler) teacherSummary(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	p, _ := auth.PrincipalFrom(r.Context())
	sum, err := h.svc.TeacherSummary(r.Context(), p.ID, id)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	httpx.JSON(w, http.StatusOK, sum)
}

func (h *Handler) studentSummary(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	p, _ := auth.PrincipalFrom(r.Context())
	sum, err := h.svc.StudentSummary(r.Context(), p.ID, id)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	httpx.JSON(w, http.StatusOK, sum)
}

func (h *Handler) announce(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	p, _ := auth.PrincipalFrom(r.Context())
	a, err := h.svc.Announce(r.Context(), p.ID, id, req.Text)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, a)
}

func (h *Handler) listAnnouncements(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.IDParam(r, "id")
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	p, _ := auth.PrincipalFrom(r.Context())
	list, err := h.svc.ListAnnouncements(r.Context(), p.ID, p.Role == auth.RoleTeacher, id)
	if err != nil {
		httpx.WriteError(w, h.log, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": list})
}
