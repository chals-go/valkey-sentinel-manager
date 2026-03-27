package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/chals-go/valkey-sentinel-manager/internal/models"
)

func TestMemoryStore_RecordEvent(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore(30)

	event := &models.FailoverEvent{
		GroupName: "g1", MasterName: "m1", EventType: models.EventTypeFailover,
		ToIP: "10.0.0.1", ToPort: 6379, SentinelNodeName: "s1",
		Timestamp: float64(time.Now().Unix()),
	}

	count, err := s.RecordEvent(ctx, event)
	if err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if count != 1 {
		t.Fatalf("first record count = %d, want 1", count)
	}

	count, err = s.RecordEvent(ctx, event)
	if err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if count != 2 {
		t.Fatalf("second record count = %d, want 2", count)
	}
}

func TestMemoryStore_ClusterCRUD(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore(30)

	cluster := &models.Cluster{
		GroupName: "test-group", MasterName: "test-master",
		SentinelAddrs: []string{"127.0.0.1:26379"},
		DNSProvider:   "bind",
		PrimaryDNS:    models.DNSMapping{Zone: "example.com", RecordName: "primary", RecordType: "A", TTL: 3},
	}

	if err := s.SaveCluster(ctx, cluster); err != nil {
		t.Fatalf("SaveCluster: %v", err)
	}

	got, err := s.GetCluster(ctx, "test-master")
	if err != nil {
		t.Fatalf("GetCluster: %v", err)
	}
	if got.GroupName != "test-group" {
		t.Fatalf("GroupName = %q, want %q", got.GroupName, "test-group")
	}

	clusters, err := s.ListClusters(ctx)
	if err != nil {
		t.Fatalf("ListClusters: %v", err)
	}
	if len(clusters) != 1 {
		t.Fatalf("ListClusters len = %d, want 1", len(clusters))
	}

	removed, err := s.DeleteCluster(ctx, "test-master")
	if err != nil {
		t.Fatalf("DeleteCluster: %v", err)
	}
	if !removed {
		t.Fatal("DeleteCluster returned false")
	}

	_, err = s.GetCluster(ctx, "test-master")
	if err != ErrNotFound {
		t.Fatalf("GetCluster after delete: got err=%v, want ErrNotFound", err)
	}
}

func TestMemoryStore_AcquireReleaseLock(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore(30)

	acquired, err := s.AcquireLock(ctx, "lock-1", 5*time.Second)
	if err != nil || !acquired {
		t.Fatalf("first AcquireLock: acquired=%v, err=%v", acquired, err)
	}

	acquired, err = s.AcquireLock(ctx, "lock-1", 5*time.Second)
	if err != nil || acquired {
		t.Fatalf("second AcquireLock should fail: acquired=%v, err=%v", acquired, err)
	}

	if err := s.ReleaseLock(ctx, "lock-1"); err != nil {
		t.Fatalf("ReleaseLock: %v", err)
	}

	acquired, err = s.AcquireLock(ctx, "lock-1", 5*time.Second)
	if err != nil || !acquired {
		t.Fatalf("AcquireLock after release: acquired=%v, err=%v", acquired, err)
	}
}

func TestMemoryStore_AdminPasswordHash(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore(30)

	hash, err := s.GetAdminPasswordHash(ctx)
	if err != nil || hash != "" {
		t.Fatalf("initial hash = %q, err=%v, want empty", hash, err)
	}

	if err := s.SetAdminPasswordHash(ctx, "salted$hashed"); err != nil {
		t.Fatalf("SetAdminPasswordHash: %v", err)
	}

	hash, err = s.GetAdminPasswordHash(ctx)
	if err != nil || hash != "salted$hashed" {
		t.Fatalf("hash = %q, err=%v", hash, err)
	}
}

