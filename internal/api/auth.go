package api

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/chals-go/valkey-sentinel-manager/internal/store"
)

// TokenAuth는 Bearer 토큰을 검증하는 HTTP 미들웨어를 반환한다.
// 런타임 설정에 JSON 형식으로 저장된 다중 명명 토큰을 지원하며,
// 유효한 토큰이 없으면 401 Unauthorized를 반환한다.
func TokenAuth(s store.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check multi-token from runtime settings first.
			validTokens := getValidTokens(s, r)
			if len(validTokens) == 0 {
				// No tokens configured — deny access.
				writeError(w, http.StatusUnauthorized, "No API tokens configured. Create a token in Settings > API Token.")
				return
			}

			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				writeError(w, http.StatusUnauthorized, "Authorization header required")
				return
			}
			reqToken := auth[7:]
			for _, t := range validTokens {
				if subtle.ConstantTimeCompare([]byte(reqToken), []byte(t)) == 1 {
					next.ServeHTTP(w, r)
					return
				}
			}
			writeError(w, http.StatusUnauthorized, "Invalid API token")
		})
	}
}

func getValidTokens(s store.Store, r *http.Request) []string {
	// Multi-token from runtime settings.
	rt, _ := s.GetRuntimeSettings(r.Context())
	if raw, ok := rt["api_tokens"]; ok && raw != "" {
		var tokens map[string]string
		if json.Unmarshal([]byte(raw), &tokens) == nil && len(tokens) > 0 {
			result := make([]string, 0, len(tokens))
			for _, v := range tokens {
				result = append(result, v)
			}
			return result
		}
	}
	// Fallback: single token.
	if token, err := s.GetAPIToken(r.Context()); err == nil && token != "" {
		return []string{token}
	}
	return nil
}
