// Package web serves the built-in browser UI (prebuilt React and local assets,
// no CDN) so the system works on an offline LAN: http://<server>:8080/.
package web

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
)

//go:embed static
var files embed.FS

// Mount registers "/" (the single page) and "/assets/*" on r.
func Mount(r chi.Router) {
	static, err := fs.Sub(files, "static")
	if err != nil {
		panic(err)
	}
	assets := http.FileServer(http.FS(static))
	noCache := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Small files on a LAN: always revalidate so a rebuilt API is
			// picked up without asking 50 students to clear their cache.
			w.Header().Set("Cache-Control", "no-cache")
			h.ServeHTTP(w, r)
		})
	}
	r.Handle("/assets/*", noCache(http.StripPrefix("/assets", assets)))
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, static, "index.html")
	})
}
