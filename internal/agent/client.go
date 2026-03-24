package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// SendEvent는 이벤트 페이로드를 Monitor 서버로 전송하며, 실패 시 재시도한다.
// 전송에 성공하면 true를, 모든 재시도가 소진되면 false를 반환한다.
func SendEvent(cfg *Config, payload map[string]any) bool {
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[ERROR] marshal payload: %v", err)
		return false
	}

	endpoint := strings.TrimRight(cfg.MonitorURL, "/") + "/api/v1/events"
	client := &http.Client{Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second}

	for attempt := 1; attempt <= cfg.RetryCount; attempt++ {
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
		if err != nil {
			log.Printf("[ERROR] create request: %v", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		if cfg.APIKey != "" {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cfg.APIKey))
		}

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("[WARN] send failed (attempt %d/%d): %v", attempt, cfg.RetryCount, err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			log.Printf("[INFO] event sent: url=%s status=%d", endpoint, resp.StatusCode)
			return true
		}
		log.Printf("[WARN] send failed (HTTP %d): attempt %d/%d", resp.StatusCode, attempt, cfg.RetryCount)
	}

	log.Printf("[ERROR] all attempts failed: %s", endpoint)
	return false
}
