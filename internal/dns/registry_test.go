package dns

import (
	"context"
	"testing"
)

func TestNewProvider_RestAPI(t *testing.T) {
	if !IsProviderAvailable("restapi") {
		t.Skip("restapi provider not included in build")
	}
	p, err := NewProvider(context.Background(), "restapi", map[string]string{
		"base_url":      "http://localhost:8053",
		"update_method": "PUT",
		"update_url":    "/api/record",
		"update_body":   `{"ip":"$ip"}`,
	})
	if err != nil {
		t.Fatalf("restapi: unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("restapi: provider should not be nil")
	}
}

func TestNewProvider_Cloudflare(t *testing.T) {
	if !IsProviderAvailable("cloudflare") {
		t.Skip("cloudflare provider not included in build")
	}
	p, err := NewProvider(context.Background(), "cloudflare", map[string]string{
		"api_token": "test-token",
		"zone_id":   "zone123",
	})
	if err != nil {
		t.Fatalf("cloudflare: unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("cloudflare: provider should not be nil")
	}
}

func TestNewProvider_Cloudflare_MissingToken(t *testing.T) {
	if !IsProviderAvailable("cloudflare") {
		t.Skip("cloudflare provider not included in build")
	}
	_, err := NewProvider(context.Background(), "cloudflare", map[string]string{
		"zone_id": "zone123",
	})
	if err == nil {
		t.Fatal("cloudflare without api_token should return error")
	}
}

func TestNewProvider_Cloudflare_MissingZoneID(t *testing.T) {
	if !IsProviderAvailable("cloudflare") {
		t.Skip("cloudflare provider not included in build")
	}
	_, err := NewProvider(context.Background(), "cloudflare", map[string]string{
		"api_token": "test-token",
	})
	if err == nil {
		t.Fatal("cloudflare without zone_id should return error")
	}
}

func TestNewProvider_Unknown(t *testing.T) {
	_, err := NewProvider(context.Background(), "nonexistent", map[string]string{})
	if err == nil {
		t.Fatal("unknown provider should return error")
	}
}

func TestAvailableProviders(t *testing.T) {
	providers := AvailableProviders()
	if len(providers) == 0 {
		t.Fatal("at least one provider should be available in default build")
	}
	for _, p := range providers {
		if p.Type == "" || p.DisplayName == "" {
			t.Fatalf("provider info incomplete: %+v", p)
		}
		if !IsProviderAvailable(p.Type) {
			t.Fatalf("listed provider %q should be available", p.Type)
		}
	}
}

func TestIsProviderAvailable_Nonexistent(t *testing.T) {
	if IsProviderAvailable("nonexistent") {
		t.Fatal("nonexistent provider should not be available")
	}
}
