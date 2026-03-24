package server

import (
	"io/fs"
	"net/http"
)

// cacheControl wraps a handler and sets Cache-Control headers for static assets.
func cacheControl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// CSS, JS, fonts — cache for 7 days, allow browser revalidation.
		w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
		next.ServeHTTP(w, r)
	})
}

// NewRouter creates the top-level HTTP mux with static file serving.
// API and web handlers are registered separately via the returned mux.
func NewRouter(staticFS fs.FS) *http.ServeMux {
	mux := http.NewServeMux()

	// Serve embedded static files under /static/ with caching.
	staticSub, err := fs.Sub(staticFS, "web/static")
	if err != nil {
		panic("failed to create static sub-fs: " + err.Error())
	}
	mux.Handle("GET /static/", cacheControl(http.StripPrefix("/static/", http.FileServer(http.FS(staticSub)))))

	return mux
}
