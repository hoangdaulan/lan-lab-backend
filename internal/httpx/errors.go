package httpx

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/hoangdaulan/lan-lab-backend/internal/apperr"
)

// WriteError maps apperr kinds to status codes; anything else is logged and
// returned as a generic 500 so internals are not leaked.
func WriteError(w http.ResponseWriter, log *slog.Logger, err error) {
	switch {
	case errors.Is(err, apperr.ErrInvalidInput):
		Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, apperr.ErrNotFound):
		Error(w, http.StatusNotFound, err.Error())
	case errors.Is(err, apperr.ErrConflict):
		Error(w, http.StatusConflict, err.Error())
	case errors.Is(err, apperr.ErrForbidden):
		Error(w, http.StatusForbidden, err.Error())
	default:
		log.Error("request failed", "err", err)
		Error(w, http.StatusInternalServerError, "internal error")
	}
}

// IDParam parses a positive int64 URL parameter.
func IDParam(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.Invalid("invalid %s", name)
	}
	return id, nil
}
