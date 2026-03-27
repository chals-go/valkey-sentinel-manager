package dns

import (
	"context"
	"testing"
)

func TestNewProvider_Bind(t *testing.T) {
	if !IsProviderAvailable("bind") {
		t.Skip("bind provider not included in build")
	}
	p, err := NewProvider(context.Background(), "bind", map[string]string{
		"api_url": "http://localhost:8053",
		"api_key": "test-key",
	})
	if err != nil {
		t.Fatalf("bind: unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("bind: provider should not be nil")
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
