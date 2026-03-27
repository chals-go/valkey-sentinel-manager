package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chals-go/valkey-sentinel-manager/internal/store"
)

func newTestSessionManager() *SessionManager {
	s := store.NewMemoryStore(30)
	return NewSessionManager(s, false)
}

func TestSessionManager_CreateAndValidate(t *testing.T) {
	sm := newTestSessionManager()

	sessionID, err := sm.CreateSession()
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sessionID == "" {
		t.Fatal("sessionID should not be empty")
	}

	// Build a request with the session cookie.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "smgr_session", Value: sessionID})

	if !sm.ValidateSession(req) {
		t.Fatal("valid session should pass validation")
	}
}

func TestSessionManager_InvalidSession(t *testing.T) {
	sm := newTestSessionManager()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "smgr_session", Value: "invalid-session-id"})

	if sm.ValidateSession(req) {
		t.Fatal("invalid session should fail validation")
	}

	// No cookie at all.
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	if sm.ValidateSession(req2) {
		t.Fatal("no cookie should fail validation")
	}
}

func TestSessionManager_DestroySession(t *testing.T) {
	sm := newTestSessionManager()

	sessionID, _ := sm.CreateSession()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "smgr_session", Value: sessionID})

	sm.DestroySession(req)

	if sm.ValidateSession(req) {
		t.Fatal("destroyed session should fail validation")
	}
}

func TestSessionManager_VerifyPassword_Default(t *testing.T) {
	sm := newTestSessionManager()
	ctx := context.Background()

	// No hash set → default password "admin" should work.
	if !sm.VerifyPassword(ctx, "admin") {
		t.Fatal("default password 'admin' should verify when no hash is set")
	}
	if sm.VerifyPassword(ctx, "wrong") {
		t.Fatal("wrong password should not verify")
	}
}

func TestSessionManager_VerifyPassword_Custom(t *testing.T) {
	s := store.NewMemoryStore(30)
	sm := NewSessionManager(s, false)
	ctx := context.Background()

	hash, _ := HashPassword("mypassword")
	s.SetAdminPasswordHash(ctx, hash)

	if !sm.VerifyPassword(ctx, "mypassword") {
		t.Fatal("custom password should verify")
	}
	if sm.VerifyPassword(ctx, "admin") {
		t.Fatal("default password should not verify after custom password set")
	}
}

func TestSessionManager_ChangePassword(t *testing.T) {
	s := store.NewMemoryStore(30)
	sm := NewSessionManager(s, false)
	ctx := context.Background()

	if err := sm.ChangePassword("newpassword"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	if !sm.VerifyPassword(ctx, "newpassword") {
		t.Fatal("new password should verify after ChangePassword")
	}
	if sm.VerifyPassword(ctx, "admin") {
		t.Fatal("old default password should not verify")
	}
}

func TestSessionManager_IsDefaultPassword(t *testing.T) {
	sm := newTestSessionManager()

	if !sm.IsDefaultPassword() {
		t.Fatal("should be true when no hash is set")
	}

	sm.ChangePassword("custom")
	if sm.IsDefaultPassword() {
		t.Fatal("should be false after password change")
	}
}

func TestSessionManager_LoginLockout(t *testing.T) {
	sm := newTestSessionManager()

	ip := "192.168.1.100"

	// Should not be locked initially.
	if sm.IsLoginLocked(ip) {
		t.Fatal("should not be locked initially")
	}

	// Record 5 failures.
	for i := 0; i < 5; i++ {
		sm.RecordLoginFailure(ip)
	}

	if !sm.IsLoginLocked(ip) {
		t.Fatal("should be locked after 5 failures")
	}

	// Clear and check.
	sm.ClearLoginFailures(ip)
	if sm.IsLoginLocked(ip) {
		t.Fatal("should not be locked after clearing failures")
	}
}

func TestSessionManager_RequireAuth(t *testing.T) {
	sm := newTestSessionManager()

	handler := sm.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Unauthenticated request → redirect.
	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("unauthenticated: status = %d, want %d", rec.Code, http.StatusFound)
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/login" {
		t.Fatalf("redirect location = %q, want /admin/login", loc)
	}

	// Authenticated request → 200.
	sessionID, _ := sm.CreateSession()
	req2 := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	req2.AddCookie(&http.Cookie{Name: "smgr_session", Value: sessionID})
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("authenticated: status = %d, want %d", rec2.Code, http.StatusOK)
	}
}

func TestSessionManager_CSRFToken(t *testing.T) {
	sm := newTestSessionManager()

	sessionID, _ := sm.CreateSession()

	token1 := sm.getOrCreateCSRFToken(sessionID)
	if token1 == "" {
		t.Fatal("CSRF token should not be empty")
	}

	// Same session → same token.
	token2 := sm.getOrCreateCSRFToken(sessionID)
	if token1 != token2 {
		t.Fatal("same session should return same CSRF token")
	}

	// Different session → different token.
	sessionID2, _ := sm.CreateSession()
	token3 := sm.getOrCreateCSRFToken(sessionID2)
	if token1 == token3 {
		t.Fatal("different sessions should have different CSRF tokens")
	}
}

func TestSessionManager_SetClearCookie(t *testing.T) {
	sm := newTestSessionManager()

	rec := httptest.NewRecorder()
	sm.SetSessionCookie(rec, "test-session-id")

	cookies := rec.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "smgr_session" {
			found = true
			if c.Value != "test-session-id" {
				t.Fatalf("cookie value = %q, want test-session-id", c.Value)
			}
			if !c.HttpOnly {
				t.Fatal("cookie should be HttpOnly")
			}
		}
	}
	if !found {
		t.Fatal("session cookie not found")
	}

	// Clear cookie.
	rec2 := httptest.NewRecorder()
	sm.ClearSessionCookie(rec2)
	for _, c := range rec2.Result().Cookies() {
		if c.Name == "smgr_session" && c.MaxAge != -1 {
			t.Fatalf("cleared cookie MaxAge = %d, want -1", c.MaxAge)
		}
	}
}
