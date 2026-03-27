package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chals-go/valkey-sentinel-manager/internal/models"
	"github.com/chals-go/valkey-sentinel-manager/internal/store"
)

func testEvent() NotificationEvent {
	return NotificationEvent{
		EventType: "primary_failover",
		Name:      "test-master",
		OldNode:   "10.0.0.1:6379",
		NewNode:   "10.0.0.2:6379",
		DNSRecord: "primary.example.com → 10.0.0.2",
		Timestamp: time.Date(2025, 1, 15, 12, 0, 0, 0, time.FixedZone("KST", 9*3600)),
	}
}

func TestRenderText_Failover(t *testing.T) {
	e := testEvent()
	text := renderText(e)

	if !strings.Contains(text, ":red_circle:") {
		t.Error("failover should contain :red_circle: icon")
	}
	if !strings.Contains(text, "Primary Failover") {
		t.Error("should contain 'Primary Failover' label")
	}
	if !strings.Contains(text, "test-master") {
		t.Error("should contain master name")
	}
	if !strings.Contains(text, "10.0.0.1:6379 → 10.0.0.2:6379") {
		t.Error("should contain old → new node")
	}
	if !strings.Contains(text, "primary.example.com") {
		t.Error("should contain DNS record")
	}
}

func TestRenderText_ReplicaDown(t *testing.T) {
	e := NotificationEvent{
		EventType: "replica_down",
		Name:      "test-master",
		Node:      "10.0.0.3:6379",
		Timestamp: time.Now(),
	}
	text := renderText(e)
	if !strings.Contains(text, ":warning:") {
		t.Error("replica_down should contain :warning: icon")
	}
	if !strings.Contains(text, "Replica Down") {
		t.Error("should contain 'Replica Down'")
	}
	if !strings.Contains(text, "10.0.0.3:6379") {
		t.Error("should contain node address")
	}
}

func TestRenderText_ReplicaUp(t *testing.T) {
	e := NotificationEvent{
		EventType: "replica_up",
		Name:      "test-master",
		Node:      "10.0.0.3:6379",
		Timestamp: time.Now(),
	}
	text := renderText(e)
	if !strings.Contains(text, ":large_green_circle:") {
		t.Error("replica_up should contain :large_green_circle: icon")
	}
	if !strings.Contains(text, "Replica Up") {
		t.Error("should contain 'Replica Up'")
	}
}

func TestRenderPlainText(t *testing.T) {
	e := testEvent()
	text := renderPlainText(e)

	if strings.Contains(text, ":red_circle:") {
		t.Error("plain text should not contain emoji")
	}
	if !strings.Contains(text, "[Primary Failover]") {
		t.Error("should contain bracketed label")
	}
	if !strings.Contains(text, "test-master") {
		t.Error("should contain master name")
	}
}

func TestSendSlack(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ep := &models.WebhookEndpoint{Type: models.WebhookTypeSlack, URL: srv.URL, Channel: "ops"}
	err := sendSlack(context.Background(), ep, testEvent())
	if err != nil {
		t.Fatalf("sendSlack: %v", err)
	}
	if received["username"] != "Sentinel Manager" {
		t.Errorf("username = %v", received["username"])
	}
	if received["channel"] != "#ops" {
		t.Errorf("channel = %v, want #ops", received["channel"])
	}
	text, _ := received["text"].(string)
	if !strings.Contains(text, "Primary Failover") {
		t.Error("text should contain event label")
	}
}

func TestSendDiscord(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ep := &models.WebhookEndpoint{Type: models.WebhookTypeDiscord, URL: srv.URL}
	err := sendDiscord(context.Background(), ep, testEvent())
	if err != nil {
		t.Fatalf("sendDiscord: %v", err)
	}
	embeds, _ := received["embeds"].([]any)
	if len(embeds) != 1 {
		t.Fatalf("embeds len = %d, want 1", len(embeds))
	}
	embed, _ := embeds[0].(map[string]any)
	if embed["title"] != "Primary Failover" {
		t.Errorf("title = %v", embed["title"])
	}
}

func TestSendTeams(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ep := &models.WebhookEndpoint{Type: models.WebhookTypeTeams, URL: srv.URL}
	err := sendTeams(context.Background(), ep, testEvent())
	if err != nil {
		t.Fatalf("sendTeams: %v", err)
	}
	if received["@type"] != "MessageCard" {
		t.Errorf("@type = %v", received["@type"])
	}
	if received["title"] != "Primary Failover" {
		t.Errorf("title = %v", received["title"])
	}
}

func TestSendKakaoWork(t *testing.T) {
	var authHeader string
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ep := &models.WebhookEndpoint{Type: models.WebhookTypeKakaoWork, URL: srv.URL, AppKey: "test-app-key", ConversationID: "conv-123"}
	err := sendKakaoWork(context.Background(), ep, testEvent())
	if err != nil {
		t.Fatalf("sendKakaoWork: %v", err)
	}
	if authHeader != "Bearer test-app-key" {
		t.Errorf("Authorization = %q, want 'Bearer test-app-key'", authHeader)
	}
	if received["conversation_id"] != "conv-123" {
		t.Errorf("conversation_id = %v", received["conversation_id"])
	}
}

func TestSendCustom_TextMode(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ep := &models.WebhookEndpoint{Type: models.WebhookTypeCustom, URL: srv.URL, PayloadMode: "text", BodyKey: "message"}
	err := sendCustom(context.Background(), ep, testEvent())
	if err != nil {
		t.Fatalf("sendCustom text: %v", err)
	}
	if _, ok := received["message"]; !ok {
		t.Error("text mode should use BodyKey 'message'")
	}
}

func TestSendCustom_JSONMode(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ep := &models.WebhookEndpoint{Type: models.WebhookTypeCustom, URL: srv.URL, PayloadMode: "json"}
	err := sendCustom(context.Background(), ep, testEvent())
	if err != nil {
		t.Fatalf("sendCustom json: %v", err)
	}
	if received["event_type"] != "primary_failover" {
		t.Errorf("event_type = %v", received["event_type"])
	}
	if received["name"] != "test-master" {
		t.Errorf("name = %v", received["name"])
	}
	if _, ok := received["old_node"]; !ok {
		t.Error("json mode should include old_node")
	}
}

func TestSendNotifications(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := store.NewMemoryStore(30)
	ctx := context.Background()

	// One enabled, one disabled.
	s.SaveWebhook(ctx, &models.WebhookEndpoint{ID: "wh1", Type: models.WebhookTypeSlack, URL: srv.URL, Enabled: true})
	s.SaveWebhook(ctx, &models.WebhookEndpoint{ID: "wh2", Type: models.WebhookTypeSlack, URL: srv.URL, Enabled: false})

	SendNotifications(ctx, s, testEvent())

	if callCount != 1 {
		t.Fatalf("callCount = %d, want 1 (only enabled webhook)", callCount)
	}
}

func TestSendTestNotification(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ep := &models.WebhookEndpoint{Type: models.WebhookTypeSlack, URL: srv.URL}
	err := SendTestNotification(context.Background(), ep)
	if err != nil {
		t.Fatalf("SendTestNotification: %v", err)
	}
	text, _ := received["text"].(string)
	if !strings.Contains(text, "test-master") {
		t.Error("test notification should contain 'test-master'")
	}
}
