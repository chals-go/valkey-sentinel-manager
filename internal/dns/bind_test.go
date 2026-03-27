//go:build !dns_select || dns_bind

package dns

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBINDProvider_UpdateRecord(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s, want PUT", r.Method)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewBINDProvider(srv.URL, "test-key")
	err := p.UpdateRecord(context.Background(), "example.com", "primary", "A", "10.0.0.1", 3)
	if err != nil {
		t.Fatalf("UpdateRecord: %v", err)
	}
	if gotBody["value"] != "10.0.0.1" {
		t.Fatalf("value = %v, want 10.0.0.1", gotBody["value"])
	}
}

func TestBINDProvider_UpdateRecordValues(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewBINDProvider(srv.URL, "")
	err := p.UpdateRecordValues(context.Background(), "example.com", "replica", "A", []string{"10.0.0.2", "10.0.0.3"}, 3)
	if err != nil {
		t.Fatalf("UpdateRecordValues: %v", err)
	}
	vals, ok := gotBody["values"].([]any)
	if !ok || len(vals) != 2 {
		t.Fatalf("values = %v, want 2 elements", gotBody["values"])
	}
}

func TestBINDProvider_UpdateRecordValues_Empty(t *testing.T) {
	p := NewBINDProvider("http://unused", "")
	err := p.UpdateRecordValues(context.Background(), "example.com", "replica", "A", nil, 3)
	if err != nil {
		t.Fatalf("empty values should not error: %v", err)
	}
}

func TestBINDProvider_HealthCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("path = %s, want /health", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewBINDProvider(srv.URL, "")
	if err := p.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
}

func TestBINDProvider_HealthCheck_Fail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := NewBINDProvider(srv.URL, "")
	if err := p.HealthCheck(context.Background()); err == nil {
		t.Fatal("HealthCheck should fail on 500")
	}
}

func TestBINDProvider_VerifyRecord(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"values": []string{"10.0.0.1", "10.0.0.2"},
		})
	}))
	defer srv.Close()

	p := NewBINDProvider(srv.URL, "")
	ok, err := p.VerifyRecord(context.Background(), "example.com", "primary", "10.0.0.1")
	if err != nil {
		t.Fatalf("VerifyRecord: %v", err)
	}
	if !ok {
		t.Fatal("expected record to be found")
	}

	ok, err = p.VerifyRecord(context.Background(), "example.com", "primary", "10.0.0.99")
	if err != nil {
		t.Fatalf("VerifyRecord: %v", err)
	}
	if ok {
		t.Fatal("expected record not to be found")
	}
}

func TestBINDProvider_AuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewBINDProvider(srv.URL, "my-secret-key")
	p.UpdateRecord(context.Background(), "z", "r", "A", "1.2.3.4", 3)
	if gotAuth != "Bearer my-secret-key" {
		t.Fatalf("Authorization = %q, want 'Bearer my-secret-key'", gotAuth)
	}
}

func TestNewProvider_Factory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p, err := NewProvider(context.Background(), "bind", map[string]string{
		"api_url": srv.URL,
		"api_key": "test",
	})
	if err != nil {
		t.Fatalf("NewProvider(bind): %v", err)
	}
	if p == nil {
		t.Fatal("provider is nil")
	}

	_, err = NewProvider(context.Background(), "unknown", nil)
	if err == nil {
		t.Fatal("NewProvider(unknown) should fail")
	}
}
