package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/chals-go/valkey-sentinel-manager/internal/models"
)

// eventStyle은 이벤트 타입별 Slack 이모지와 레이블을 매핑하는 변수이다.
var eventStyle = map[models.EventType]struct {
	icon  string
	label string
}{
	models.EventTypeFailover:    {icon: ":red_circle:", label: "Primary Failover"},
	models.EventTypeReplicaDown: {icon: ":warning:", label: "Replica Down"},
	models.EventTypeReplicaUp:   {icon: ":large_green_circle:", label: "Replica Up"},
}

// buildMessageText는 이벤트와 클러스터 정보를 바탕으로 Slack 메시지 본문을 생성한다.
func buildMessageText(event *models.FailoverEvent, cluster *models.Cluster) string {
	style, ok := eventStyle[event.EventType]
	if !ok {
		style = struct {
			icon  string
			label string
		}{icon: ":information_source:", label: string(event.EventType)}
	}

	ts := time.Unix(int64(event.Timestamp), 0).Format("2006-01-02 15:04:05")

	var nodeInfo string
	if event.EventType == models.EventTypeFailover {
		nodeInfo = fmt.Sprintf("%s:%d → %s:%d", event.FromIP, event.FromPort, event.ToIP, event.ToPort)
	} else {
		nodeInfo = fmt.Sprintf("%s:%d", event.FromIP, event.FromPort)
	}

	var dnsInfo string
	if cluster != nil {
		switch event.EventType {
		case models.EventTypeFailover:
			fqdn := cluster.PrimaryDNS.RecordName + "." + cluster.PrimaryDNS.Zone
			dnsInfo = fmt.Sprintf("DNS : %s → %s", fqdn, event.ToIP)
		case models.EventTypeReplicaDown:
			if cluster.ReplicaDNS != nil {
				fqdn := cluster.ReplicaDNS.RecordName + "." + cluster.ReplicaDNS.Zone
				dnsInfo = fmt.Sprintf("DNS : %s -= %s", fqdn, event.FromIP)
			}
		case models.EventTypeReplicaUp:
			if cluster.ReplicaDNS != nil {
				fqdn := cluster.ReplicaDNS.RecordName + "." + cluster.ReplicaDNS.Zone
				dnsInfo = fmt.Sprintf("DNS : %s += %s", fqdn, event.FromIP)
			}
		}
	}

	lines := []string{
		fmt.Sprintf("%s *%s*", style.icon, style.label),
		fmt.Sprintf("Name : %s", event.MasterName),
		fmt.Sprintf("Node : %s", nodeInfo),
	}
	if dnsInfo != "" {
		lines = append(lines, dnsInfo)
	}
	lines = append(lines, fmt.Sprintf("Time : %s (KST)", ts))

	return strings.Join(lines, "\n")
}

// SendSlackNotification은 이벤트 알림을 Slack 인커밍 웹훅으로 전송한다.
func SendSlackNotification(ctx context.Context, webhookURL string, event *models.FailoverEvent, cluster *models.Cluster, channel string) bool {
	text := buildMessageText(event, cluster)

	payload := map[string]string{
		"text":       text,
		"username":   "Sentinel Manager",
		"icon_emoji": ":satellite:",
	}
	if channel != "" {
		if !strings.HasPrefix(channel, "#") {
			channel = "#" + channel
		}
		payload["channel"] = channel
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("failed to marshal slack payload", "error", err)
		return false
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		slog.Error("failed to create slack request", "error", err)
		return false
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("slack notification failed", "error", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		slog.Info("slack notification sent", "summary", strings.SplitN(text, "\n", 2)[0])
		return true
	}
	slog.Warn("slack notification failed", "status", resp.StatusCode)
	return false
}

// SendSentinelDownSlack은 센티널 노드 다운 알림을 Slack으로 전송한다.
func SendSentinelDownSlack(ctx context.Context, webhookURL, channel, nodeName, addr, groupName string) bool {
	text := fmt.Sprintf(":red_circle: *Sentinel Node Down*\nGroup : %s\nNode  : %s\nAddr  : %s\nTime  : %s (KST)",
		groupName, nodeName, addr, time.Now().Format("2006-01-02 15:04:05"))

	payload := map[string]string{
		"text":       text,
		"username":   "Sentinel Manager",
		"icon_emoji": ":satellite:",
	}
	if channel != "" {
		if !strings.HasPrefix(channel, "#") {
			channel = "#" + channel
		}
		payload["channel"] = channel
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return false
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("sentinel down slack failed", "error", err)
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// SendSentinelUpSlack은 센티널 노드 복구 알림을 Slack으로 전송한다.
func SendSentinelUpSlack(ctx context.Context, webhookURL, channel, nodeName, addr, groupName string) bool {
	text := fmt.Sprintf(":large_green_circle: *Sentinel Node Up*\nGroup : %s\nNode  : %s\nAddr  : %s\nTime  : %s (KST)",
		groupName, nodeName, addr, time.Now().Format("2006-01-02 15:04:05"))

	payload := map[string]string{
		"text":       text,
		"username":   "Sentinel Manager",
		"icon_emoji": ":satellite:",
	}
	if channel != "" {
		if !strings.HasPrefix(channel, "#") {
			channel = "#" + channel
		}
		payload["channel"] = channel
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return false
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("sentinel up slack failed", "error", err)
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
