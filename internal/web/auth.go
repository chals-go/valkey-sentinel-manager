package web

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/chals-go/valkey-sentinel-manager/internal/store"
)

const (
	sessionCookie      = "smgr_session"
	sessionTTL         = 8 * time.Hour
	maxLoginAttempts   = 5
	loginLockoutPeriod = 5 * time.Minute
	defaultUsername    = "admin"
	defaultPassword    = "admin"
	saltSep            = "$"
)

// SessionManager는 쿠키 기반 세션 인증과 브루트포스 방어를 담당하는 구조체다.
type SessionManager struct {
	store         store.Store
	secureCookie  bool

	mu            sync.Mutex
	sessions      map[string]time.Time                // sessionID -> expiresAt
	csrfTokens    map[string]string                   // sessionID -> CSRF token
	loginAttempts map[string]loginAttemptRecord        // IP -> record
}

type loginAttemptRecord struct {
	count    int
	lastFail time.Time
}

// NewSessionManager는 SessionManager를 생성하여 반환한다.
func NewSessionManager(s store.Store, secureCookie bool) *SessionManager {
	return &SessionManager{
		store:         s,
		secureCookie:  secureCookie,
		sessions:      make(map[string]time.Time),
		csrfTokens:    make(map[string]string),
		loginAttempts: make(map[string]loginAttemptRecord),
	}
}

// IsLoginLocked는 주어진 IP가 로그인 실패 횟수 초과로 잠금 상태인지 확인한다.
func (sm *SessionManager) IsLoginLocked(ip string) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	rec, ok := sm.loginAttempts[ip]
	if !ok {
		return false
	}
	if rec.count >= maxLoginAttempts {
		if time.Since(rec.lastFail) < loginLockoutPeriod {
			return true
		}
		delete(sm.loginAttempts, ip)
	}
	return false
}

// RecordLoginFailure는 주어진 IP의 로그인 실패 횟수를 기록한다.
func (sm *SessionManager) RecordLoginFailure(ip string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	rec := sm.loginAttempts[ip]
	rec.count++
	rec.lastFail = time.Now()
	sm.loginAttempts[ip] = rec
}

// ClearLoginFailures는 로그인 성공 시 해당 IP의 실패 기록을 초기화한다.
func (sm *SessionManager) ClearLoginFailures(ip string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.loginAttempts, ip)
}

// HashPassword는 무작위 솔트를 사용하여 SHA-256으로 비밀번호를 해시한다.
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("crypto/rand failed: %w", err)
	}
	saltHex := hex.EncodeToString(salt)
	hash := sha256.Sum256([]byte(saltHex + password))
	return saltHex + saltSep + hex.EncodeToString(hash[:]), nil
}

// VerifyHash는 입력된 비밀번호와 저장된 해시값을 비교하여 일치 여부를 반환한다.
func VerifyHash(password, stored string) bool {
	for i, c := range stored {
		if string(c) == saltSep {
			salt := stored[:i]
			expectedHash := stored[i+1:]
			actual := sha256.Sum256([]byte(salt + password))
			return hmac.Equal([]byte(hex.EncodeToString(actual[:])), []byte(expectedHash))
		}
	}
	// Legacy: plain SHA-256 without salt.
	h := sha256.Sum256([]byte(password))
	return hmac.Equal([]byte(hex.EncodeToString(h[:])), []byte(stored))
}

// VerifyPassword는 입력된 비밀번호가 저장소에 저장된 관리자 비밀번호 해시와 일치하는지 확인한다.
func (sm *SessionManager) VerifyPassword(ctx context.Context, password string) bool {
	hash, err := sm.store.GetAdminPasswordHash(ctx)
	if err != nil || hash == "" {
		return password == defaultPassword
	}
	return VerifyHash(password, hash)
}

// CreateSession은 새 세션을 생성하고 세션 ID를 반환한다.
func (sm *SessionManager) CreateSession() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto/rand failed: %w", err)
	}
	id := hex.EncodeToString(b)
	sm.mu.Lock()
	sm.sessions[id] = time.Now().Add(sessionTTL)
	sm.csrfTokens[id] = generateCSRFToken()
	sm.cleanupLocked()
	sm.mu.Unlock()
	return id, nil
}

// ValidateSession은 요청의 세션 쿠키가 유효한지 확인한다.
func (sm *SessionManager) ValidateSession(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return false
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	exp, ok := sm.sessions[cookie.Value]
	if !ok || time.Now().After(exp) {
		delete(sm.sessions, cookie.Value)
		return false
	}
	return true
}

// SetSessionCookie는 응답에 세션 쿠키를 설정한다.
func (sm *SessionManager) SetSessionCookie(w http.ResponseWriter, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   sm.secureCookie,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

// ClearSessionCookie는 응답에서 세션 쿠키를 제거한다.
func (sm *SessionManager) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

// DestroySession은 요청의 쿠키에서 세션 ID를 읽어 세션을 제거한다.
func (sm *SessionManager) DestroySession(r *http.Request) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return
	}
	sm.mu.Lock()
	delete(sm.sessions, cookie.Value)
	delete(sm.csrfTokens, cookie.Value)
	sm.mu.Unlock()
}

// RequireAuth는 인증되지 않은 요청을 로그인 페이지로 리다이렉트하는 미들웨어다.
func (sm *SessionManager) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !sm.ValidateSession(r) {
			http.Redirect(w, r, "/admin/login", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (sm *SessionManager) cleanupLocked() {
	now := time.Now()
	for id, exp := range sm.sessions {
		if now.After(exp) {
			delete(sm.sessions, id)
			delete(sm.csrfTokens, id)
		}
	}
}

// getOrCreateCSRFToken은 세션에 연결된 CSRF 토큰을 반환하거나, 없으면 새로 생성한다.
func (sm *SessionManager) getOrCreateCSRFToken(sessionID string) string {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if token, ok := sm.csrfTokens[sessionID]; ok {
		return token
	}
	token := generateCSRFToken()
	sm.csrfTokens[sessionID] = token
	return token
}

// ChangePassword는 새 관리자 비밀번호를 해시하여 저장소에 저장한다.
func (sm *SessionManager) ChangePassword(password string) error {
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return sm.store.SetAdminPasswordHash(context.Background(), hash)
}

// IsDefaultPassword는 저장소에 비밀번호 해시가 없어 기본 비밀번호를 사용 중인지 확인한다.
func (sm *SessionManager) IsDefaultPassword() bool {
	hash, err := sm.store.GetAdminPasswordHash(context.Background())
	return err != nil || hash == ""
}

// GenerateAPIToken은 smgr_ 접두사가 붙은 새 API 토큰을 생성하여 반환한다.
func GenerateAPIToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto/rand failed: %w", err)
	}
	return fmt.Sprintf("smgr_%s", hex.EncodeToString(b)), nil
}
