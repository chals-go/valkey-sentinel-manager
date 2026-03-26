// Package core는 센티널 이벤트 처리, 페일오버 관리, 헬스체크 등 핵심 비즈니스 로직을 제공한다.
package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/chals-go/valkey-sentinel-manager/internal/models"
	"github.com/chals-go/valkey-sentinel-manager/internal/store"
)

// NotificationEvent는 알림 전송에 필요한 이벤트 데이터를 담는 구조체이다.
type NotificationEvent struct {
	EventType string    // primary_failover, replica_down, replica_up, sentinel_down, sentinel_up
	Name      string    // 그룹/마스터 이름
	OldNode   string    // failover: 이전 primary
	NewNode   string    // failover: 새 primary
	Node      string    // replica/sentinel down/up: 해당 노드
	DNSRecord string    // DNS 변경 내용 (빈 문자열이면 생략)
	Timestamp time.Time // 발생 시각
}

// eventMeta는 이벤트 타입별 아이콘과 라벨 메타데이터이다.
var eventMeta = map[string]struct {
	icon  string
	label string
	color int // Discord embed color
	theme string // Teams themeColor
}{
	"primary_failover": {icon: ":red_circle:", label: "Primary Failover", color: 0xE74C3C, theme: "FF0000"},
	"replica_down":     {icon: ":warning:", label: "Replica Down", color: 0xF39C12, theme: "FFA500"},
	"replica_up":       {icon: ":large_green_circle:", label: "Replica Up", color: 0x2ECC71, theme: "00FF00"},
	"sentinel_down":    {icon: ":red_circle:", label: "Sentinel Node Down", color: 0xE74C3C, theme: "FF0000"},
	"sentinel_up":      {icon: ":large_green_circle:", label: "Sentinel Node Up", color: 0x2ECC71, theme: "00FF00"},
}

// renderText는 이벤트를 텍스트 메시지로 렌더링한다.
func renderText(e NotificationEvent) string {
	meta, ok := eventMeta[e.EventType]
	if !ok {
		meta = eventMeta["primary_failover"]
	}
	var lines []string
	lines = append(lines, fmt.Sprintf("%s *%s*", meta.icon, meta.label))
	lines = append(lines, fmt.Sprintf("Name : %s", e.Name))
	if e.OldNode != "" && e.NewNode != "" {
		lines = append(lines, fmt.Sprintf("Node : %s → %s", e.OldNode, e.NewNode))
	} else if e.Node != "" {
		lines = append(lines, fmt.Sprintf("Node : %s", e.Node))
	}
	if e.DNSRecord != "" {
		lines = append(lines, fmt.Sprintf("DNS  : %s", e.DNSRecord))
	}
	lines = append(lines, fmt.Sprintf("Time : %s (KST)", e.Timestamp.Format("2006-01-02 15:04:05")))
	return strings.Join(lines, "\n")
}

// renderPlainText는 이모지 없는 플레인 텍스트 메시지를 렌더링한다.
func renderPlainText(e NotificationEvent) string {
	meta, ok := eventMeta[e.EventType]
	if !ok {
		meta = eventMeta["primary_failover"]
	}
	var lines []string
	lines = append(lines, fmt.Sprintf("[%s]", meta.label))
	lines = append(lines, fmt.Sprintf("Name : %s", e.Name))
	if e.OldNode != "" && e.NewNode != "" {
		lines = append(lines, fmt.Sprintf("Node : %s → %s", e.OldNode, e.NewNode))
	} else if e.Node != "" {
		lines = append(lines, fmt.Sprintf("Node : %s", e.Node))
	}
	if e.DNSRecord != "" {
		lines = append(lines, fmt.Sprintf("DNS  : %s", e.DNSRecord))
	}
	lines = append(lines, fmt.Sprintf("Time : %s (KST)", e.Timestamp.Format("2006-01-02 15:04:05")))
	return strings.Join(lines, "\n")
}

// postJSON은 JSON 페이로드를 HTTP POST로 전송하는 공통 헬퍼이다.
func postJSON(ctx context.Context, url string, payload any, headers map[string]string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		// Sanitize header key/value to prevent CRLF injection
		k = strings.ReplaceAll(strings.ReplaceAll(k, "\r", ""), "\n", "")
		v = strings.ReplaceAll(strings.ReplaceAll(v, "\r", ""), "\n", "")
		if k != "" {
			req.Header.Set(k, v)
		}
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("http status %d", resp.StatusCode)
	}
	return nil
}

// --- Slack Provider ---

func sendSlack(ctx context.Context, ep *models.WebhookEndpoint, e NotificationEvent) error {
	payload := map[string]string{
		"text":       renderText(e),
		"username":   "Sentinel Manager",
		"icon_emoji": ":satellite:",
	}
	if ep.Channel != "" {
		ch := ep.Channel
		if !strings.HasPrefix(ch, "#") {
			ch = "#" + ch
		}
		payload["channel"] = ch
	}
	return postJSON(ctx, ep.URL, payload, nil)
}

// --- Discord Provider ---

