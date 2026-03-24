package server

import (
	"fmt"
	"io/fs"
	"net/http"
)

// cacheControl wraps a handler and sets Cache-Control headers for static assets.
func cacheControl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
		next.ServeHTTP(w, r)
	})
}

// NewRouter creates the top-level HTTP mux with static file serving.
func NewRouter(staticFS fs.FS) (*http.ServeMux, error) {
	mux := http.NewServeMux()

	staticSub, err := fs.Sub(staticFS, "web/static")
	if err != nil {
		return nil, fmt.Errorf("failed to create static sub-fs: %w", err)
	}
	mux.Handle("GET /static/", cacheControl(http.StripPrefix("/static/", http.FileServer(http.FS(staticSub)))))

	return mux, nil
}