func TestMemoryStore_SentinelCRUD(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore(30)

	s1 := &models.Sentinel{SentinelNodeName: "s1", GroupName: "grp-a", Host: "10.0.0.1", Port: 26379}
	s2 := &models.Sentinel{SentinelNodeName: "s2", GroupName: "grp-a", Host: "10.0.0.2", Port: 26379}
	s3 := &models.Sentinel{SentinelNodeName: "s3", GroupName: "grp-b", Host: "10.0.0.3", Port: 26379}

	for _, sn := range []*models.Sentinel{s1, s2, s3} {
		if err := s.SaveSentinel(ctx, sn); err != nil {
			t.Fatalf("SaveSentinel(%s): %v", sn.SentinelNodeName, err)
		}
	}

	got, err := s.GetSentinel(ctx, "s1")
	if err != nil {
		t.Fatalf("GetSentinel: %v", err)
	}
	if got.Host != "10.0.0.1" {
		t.Fatalf("Host = %q, want 10.0.0.1", got.Host)
	}

	// List all.
	all, err := s.ListSentinels(ctx, "")
	if err != nil {
		t.Fatalf("ListSentinels all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("ListSentinels all len = %d, want 3", len(all))
	}

	// List by group.
	grpA, err := s.ListSentinels(ctx, "grp-a")
	if err != nil {
		t.Fatalf("ListSentinels grp-a: %v", err)
	}
	if len(grpA) != 2 {
		t.Fatalf("ListSentinels grp-a len = %d, want 2", len(grpA))
	}

	// Delete.
	removed, err := s.DeleteSentinel(ctx, "s1")
	if err != nil || !removed {
		t.Fatalf("DeleteSentinel: removed=%v, err=%v", removed, err)
	}
	removed, err = s.DeleteSentinel(ctx, "s1")
	if err != nil || removed {
		t.Fatalf("DeleteSentinel again: removed=%v, err=%v", removed, err)
	}

	_, err = s.GetSentinel(ctx, "s1")
	if err != ErrNotFound {
		t.Fatalf("GetSentinel after delete: err=%v, want ErrNotFound", err)
	}
}

func TestMemoryStore_UpdateSentinelLastSeen(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore(30)

	sn := &models.Sentinel{SentinelNodeName: "s1", GroupName: "g1", Host: "10.0.0.1", Port: 26379}
	s.SaveSentinel(ctx, sn)

	ts := 1234567890.123
	s.UpdateSentinelLastSeen(ctx, "s1", ts)

	got, _ := s.GetSentinel(ctx, "s1")
	if got.LastSeen != ts {
		t.Fatalf("LastSeen = %f, want %f", got.LastSeen, ts)
	}
}

func TestMemoryStore_APIToken(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore(30)

	token, err := s.GetAPIToken(ctx)
	if err != nil || token != "" {
		t.Fatalf("initial token = %q, err=%v", token, err)
	}

	s.SetAPIToken(ctx, "smgr_abc123")
	token, _ = s.GetAPIToken(ctx)
	if token != "smgr_abc123" {
		t.Fatalf("token = %q, want smgr_abc123", token)
	}

	s.DeleteAPIToken(ctx)
	token, _ = s.GetAPIToken(ctx)
	if token != "" {
		t.Fatalf("token after delete = %q, want empty", token)
	}
}

func TestMemoryStore_WebhookCRUD(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore(30)

	wh := &models.WebhookEndpoint{ID: "wh_1", Name: "test-slack", Type: "slack", URL: "https://hooks.slack.com/test", Enabled: true}
	if err := s.SaveWebhook(ctx, wh); err != nil {
		t.Fatalf("SaveWebhook: %v", err)
	}

	got, err := s.GetWebhook(ctx, "wh_1")
	if err != nil {
		t.Fatalf("GetWebhook: %v", err)
	}
	if got.Name != "test-slack" {
		t.Fatalf("Name = %q, want test-slack", got.Name)
	}

	list, err := s.ListWebhooks(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListWebhooks: len=%d, err=%v", len(list), err)
	}

	s.DeleteWebhook(ctx, "wh_1")
	_, err = s.GetWebhook(ctx, "wh_1")
	if err != ErrNotFound {
		t.Fatalf("GetWebhook after delete: err=%v, want ErrNotFound", err)
	}
}

func TestMemoryStore_GetEventCount(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore(30)

	event := &models.FailoverEvent{
		GroupName: "g1", MasterName: "m1", EventType: models.EventTypeFailover,
		ToIP: "10.0.0.1", ToPort: 6379, SentinelNodeName: "s1",
		Timestamp: float64(time.Now().Unix()),
	}

	s.RecordEvent(ctx, event)
	s.RecordEvent(ctx, event)

	count, err := s.GetEventCount(ctx, event.DedupKey())
	if err != nil || count != 2 {
		t.Fatalf("GetEventCount = %d, err=%v, want 2", count, err)
	}

	count, _ = s.GetEventCount(ctx, "nonexistent-key")
	if count != 0 {
		t.Fatalf("GetEventCount for missing key = %d, want 0", count)
	}
}

func TestMemoryStore_GetRecentEvents(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore(30)

	for i := 0; i < 5; i++ {
		event := &models.FailoverEvent{
			GroupName: "g1", MasterName: "m1", EventType: models.EventTypeFailover,
			ToIP: fmt.Sprintf("10.0.0.%d", i+1), ToPort: 6379, SentinelNodeName: "s1",
			Timestamp: float64(time.Now().Unix()),
		}
		s.RecordEvent(ctx, event)
	}

	events, err := s.GetRecentEvents(ctx, 3)
	if err != nil {
		t.Fatalf("GetRecentEvents: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("len = %d, want 3", len(events))
	}
	// Most recent first.
	if events[0].ToIP != "10.0.0.5" {
		t.Fatalf("first event ToIP = %q, want 10.0.0.5", events[0].ToIP)
	}

	// Limit exceeding stored count.
	all, _ := s.GetRecentEvents(ctx, 100)
	if len(all) != 5 {
		t.Fatalf("GetRecentEvents(100) len = %d, want 5", len(all))
	}
}

func TestMemoryStore_RuntimeSettings(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore(30)

	settings, err := s.GetRuntimeSettings(ctx)
	if err != nil {
		t.Fatalf("GetRuntimeSettings: %v", err)
	}
	if len(settings) != 0 {
		t.Fatalf("initial settings len = %d, want 0", len(settings))
	}

	s.SaveRuntimeSettings(ctx, map[string]string{"sentinel_ping_interval": "10", "client_kill_enabled": "true"})
	settings, _ = s.GetRuntimeSettings(ctx)
	if settings["sentinel_ping_interval"] != "10" {
		t.Fatalf("sentinel_ping_interval = %q, want 10", settings["sentinel_ping_interval"])
	}

	// Overwrite.
	s.SaveRuntimeSettings(ctx, map[string]string{"foo": "bar"})
	settings, _ = s.GetRuntimeSettings(ctx)
	if _, ok := settings["sentinel_ping_interval"]; ok {
		t.Fatal("old key should be gone after overwrite")
	}
	if settings["foo"] != "bar" {
		t.Fatalf("foo = %q, want bar", settings["foo"])
	}
}

func TestMemoryStore_DNSProviderConfig(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore(30)

	cfg := map[string]string{"type": "cloudflare", "api_token": "secret", "zone_id": "z1"}
	if err := s.SaveDNSProviderConfig(ctx, "cf-prod", cfg); err != nil {
		t.Fatalf("SaveDNSProviderConfig: %v", err)
	}

	got, err := s.GetDNSProviderConfig(ctx, "cf-prod")
	if err != nil {
		t.Fatalf("GetDNSProviderConfig: %v", err)
	}
	if got["api_token"] != "secret" {
		t.Fatalf("api_token = %q, want secret", got["api_token"])
	}

	// List.
	all, err := s.ListDNSProviderConfigs(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("ListDNSProviderConfigs: len=%d, err=%v", len(all), err)
	}

	// Delete.
	removed, err := s.DeleteDNSProviderConfig(ctx, "cf-prod")
	if err != nil || !removed {
		t.Fatalf("DeleteDNSProviderConfig: removed=%v, err=%v", removed, err)
	}

	_, err = s.GetDNSProviderConfig(ctx, "cf-prod")
	if err != ErrNotFound {
		t.Fatalf("GetDNSProviderConfig after delete: err=%v, want ErrNotFound", err)
	}

	removed, _ = s.DeleteDNSProviderConfig(ctx, "cf-prod")
	if removed {
		t.Fatal("DeleteDNSProviderConfig of missing should return false")
	}
}

func TestMemoryStore_SlackLegacy(t *testing.T) {
	s := NewMemoryStore(30)
	ctx := context.Background()

	_, err := s.GetSlackWebhookURL(ctx)
	if err != ErrNotFound {
		t.Fatalf("GetSlackWebhookURL: err=%v, want ErrNotFound", err)
	}

	_, err = s.GetSlackChannel(ctx)
	if err != ErrNotFound {
		t.Fatalf("GetSlackChannel: err=%v, want ErrNotFound", err)
	}

	if err := s.DeleteSlackLegacy(ctx); err != nil {
		t.Fatalf("DeleteSlackLegacy: %v", err)
	}
}

func TestMemoryStore_Close(t *testing.T) {
	s := NewMemoryStore(30)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
