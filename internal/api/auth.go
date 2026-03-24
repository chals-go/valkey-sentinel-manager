package api

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/chals-go/valkey-sentinel-manager/internal/store"
)

// TokenAuth returns middleware that verifies the Bearer token.
// Supports multiple named tokens stored in runtime settings as JSON.
func TokenAuth(s store.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check multi-token from runtime settings first.
			validTokens := getValidTokens(s, r)
			if len(validTokens) == 0 {
				// No tokens configured — allow access.
				next.ServeHTTP(w, r)
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
