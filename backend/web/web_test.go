package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestMountServesPageAndAssets(t *testing.T) {
	r := chi.NewRouter()
	Mount(r)
	cases := []struct {
		path   string
		status int
		ctype  string
	}{
		{"/", 200, "text/html"},
		{"/assets/app.js", 200, "javascript"},
		{"/assets/style.css", 200, "text/css"},
		{"/assets/fonts/plex-sans-vietnamese.woff2", 200, "font/woff2"},
		{"/assets/favicon.svg", 200, "image/svg+xml"},
		{"/assets/missing.js", 404, ""},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != c.status {
			t.Errorf("%s: status %d, want %d", c.path, rec.Code, c.status)
		}
		if c.ctype != "" && !strings.Contains(rec.Header().Get("Content-Type"), c.ctype) {
			t.Errorf("%s: content type %q, want %q", c.path, rec.Header().Get("Content-Type"), c.ctype)
		}
	}
}
