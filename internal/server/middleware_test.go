package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := SecurityHeaders(inner)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	expected := map[string]string{
		"X-Content-Type-Options":            "nosniff",
		"X-Frame-Options":                   "DENY",
		"Referrer-Policy":                   "strict-origin-when-cross-origin",
		"X-Permitted-Cross-Domain-Policies": "none",
	}
	for header, want := range expected {
		got := rec.Header().Get(header)
		if got != want {
			t.Errorf("header %q = %q, want %q", header, got, want)
		}
	}

	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Error("Content-Security-Policy header is empty")
	}
}

func TestRequestLogger_StatusCapture(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	handler := RequestLogger(inner)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestTemplateFuncMap(t *testing.T) {
	tr := func(key string) string { return "translated:" + key }
	fm := TemplateFuncMap(tr)

	tFunc, ok := fm["t"].(func(string) string)
	if !ok {
		t.Fatal("t func not found in FuncMap")
	}
	if got := tFunc("hello"); got != "translated:hello" {
		t.Errorf("t('hello') = %q, want 'translated:hello'", got)
	}

	isMP, ok := fm["isMonitoringPage"].(func(string) bool)
	if !ok {
		t.Fatal("isMonitoringPage func not found")
	}
	if !isMP("dashboard") {
		t.Error("dashboard should be a monitoring page")
	}
	if isMP("settings-server") {
		t.Error("settings-server should not be a monitoring page")
	}
}
