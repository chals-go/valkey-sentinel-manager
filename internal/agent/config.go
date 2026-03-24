// Package agent implements the sentinel-agent client that runs on Sentinel nodes.
package agent

import (
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds sentinel-agent settings.
type Config struct {
	MonitorURL       string `yaml:"monitor_url"`
	APIKey           string `yaml:"api_key"`
	SentinelNodeName string `yaml:"sentinel_node_name"`
	GroupName        string `yaml:"group_name"`
	TimeoutSeconds   int    `yaml:"timeout_seconds"`
	RetryCount       int    `yaml:"retry_count"`
}

// LoadConfig loads configuration from a YAML file then environment variables.
// Priority: env vars > YAML file > defaults.
func LoadConfig() *Config {
	cfg := &Config{
		MonitorURL:     "http://localhost:8000",
		TimeoutSeconds: 10,
		RetryCount:     2,
	}

	configPath := envOr("SMGR_AGENT_CONFIG", "/etc/valkey/sentinel-agent.yaml")
	if data, err := os.ReadFile(configPath); err == nil {
		yaml.Unmarshal(data, cfg)
	}

	if v := os.Getenv("SMGR_MONITOR_URL"); v != "" {
		cfg.MonitorURL = strings.TrimSpace(v)
	}
	if v := os.Getenv("SMGR_API_KEY"); v != "" {
		cfg.APIKey = v
	}
	if v := os.Getenv("SMGR_SENTINEL_NODE_NAME"); v != "" {
		cfg.SentinelNodeName = v
	}
	if v := os.Getenv("SMGR_GROUP_NAME"); v != "" {
		cfg.GroupName = v
	}
	if v := os.Getenv("SMGR_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.TimeoutSeconds = n
		}
	}
	if v := os.Getenv("SMGR_RETRY_COUNT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RetryCount = n
		}
	}

	return cfg
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
