package auth

import (
	"errors"
	"log/slog"
	"net/http"
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

// Register adds the auth routes to the /api/v1 router.
func (h *Handler) Register(r chi.Router) {
	r.Post("/auth/login", h.login)
	r.Post("/auth/refresh", h.refresh)
	r.Post("/auth/logout", h.logout)

	r.Group(func(r chi.Router) {
		r.Use(Authenticate(h.svc))
		r.Get("/me", h.me)
		r.Put("/me/password", h.changePassword)
	})
}

type userResponse struct {
	ID       int64  `json:"id"`
	Role     Role   `json:"role"`
	Code     string `json:"code"`
	Fullname string `json:"fullname"`
}

type tokenResponse struct {
	AccessToken      string       `json:"access_token"`
	AccessExpiresAt  time.Time    `json:"access_expires_at"`
	RefreshToken     string       `json:"refresh_token"`
	RefreshExpiresAt time.Time    `json:"refresh_expires_at"`
	User             userResponse `json:"user"`
}

func toUser(a Account) userResponse {
	return userResponse{ID: a.ID, Role: a.Role, Code: a.Code, Fullname: a.Fullname}
}

func toTokenResponse(t Tokens, a Account) tokenResponse {
	return tokenResponse{
		AccessToken:      t.AccessToken,
		AccessExpiresAt:  t.AccessExpiresAt.UTC(),
		RefreshToken:     t.RefreshToken,
		RefreshExpiresAt: t.RefreshExpiresAt.UTC(),
		User:             toUser(a),
	}
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Role     Role   `json:"role"`
		Code     string `json:"code"`
		Password string `json:"password"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	tokens, acct, err := h.svc.Login(r.Context(), req.Role, req.Code, req.Password)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toTokenResponse(tokens, acct))
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	tokens, acct, err := h.svc.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toTokenResponse(tokens, acct))
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.Logout(r.Context(), req.RefreshToken); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	acct, err := h.svc.Me(r.Context(), p)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toUser(acct))
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	p, _ := PrincipalFrom(r.Context())
	if err := h.svc.ChangePassword(r.Context(), p, req.CurrentPassword, req.NewPassword); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		httpx.Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrInvalidCredentials):
		httpx.Error(w, http.StatusUnauthorized, "invalid code, password or role")
	case errors.Is(err, ErrInvalidRefreshToken):
		httpx.Error(w, http.StatusUnauthorized, "invalid or expired refresh token")
	case errors.Is(err, ErrNotFound):
		// The account behind a still-valid access token no longer exists.
		httpx.Error(w, http.StatusUnauthorized, "account not found")
	default:
		h.log.Error("auth request failed", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
	}
}
