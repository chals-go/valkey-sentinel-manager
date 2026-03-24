package store

import (
	"context"
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

	if err := s.RegisterCluster(ctx, cluster); err != nil {
		t.Fatalf("RegisterCluster: %v", err)
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

	removed, err := s.UnregisterCluster(ctx, "test-master")
	if err != nil {
		t.Fatalf("UnregisterCluster: %v", err)
	}
	if !removed {
		t.Fatal("UnregisterCluster returned false")
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
