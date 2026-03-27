package dns

import (
	"context"
	"testing"
)

func TestNewProvider_Bind(t *testing.T) {
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
	_, err := NewProvider(context.Background(), "cloudflare", map[string]string{
		"zone_id": "zone123",
	})
	if err == nil {
		t.Fatal("cloudflare without api_token should return error")
	}
}

func TestNewProvider_Cloudflare_MissingZoneID(t *testing.T) {
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
