package models

import "testing"

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
