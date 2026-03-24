package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

type csrfContextKey struct{}

// generateCSRFToken은 32바이트 랜덤 hex 문자열을 생성하여 CSRF 토큰으로 반환한다.
func generateCSRFToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// csrfTokenFromContext는 요청 컨텍스트에서 CSRF 토큰을 추출한다.
func csrfTokenFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(csrfContextKey{}).(string); ok {
		return v
	}
	return ""
}

// CSRFProtect는 POST 요청에 대해 CSRF 토큰을 검증하는 미들웨어다.
// 로그인 페이지는 세션이 없으므로 검증 대상에서 제외한다.
func (sm *SessionManager) CSRFProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || cookie.Value == "" {
			next.ServeHTTP(w, r)
			return
		}

		sessionID := cookie.Value

		// 세션의 CSRF 토큰을 가져오거나 새로 생성한다.
		token := sm.getOrCreateCSRFToken(sessionID)

		// 토큰을 컨텍스트에 저장한다.
		ctx := context.WithValue(r.Context(), csrfContextKey{}, token)
		r = r.WithContext(ctx)

		// POST 요청은 CSRF 토큰을 검증한다.
		if r.Method == http.MethodPost {
			r.ParseForm()
			formToken := r.FormValue("csrf_token")
			if formToken == "" || formToken != token {
				http.Error(w, "403 Forbidden - CSRF token invalid", http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}
