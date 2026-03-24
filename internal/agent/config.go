// Package agent는 센티널 노드에서 실행되는 sentinel-agent 클라이언트를 구현한다.
// 재설정(reconfig) 스크립트와 알림(notify) 스크립트의 실행 진입점 및
// Monitor 서버로의 이벤트 전송 기능을 제공한다.
package agent

import (
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config는 sentinel-agent의 설정값을 보관하는 구조체다.
// YAML 파일과 환경 변수에서 값을 읽어 채운다.
type Config struct {
	MonitorURL       string `yaml:"monitor_url"`
	APIKey           string `yaml:"api_key"`
	SentinelNodeName string `yaml:"sentinel_node_name"`
	GroupName        string `yaml:"group_name"`
	TimeoutSeconds   int    `yaml:"timeout_seconds"`
	RetryCount       int    `yaml:"retry_count"`
}

// LoadConfig는 YAML 파일과 환경 변수에서 설정을 읽어 Config를 반환한다.
// 우선순위: 환경 변수 > YAML 파일 > 기본값 순으로 적용된다.
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
