package submission

import "github.com/go-chi/chi/v5"

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Routes returns the submission router. It is mounted in cmd/api once the
// endpoint list is agreed.
func (h *Handler) Routes() chi.Router {
	return chi.NewRouter()
}
