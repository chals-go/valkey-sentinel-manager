package config

import (
	"os"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	// Clear any SMGR_ env vars.
	for _, env := range os.Environ() {
		if len(env) > 5 && env[:5] == "SMGR_" {
			key, _, _ := cut(env, "=")
			os.Unsetenv(key)
		}
	}

	cfg := Load("")
	if cfg.Host != "0.0.0.0" {
		t.Fatalf("Host = %q, want 0.0.0.0", cfg.Host)
	}
	if cfg.Port != 8000 {
		t.Fatalf("Port = %d, want 8000", cfg.Port)
	}
	if cfg.StoreType != "valkey" {
		t.Fatalf("StoreType = %q, want valkey", cfg.StoreType)
	}
	if cfg.EventDedupWindowSeconds != 30 {
		t.Fatalf("EventDedupWindowSeconds = %d, want 30", cfg.EventDedupWindowSeconds)
	}
	if cfg.DNSDefaultTTL != 3 {
		t.Fatalf("DNSDefaultTTL = %d, want 3", cfg.DNSDefaultTTL)
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	os.Setenv("SMGR_PORT", "9090")
	os.Setenv("SMGR_DEBUG", "true")
	os.Setenv("SMGR_STORE_TYPE", "memory")
	defer func() {
		os.Unsetenv("SMGR_PORT")
		os.Unsetenv("SMGR_DEBUG")
		os.Unsetenv("SMGR_STORE_TYPE")
	}()

	cfg := Load("")
	if cfg.Port != 9090 {
		t.Fatalf("Port = %d, want 9090", cfg.Port)
	}
	if !cfg.Debug {
		t.Fatal("Debug should be true")
	}
	if cfg.StoreType != "memory" {
		t.Fatalf("StoreType = %q, want memory", cfg.StoreType)
	}
}

func TestAddr(t *testing.T) {
	cfg := &Config{Host: "127.0.0.1", Port: 3000}
	if got := cfg.Addr(); got != "127.0.0.1:3000" {
		t.Fatalf("Addr() = %q, want 127.0.0.1:3000", got)
	}
}

func cut(s, sep string) (string, string, bool) {
	for i := range s {
		if i+len(sep) <= len(s) && s[i:i+len(sep)] == sep {
			return s[:i], s[i+len(sep):], true
		}
	}
	return s, "", false
}
