package student

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hoangdaulan/lan-lab-backend/internal/httpx"
)

type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// Register adds the student routes; the caller applies teacher-only auth.
func (h *Handler) Register(r chi.Router) {
	r.Get("/students", h.list)
	r.Post("/students", h.create)
	r.Post("/students/import", h.importCSV)
	r.Post("/students/{id}/reset-password", h.resetPassword)
}

type studentResponse struct {
	ID        int64     `json:"id"`
	Code      string    `json:"code"`
	Fullname  string    `json:"fullname"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toResponse(s Student) studentResponse {
	return studentResponse{ID: s.ID, Code: s.Code, Fullname: s.Fullname, CreatedAt: s.CreatedAt.UTC(), UpdatedAt: s.UpdatedAt.UTC()}
}

func toResponses(in []Student) []studentResponse {
	out := make([]studentResponse, 0, len(in))
	for _, s := range in {
		out = append(out, toResponse(s))
	}
	return out
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	limit, err1 := queryInt(r, "limit", DefaultPageSize)
	offset, err2 := queryInt(r, "offset", 0)
	if err1 != nil || err2 != nil {
		httpx.Error(w, http.StatusBadRequest, "limit and offset must be integers")
		return
	}
	page, err := h.svc.List(r.Context(), r.URL.Query().Get("q"), limit, offset)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"items":  toResponses(page.Items),
		"total":  page.Total,
		"limit":  page.Limit,
		"offset": page.Offset,
	})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code     string `json:"code"`
		Fullname string `json:"fullname"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	s, err := h.svc.Create(r.Context(), req.Code, req.Fullname)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, toResponse(s))
}

// maxCSVBytes caps the uploaded roster.
const maxCSVBytes = 1 << 20

func (h *Handler) importCSV(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxCSVBytes+64<<10)
	file, _, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "multipart field \"file\" (CSV, max 1 MiB) is required")
		return
	}
	defer file.Close()

	res, err := h.svc.Import(r.Context(), file)
	var ie *ImportError
	if errors.As(err, &ie) {
		httpx.JSON(w, http.StatusBadRequest, map[string]any{
			"error":      "import rejected, nothing was saved: " + ie.Error(),
			"row_errors": ie.Rows,
		})
		return
	}
	if err != nil {
		h.writeError(w, err)
		return
	}
	skipped := res.Skipped
	if skipped == nil {
		skipped = []SkippedRow{}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"created_count": len(res.Created),
		"skipped_count": len(skipped),
		"created":       toResponses(res.Created),
		"skipped":       skipped,
	})
}

func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "invalid student id")
		return
	}
	if err := h.svc.ResetPassword(r.Context(), id); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		httpx.Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotFound):
		httpx.Error(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrAlreadyExists):
		httpx.Error(w, http.StatusConflict, err.Error())
	default:
		h.log.Error("student request failed", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
	}
}

func queryInt(r *http.Request, key string, def int) (int, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def, nil
	}
	return strconv.Atoi(v)
}
