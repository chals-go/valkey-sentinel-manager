//go:build !dns_select || dns_restapi

package dns

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestRestAPIProvider(t *testing.T, srvURL string, extra map[string]string) *RestAPIProvider {
	t.Helper()
	cfg := map[string]string{
		"base_url":      srvURL,
		"headers":       `{"Authorization":"Bearer test-key"}`,
		"update_method": "PUT",
		"update_url":    "/api/record",
		"update_body":   `{"hostname":"$domain","ip":"$ip","ttl":$ttl,"type":"$record_type"}`,
	}
	for k, v := range extra {
		cfg[k] = v
	}
	p, err := NewRestAPIProvider(cfg)
	if err != nil {
		t.Fatalf("NewRestAPIProvider: %v", err)
	}
	return p
}

func TestRestAPIProvider_UpdateRecord(t *testing.T) {
	var gotBody string
	var gotMethod, gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := newTestRestAPIProvider(t, srv.URL, nil)
	err := p.UpdateRecord(context.Background(), "example.com", "primary", "A", "10.0.0.1", 60)
	if err != nil {
		t.Fatalf("UpdateRecord: %v", err)
	}
	if gotMethod != "PUT" {
		t.Errorf("method = %s, want PUT", gotMethod)
	}
	if gotPath != "/api/record" {
		t.Errorf("path = %s, want /api/record", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("auth = %q, want 'Bearer test-key'", gotAuth)
	}

	// Verify variable substitution in body.
	var body map[string]any
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body["hostname"] != "primary.example.com" {
		t.Errorf("hostname = %v, want primary.example.com", body["hostname"])
	}
	if body["ip"] != "10.0.0.1" {
		t.Errorf("ip = %v, want 10.0.0.1", body["ip"])
	}
	if body["ttl"] != float64(60) {
		t.Errorf("ttl = %v, want 60", body["ttl"])
	}
}

func TestRestAPIProvider_UpdateRecordValues(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := newTestRestAPIProvider(t, srv.URL, map[string]string{
		"update_multi_method": "PUT",
		"update_multi_url":    "/api/records",
		"update_multi_body":   `{"hostname":"$domain","ips":$ips,"ttl":$ttl}`,
	})
	err := p.UpdateRecordValues(context.Background(), "example.com", "replica", "A", []string{"10.0.0.2", "10.0.0.3"}, 30)
	if err != nil {
		t.Fatalf("UpdateRecordValues: %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body["hostname"] != "replica.example.com" {
		t.Errorf("hostname = %v, want replica.example.com", body["hostname"])
	}
	ips, ok := body["ips"].([]any)
	if !ok || len(ips) != 2 {
		t.Fatalf("ips = %v, want 2 elements", body["ips"])
	}
	if ips[0] != "10.0.0.2" || ips[1] != "10.0.0.3" {
		t.Errorf("ips = %v, want [10.0.0.2, 10.0.0.3]", ips)
	}
}

func TestRestAPIProvider_UpdateRecordValues_FallbackToUpdate(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// multi endpoint 미설정 → update로 fallback (마지막 IP).
	p := newTestRestAPIProvider(t, srv.URL, nil)
	err := p.UpdateRecordValues(context.Background(), "example.com", "replica", "A", []string{"10.0.0.2", "10.0.0.3"}, 30)
	if err != nil {
		t.Fatalf("UpdateRecordValues fallback: %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body["ip"] != "10.0.0.3" {
		t.Errorf("fallback ip = %v, want 10.0.0.3 (last)", body["ip"])
	}
}

func TestRestAPIProvider_UpdateRecordValues_Empty(t *testing.T) {
	p := newTestRestAPIProvider(t, "http://unused", nil)
	err := p.UpdateRecordValues(context.Background(), "example.com", "replica", "A", nil, 30)
	if err != nil {
		t.Fatalf("empty values should not error: %v", err)
	}
}

func TestRestAPIProvider_RemoveRecordValue(t *testing.T) {
	var gotBody string
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := newTestRestAPIProvider(t, srv.URL, map[string]string{
		"delete_method": "DELETE",
		"delete_url":    "/api/record",
		"delete_body":   `{"hostname":"$domain","ip":"$ip"}`,
	})
	err := p.RemoveRecordValue(context.Background(), "example.com", "replica", "A", "10.0.0.2")
	if err != nil {
		t.Fatalf("RemoveRecordValue: %v", err)
	}
	if gotMethod != "DELETE" {
		t.Errorf("method = %s, want DELETE", gotMethod)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body["ip"] != "10.0.0.2" {
		t.Errorf("ip = %v, want 10.0.0.2", body["ip"])
	}
}

func TestRestAPIProvider_RemoveRecordValue_NotConfigured(t *testing.T) {
	p := newTestRestAPIProvider(t, "http://unused", nil)
	// delete 미설정 시 경고만 출력하고 nil 반환.
	err := p.RemoveRecordValue(context.Background(), "example.com", "replica", "A", "10.0.0.2")
	if err != nil {
		t.Fatalf("should not error when delete not configured: %v", err)
	}
}

func TestRestAPIProvider_HealthCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("path = %s, want /health", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := newTestRestAPIProvider(t, srv.URL, map[string]string{
		"health_method": "GET",
		"health_url":    "/health",
	})
	if err := p.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
}

func TestRestAPIProvider_HealthCheck_Default(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Errorf("path = %s, want /", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// health 미설정 시 base_url + "/" 로 GET.
	p := newTestRestAPIProvider(t, srv.URL, nil)
	if err := p.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck default: %v", err)
	}
}

func TestRestAPIProvider_HealthCheck_Fail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := newTestRestAPIProvider(t, srv.URL, nil)
	if err := p.HealthCheck(context.Background()); err == nil {
		t.Fatal("HealthCheck should fail on 500")
	}
}

func TestRestAPIProvider_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	p := newTestRestAPIProvider(t, srv.URL, nil)
	err := p.UpdateRecord(context.Background(), "example.com", "primary", "A", "10.0.0.1", 60)
	if err == nil {
		t.Fatal("should error on HTTP 400")
	}
	if got := err.Error(); got == "" {
		t.Fatal("error message should not be empty")
	}
}

func TestRestAPIProvider_URLTemplateSubstitution(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := newTestRestAPIProvider(t, srv.URL, map[string]string{
		"update_url": "/zones/$zone/records/$name/$record_type",
	})
	p.UpdateRecord(context.Background(), "example.com", "primary", "A", "10.0.0.1", 60)
	if gotPath != "/zones/example.com/records/primary/A" {
		t.Errorf("path = %s, want /zones/example.com/records/primary/A", gotPath)
	}
}

func TestRenderTemplate(t *testing.T) {
	vars := map[string]string{
		"domain":      "primary.example.com",
		"ip":          "10.0.0.1",
		"ttl":         "60",
		"record_type": "A",
	}
	got := renderTemplate(`{"host":"$domain","ip":"$ip","ttl":$ttl}`, vars)
	want := `{"host":"primary.example.com","ip":"10.0.0.1","ttl":60}`
	if got != want {
		t.Errorf("renderTemplate = %s, want %s", got, want)
	}
}

func TestNewRestAPIProvider_Validation(t *testing.T) {
	_, err := NewRestAPIProvider(map[string]string{})
	if err == nil {
		t.Fatal("should error without base_url")
	}

	_, err = NewRestAPIProvider(map[string]string{
		"base_url": "http://example.com",
		"headers":  "invalid json",
	})
	if err == nil {
		t.Fatal("should error with invalid headers JSON")
	}
}

func TestNewProvider_Factory(t *testing.T) {
	if !IsProviderAvailable("restapi") {
		t.Skip("restapi provider not available in this build")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p, err := NewProvider(context.Background(), "restapi", map[string]string{
		"base_url":      srv.URL,
		"update_method": "PUT",
		"update_url":    "/api/record",
		"update_body":   `{"ip":"$ip"}`,
	})
	if err != nil {
		t.Fatalf("NewProvider(restapi): %v", err)
	}
	if p == nil {
		t.Fatal("provider is nil")
	}

	_, err = NewProvider(context.Background(), "unknown", nil)
	if err == nil {
		t.Fatal("NewProvider(unknown) should fail")
	}
}