func sendDiscord(ctx context.Context, ep *models.WebhookEndpoint, e NotificationEvent) error {
	meta := eventMeta[e.EventType]

	var fields []map[string]any
	fields = append(fields, map[string]any{"name": "Name", "value": e.Name, "inline": true})
	if e.OldNode != "" && e.NewNode != "" {
		fields = append(fields, map[string]any{"name": "Node", "value": e.OldNode + " → " + e.NewNode, "inline": false})
	} else if e.Node != "" {
		fields = append(fields, map[string]any{"name": "Node", "value": e.Node, "inline": true})
	}
	if e.DNSRecord != "" {
		fields = append(fields, map[string]any{"name": "DNS", "value": e.DNSRecord, "inline": false})
	}
	fields = append(fields, map[string]any{"name": "Time", "value": e.Timestamp.Format("2006-01-02 15:04:05") + " (KST)", "inline": true})

	payload := map[string]any{
		"username": "Sentinel Manager",
		"embeds": []map[string]any{{
			"title":  meta.label,
			"color":  meta.color,
			"fields": fields,
		}},
	}
	return postJSON(ctx, ep.URL, payload, nil)
}

// --- Teams Provider ---

func sendTeams(ctx context.Context, ep *models.WebhookEndpoint, e NotificationEvent) error {
	meta := eventMeta[e.EventType]

	var facts []map[string]string
	facts = append(facts, map[string]string{"name": "Name", "value": e.Name})
	if e.OldNode != "" && e.NewNode != "" {
		facts = append(facts, map[string]string{"name": "Node", "value": e.OldNode + " → " + e.NewNode})
	} else if e.Node != "" {
		facts = append(facts, map[string]string{"name": "Node", "value": e.Node})
	}
	if e.DNSRecord != "" {
		facts = append(facts, map[string]string{"name": "DNS", "value": e.DNSRecord})
	}
	facts = append(facts, map[string]string{"name": "Time", "value": e.Timestamp.Format("2006-01-02 15:04:05") + " (KST)"})

	payload := map[string]any{
		"@type":      "MessageCard",
		"@context":   "http://schema.org/extensions",
		"themeColor": meta.theme,
		"title":      meta.label,
		"sections":   []map[string]any{{"facts": facts}},
	}
	return postJSON(ctx, ep.URL, payload, nil)
}

// --- 카카오워크 Provider ---

func sendKakaoWork(ctx context.Context, ep *models.WebhookEndpoint, e NotificationEvent) error {
	payload := map[string]string{
		"conversation_id": ep.ConversationID,
		"text":            renderPlainText(e),
	}
	headers := map[string]string{
		"Authorization": "Bearer " + ep.AppKey,
	}
	return postJSON(ctx, ep.URL, payload, headers)
}

// --- Custom Provider ---

func sendCustom(ctx context.Context, ep *models.WebhookEndpoint, e NotificationEvent) error {
	var payload any

	if ep.PayloadMode == "json" {
		p := map[string]any{
			"event_type": e.EventType,
			"name":       e.Name,
			"timestamp":  e.Timestamp.Format(time.RFC3339),
		}
		if e.OldNode != "" {
			p["old_node"] = e.OldNode
		}
		if e.NewNode != "" {
			p["new_node"] = e.NewNode
		}
		if e.Node != "" {
			p["node"] = e.Node
		}
		if e.DNSRecord != "" {
			p["dns_record"] = e.DNSRecord
		}
		payload = p
	} else {
		key := ep.BodyKey
		if key == "" {
			key = "text"
		}
		payload = map[string]string{key: renderText(e)}
	}

	return postJSON(ctx, ep.URL, payload, ep.CustomHeaders)
}

// --- 발송 디스패처 ---

// sendToEndpoint는 웹훅 타입에 따라 적절한 provider로 이벤트를 전송한다.
func sendToEndpoint(ctx context.Context, ep *models.WebhookEndpoint, e NotificationEvent) error {
	switch ep.Type {
	case models.WebhookTypeSlack:
		return sendSlack(ctx, ep, e)
	case models.WebhookTypeDiscord:
		return sendDiscord(ctx, ep, e)
	case models.WebhookTypeTeams:
		return sendTeams(ctx, ep, e)
	case models.WebhookTypeKakaoWork:
		return sendKakaoWork(ctx, ep, e)
	case models.WebhookTypeCustom:
		return sendCustom(ctx, ep, e)
	default:
		return fmt.Errorf("unknown webhook type: %s", ep.Type)
	}
}

// SendNotifications는 활성화된 모든 웹훅 엔드포인트로 이벤트를 전송한다.
func SendNotifications(ctx context.Context, s store.Store, e NotificationEvent) {
	webhooks, err := s.ListWebhooks(ctx)
	if err != nil {
		slog.Error("failed to list webhooks for notification", "error", err)
		return
	}
	for _, wh := range webhooks {
		if !wh.Enabled {
			continue
		}
		if err := sendToEndpoint(ctx, wh, e); err != nil {
			slog.Error("webhook send failed", "id", wh.ID, "name", wh.Name, "type", wh.Type, "error", err)
		} else {
			slog.Info("webhook notification sent", "id", wh.ID, "name", wh.Name, "type", wh.Type)
		}
	}
}

// SendTestNotification은 지정된 웹훅으로 테스트 이벤트를 전송한다.
func SendTestNotification(ctx context.Context, ep *models.WebhookEndpoint) error {
	e := NotificationEvent{
		EventType: "primary_failover",
		Name:      "test-master",
		OldNode:   "10.0.0.1:6379",
		NewNode:   "10.0.0.2:6379",
		DNSRecord: "primary-test.example.com → 10.0.0.2",
		Timestamp: time.Now(),
	}
	return sendToEndpoint(ctx, ep, e)
}
