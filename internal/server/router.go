package server

import (
	"fmt"
	"io/fs"
	"net/http"
)

// cacheControl은 핸들러를 감싸 정적 자산에 Cache-Control 헤더를 설정한다.
func cacheControl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
		next.ServeHTTP(w, r)
	})
}

// NewRouter는 정적 파일 서빙을 포함한 최상위 HTTP 멀티플렉서를 생성한다.
func NewRouter(staticFS fs.FS) (*http.ServeMux, error) {
	mux := http.NewServeMux()

	staticSub, err := fs.Sub(staticFS, "web/static")
	if err != nil {
		return nil, fmt.Errorf("failed to create static sub-fs: %w", err)
	}
	mux.Handle("GET /static/", cacheControl(http.StripPrefix("/static/", http.FileServer(http.FS(staticSub)))))

	return mux, nil
}
