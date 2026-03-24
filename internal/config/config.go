// Package config는 config.yaml 파일과 환경 변수로부터 애플리케이션 설정을 로드하는 기능을 제공한다.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Config는 Sentinel Manager 서버의 전체 설정을 담는 구조체이다.
type Config struct {
	// Server
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	Debug        bool   `yaml:"debug"`
	SecureCookie bool   `yaml:"secure_cookie"`

	// Data store (internal Valkey for Monitor data)
	StoreType           string `yaml:"store_type"`
	StoreURL            string `yaml:"store_url"`
	StoreSentinels      string `yaml:"store_sentinels"`
	StoreSentinelMaster string `yaml:"store_sentinel_master"`
	StoreDB             int    `yaml:"store_db"`
	StorePassword       string `yaml:"store_password"`

	// Event processing
	EventDedupWindowSeconds int `yaml:"event_dedup_window_seconds"`
	QuorumThreshold         int `yaml:"quorum_threshold"`

	// Logging
	LogDir string `yaml:"log_dir"`

	// DNS defaults
	DNSDefaultTTL     int     `yaml:"dns_default_ttl"`
	DNSRetryCount     int     `yaml:"dns_retry_count"`
	DNSRetryBaseDelay float64 `yaml:"dns_retry_base_delay"`

	// Encryption key (32-byte base64-encoded, for sensitive config encryption)
	EncryptionKey string `yaml:"encryption_key"`
}

// Load는 지정된 YAML 설정 파일을 읽고 환경 변수 오버라이드를 적용하여 Config를 반환한다.
// encryption_key가 비어 있으면 새 키를 자동 생성하고 YAML 파일에 다시 저장한다.
func Load(configFile string) *Config {
	cfg := defaults()

	if configFile != "" {
		loadYAML(configFile, cfg)
	}

	applyEnvOverrides(cfg)

	// Auto-generate encryption key if not set.
	if cfg.EncryptionKey == "" {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			slog.Error("failed to generate encryption key", "error", err)
		} else {
			cfg.EncryptionKey = base64.StdEncoding.EncodeToString(key)
			slog.Info("encryption key auto-generated — saving to config file")
			if configFile != "" {
				writeBackEncryptionKey(configFile, cfg.EncryptionKey)
			}
		}
	}

	return cfg
}

func defaults() *Config {
	return &Config{
		Host:                    "0.0.0.0",
		Port:                    8000,
		StoreType:               "valkey",
		StoreURL:                "valkey://localhost:6379/0",
		StoreSentinelMaster:     "smgr-store",
		EventDedupWindowSeconds: 30,
		QuorumThreshold:         2,
		LogDir:                  "/var/log/sentinel-manager",
		DNSDefaultTTL:           3,
		DNSRetryCount:           3,
		DNSRetryBaseDelay:       1.0,
	}
}

func loadYAML(path string, cfg *Config) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("failed to read config file", "path", path, "error", err)
		}
		return
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		slog.Warn("failed to parse config file", "path", path, "error", err)
	}
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("SMGR_HOST"); v != "" {
		cfg.Host = v
	}
	if v := os.Getenv("SMGR_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Port = n
		}
	}
	if v := os.Getenv("SMGR_DEBUG"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Debug = b
		}
	}
	if v := os.Getenv("SMGR_SECURE_COOKIE"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.SecureCookie = b
		}
	}
	if v := os.Getenv("SMGR_STORE_TYPE"); v != "" {
		cfg.StoreType = v
	}
	if v := os.Getenv("SMGR_STORE_URL"); v != "" {
		cfg.StoreURL = v
	}
	if v := os.Getenv("SMGR_STORE_SENTINELS"); v != "" {
		cfg.StoreSentinels = v
	}
	if v := os.Getenv("SMGR_STORE_SENTINEL_MASTER"); v != "" {
		cfg.StoreSentinelMaster = v
	}
	if v := os.Getenv("SMGR_STORE_DB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.StoreDB = n
		}
	}
	if v := os.Getenv("SMGR_STORE_PASSWORD"); v != "" {
		cfg.StorePassword = v
	}
	if v := os.Getenv("SMGR_EVENT_DEDUP_WINDOW_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.EventDedupWindowSeconds = n
		}
	}
	if v := os.Getenv("SMGR_QUORUM_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.QuorumThreshold = n
		}
	}
	if v := os.Getenv("SMGR_LOG_DIR"); v != "" {
		cfg.LogDir = v
	}
	if v := os.Getenv("SMGR_DNS_DEFAULT_TTL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.DNSDefaultTTL = n
		}
	}
	if v := os.Getenv("SMGR_DNS_RETRY_COUNT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.DNSRetryCount = n
		}
	}
	if v := os.Getenv("SMGR_DNS_RETRY_BASE_DELAY"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.DNSRetryBaseDelay = f
		}
	}
	if v := os.Getenv("SMGR_ENCRYPTION_KEY"); v != "" {
		cfg.EncryptionKey = v
	}
}

// writeBackEncryptionKey는 YAML 설정 파일에 encryption_key를 추가하거나 갱신한다.
func writeBackEncryptionKey(path, key string) {
	// Read existing file, re-parse, set key, write back.
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		slog.Warn("cannot write back encryption key", "error", err)
		return
	}

	var raw map[string]any
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &raw); err != nil {
			slog.Warn("cannot parse config for write-back", "error", err)
			return
		}
	}
	if raw == nil {
		raw = make(map[string]any)
	}
	raw["encryption_key"] = key

	out, err := yaml.Marshal(raw)
	if err != nil {
		slog.Warn("cannot marshal config for write-back", "error", err)
		return
	}
	if err := os.WriteFile(path, out, 0600); err != nil {
		slog.Warn("cannot write config file", "path", path, "error", err)
	}
}

// Addr는 서버 리슨 주소를 "host:port" 형식으로 반환한다.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}
