package models

import (
	"testing"
	"time"
)

func TestDedupKey(t *testing.T) {
	tests := []struct {
		name   string
		event  FailoverEvent
		expect string // just check non-empty and consistency
	}{
		{
			name: "failover key uses to_ip and to_port",
			event: FailoverEvent{
				GroupName: "g1", MasterName: "m1", EventType: EventTypeFailover,
				ToIP: "10.0.0.2", ToPort: 6379,
			},
		},
		{
			name: "replica_down key uses from_ip",
			event: FailoverEvent{
				GroupName: "g1", MasterName: "m1", EventType: EventTypeReplicaDown,
				FromIP: "10.0.0.3",
			},
		},
		{
			name: "replica_up key uses from_ip",
			event: FailoverEvent{
				GroupName: "g1", MasterName: "m1", EventType: EventTypeReplicaUp,
				FromIP: "10.0.0.3",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := tt.event.DedupKey()
			if key == "" {
				t.Fatal("DedupKey() returned empty string")
			}
			if len(key) != 64 { // SHA-256 hex = 64 chars
				t.Fatalf("DedupKey() len = %d, want 64", len(key))
			}
		})
	}

	// Same input should produce the same key.
	e1 := FailoverEvent{GroupName: "g1", MasterName: "m1", EventType: EventTypeFailover, ToIP: "1.2.3.4", ToPort: 6379}
	e2 := FailoverEvent{GroupName: "g1", MasterName: "m1", EventType: EventTypeFailover, ToIP: "1.2.3.4", ToPort: 6379}
	if e1.DedupKey() != e2.DedupKey() {
		t.Fatal("identical events should produce the same dedup key")
	}

	// Different event types with same group/master should differ.
	eDown := FailoverEvent{GroupName: "g1", MasterName: "m1", EventType: EventTypeReplicaDown, FromIP: "1.2.3.4"}
	eUp := FailoverEvent{GroupName: "g1", MasterName: "m1", EventType: EventTypeReplicaUp, FromIP: "1.2.3.4"}
	if eDown.DedupKey() == eUp.DedupKey() {
		t.Fatal("replica_down and replica_up with same IP should have different dedup keys")
	}
}

func TestDedupKey_SentinelDown(t *testing.T) {
	e := FailoverEvent{GroupName: "g1", SentinelNodeName: "s1", EventType: EventTypeSentinelDown}
	key := e.DedupKey()
	if key == "" || len(key) != 64 {
		t.Fatalf("DedupKey() = %q, want 64-char hex", key)
	}
	// Same input → same key.
	e2 := FailoverEvent{GroupName: "g1", SentinelNodeName: "s1", EventType: EventTypeSentinelDown}
	if key != e2.DedupKey() {
		t.Fatal("identical sentinel_down events should have same key")
	}
}

func TestDedupKey_SentinelUp(t *testing.T) {
	eDown := FailoverEvent{GroupName: "g1", SentinelNodeName: "s1", EventType: EventTypeSentinelDown}
	eUp := FailoverEvent{GroupName: "g1", SentinelNodeName: "s1", EventType: EventTypeSentinelUp}
	if eDown.DedupKey() == eUp.DedupKey() {
		t.Fatal("sentinel_down and sentinel_up should have different keys")
	}
}

func TestNewFailoverEvent(t *testing.T) {
	before := float64(time.Now().UnixMilli()) / 1000.0
	e := NewFailoverEvent("g1", "m1", "leader", "promoted", "10.0.0.1", 6379, "10.0.0.2", 6379, "s1", EventTypeFailover)
	after := float64(time.Now().UnixMilli()) / 1000.0

	if e.GroupName != "g1" || e.MasterName != "m1" {
		t.Fatalf("GroupName=%q, MasterName=%q", e.GroupName, e.MasterName)
	}
	if e.Timestamp < before || e.Timestamp > after {
		t.Fatalf("Timestamp %f not in range [%f, %f]", e.Timestamp, before, after)
	}
	if e.EventType != EventTypeFailover {
		t.Fatalf("EventType = %q, want failover", e.EventType)
	}
}

func TestNewSentinel(t *testing.T) {
	before := float64(time.Now().UnixMilli()) / 1000.0
	s := NewSentinel("s1", "g1", "10.0.0.1", 26379)
	after := float64(time.Now().UnixMilli()) / 1000.0

	if s.SentinelNodeName != "s1" || s.GroupName != "g1" {
		t.Fatalf("SentinelNodeName=%q, GroupName=%q", s.SentinelNodeName, s.GroupName)
	}
	if s.RegisteredAt < before || s.RegisteredAt > after {
		t.Fatalf("RegisteredAt %f not in range [%f, %f]", s.RegisteredAt, before, after)
	}
}
