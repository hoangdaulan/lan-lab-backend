package auth

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/hoangdaulan/lan-lab-backend/internal/httpx"
)

// TokenVerifier is satisfied by *Service.
type TokenVerifier interface {
	VerifyAccessToken(token string) (Principal, error)
}

type principalKey struct{}

// PrincipalFrom returns the caller set by Authenticate.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// WithPrincipal stores p in ctx (used by Authenticate and tests).
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// Authenticate requires a valid "Authorization: Bearer <jwt>" header.
func Authenticate(v TokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
			if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
				httpx.Error(w, http.StatusUnauthorized, "missing bearer token")
				return
			}
			p, err := v.VerifyAccessToken(token)
			if err != nil {
				httpx.Error(w, http.StatusUnauthorized, "invalid or expired access token")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
		})
	}
}

// RequireRole allows only the given roles; it must run after Authenticate.
func RequireRole(roles ...Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := PrincipalFrom(r.Context())
			if !ok {
				httpx.Error(w, http.StatusUnauthorized, "not authenticated")
				return
			}
			if !slices.Contains(roles, p.Role) {
				httpx.Error(w, http.StatusForbidden, "forbidden for role "+string(p.Role))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
