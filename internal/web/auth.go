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

// SessionManager handles cookie-based session auth and brute-force protection.
type SessionManager struct {
	store         store.Store
	secureCookie  bool

	mu            sync.Mutex
	sessions      map[string]time.Time                // sessionID -> expiresAt
	loginAttempts map[string]loginAttemptRecord        // IP -> record
}

type loginAttemptRecord struct {
	count    int
	lastFail time.Time
}

// NewSessionManager creates a SessionManager.
func NewSessionManager(s store.Store, secureCookie bool) *SessionManager {
	return &SessionManager{
		store:         s,
		secureCookie:  secureCookie,
		sessions:      make(map[string]time.Time),
		loginAttempts: make(map[string]loginAttemptRecord),
	}
}

// IsLoginLocked checks if an IP is locked out due to too many failures.
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

// RecordLoginFailure records a failed login attempt.
func (sm *SessionManager) RecordLoginFailure(ip string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	rec := sm.loginAttempts[ip]
	rec.count++
	rec.lastFail = time.Now()
	sm.loginAttempts[ip] = rec
}

// ClearLoginFailures clears failed login attempts on success.
func (sm *SessionManager) ClearLoginFailures(ip string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.loginAttempts, ip)
}

// HashPassword hashes a password with a random salt using SHA-256.
func HashPassword(password string) string {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	saltHex := hex.EncodeToString(salt)
	hash := sha256.Sum256([]byte(saltHex + password))
	return saltHex + saltSep + hex.EncodeToString(hash[:])
}

// VerifyHash compares a password against a stored hash.
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

// VerifyPassword checks if the given password matches the stored admin hash.
func (sm *SessionManager) VerifyPassword(ctx interface{ Value(any) any }, password string) bool {
	hash, err := sm.store.GetAdminPasswordHash(context.Background())
	if err != nil || hash == "" {
		return password == defaultPassword
	}
	return VerifyHash(password, hash)
}

// CreateSession creates a new session and returns the session ID.
func (sm *SessionManager) CreateSession() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	id := hex.EncodeToString(b)
	sm.mu.Lock()
	sm.sessions[id] = time.Now().Add(sessionTTL)
	sm.cleanupLocked()
	sm.mu.Unlock()
	return id
}

// ValidateSession checks if the session cookie is valid.
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

// SetSessionCookie sets the session cookie on the response.
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

// ClearSessionCookie removes the session cookie.
func (sm *SessionManager) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

// DestroySession removes a session by ID.
func (sm *SessionManager) DestroySession(r *http.Request) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return
	}
	sm.mu.Lock()
	delete(sm.sessions, cookie.Value)
	sm.mu.Unlock()
}

// RequireAuth is middleware that redirects to login if not authenticated.
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
		}
	}
}

// ChangePassword sets a new admin password.
func (sm *SessionManager) ChangePassword(password string) error {
	hash := HashPassword(password)
	return sm.store.SetAdminPasswordHash(context.Background(), hash)
}

// IsDefaultPassword checks if default password is still in use.
func (sm *SessionManager) IsDefaultPassword() bool {
	hash, err := sm.store.GetAdminPasswordHash(context.Background())
	return err != nil || hash == ""
}

// GenerateAPIToken generates a new API token with smgr_ prefix.
func GenerateAPIToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return fmt.Sprintf("smgr_%s", hex.EncodeToString(b))
}
