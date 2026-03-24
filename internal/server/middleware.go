// Package server는 HTTP 서버 생성, 미들웨어, 라우팅 기능을 제공한다.
package server

import (
	"log/slog"
	"net/http"
	"time"
)

// SecurityHeaders는 http.Handler를 감싸 모든 응답에 보안 헤더를 추가한다.
// HTTPS 환경에서 HSTS를 활성화하려면 SecurityHeadersWithOptions를 사용한다.
func SecurityHeaders(next http.Handler) http.Handler {
	return SecurityHeadersWithOptions(next, false)
}

// SecurityHeadersWithOptions는 HTTPS 옵션을 지정할 수 있는 보안 헤더 미들웨어를 생성한다.
func SecurityHeadersWithOptions(next http.Handler, https bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"font-src 'self'; "+
				"img-src 'self' data:; "+
				"connect-src 'self'")
		if https {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// responseWriter는 상태 코드를 캡처하기 위해 http.ResponseWriter를 감싸는 내부 타입이다.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// RequestLogger는 각 HTTP 요청의 메서드, 경로, 상태 코드, 처리 시간을 로깅하는 미들웨어를 반환한다.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)
		slog.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.statusCode,
			"duration", time.Since(start).String(),
			"remote", r.RemoteAddr,
		)
	})
}
