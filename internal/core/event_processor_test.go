package core

import (
	"context"
	"testing"
	"time"

	"github.com/chals-go/valkey-sentinel-manager/internal/models"
	"github.com/chals-go/valkey-sentinel-manager/internal/store"
)

func TestEventProcessor_QuorumMode(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(30)
	ep := NewEventProcessor(s)

	event := &models.FailoverEvent{
		GroupName: "g1", MasterName: "m1", EventType: models.EventTypeFailover,
		ToIP: "10.0.0.2", ToPort: 6379, SentinelNodeName: "s1",
		Timestamp: float64(time.Now().Unix()),
	}

	// First report: count=1, quorum=2 → should NOT update DNS.
	result, err := ep.Process(ctx, event, true, 2)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if result.ShouldUpdateDNS {
		t.Fatal("first report should not trigger DNS update in quorum mode")
	}
	if result.ReportCount != 1 {
		t.Fatalf("ReportCount = %d, want 1", result.ReportCount)
	}

	// Second report: count=2 == threshold → SHOULD update DNS.
	event2 := *event
	event2.SentinelNodeName = "s2"
	result, err = ep.Process(ctx, &event2, true, 2)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !result.ShouldUpdateDNS {
		t.Fatal("second report should trigger DNS update (quorum reached)")
	}
	if !result.QuorumReached {
		t.Fatal("QuorumReached should be true")
	}

	// Third report: count=3 > threshold → should NOT update again.
	event3 := *event
	event3.SentinelNodeName = "s3"
	result, err = ep.Process(ctx, &event3, true, 2)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if result.ShouldUpdateDNS {
		t.Fatal("third report should not trigger DNS update")
	}
}

func TestEventProcessor_FirstComeMode(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(30)
	ep := NewEventProcessor(s)

	event := &models.FailoverEvent{
		GroupName: "g2", MasterName: "m2", EventType: models.EventTypeFailover,
		ToIP: "10.0.0.5", ToPort: 6379, SentinelNodeName: "s1",
		Timestamp: float64(time.Now().Unix()),
	}

	// First report in first-come mode → SHOULD update DNS.
	result, err := ep.Process(ctx, event, false, 2)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !result.ShouldUpdateDNS {
		t.Fatal("first report in first-come mode should trigger DNS update")
	}
}

func TestEventProcessor_ReplicaDown(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(30)
	ep := NewEventProcessor(s)

	event := &models.FailoverEvent{
		GroupName: "g1", MasterName: "m1", EventType: models.EventTypeReplicaDown,
		FromIP: "10.0.0.3", FromPort: 6379, SentinelNodeName: "s1",
		Timestamp: float64(time.Now().Unix()),
	}

	result, err := ep.Process(ctx, event, true, 2)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if result.ReportCount != 1 {
		t.Fatalf("ReportCount = %d, want 1", result.ReportCount)
	}
	if result.ShouldUpdateDNS {
		t.Fatal("first report should not trigger DNS in quorum mode")
	}

	// Second sentinel reports the same event.
	event2 := *event
	event2.SentinelNodeName = "s2"
	result, err = ep.Process(ctx, &event2, true, 2)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !result.ShouldUpdateDNS {
		t.Fatal("quorum reached should trigger DNS update")
	}
}

func TestEventProcessor_ReplicaUp(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(30)
	ep := NewEventProcessor(s)

	event := &models.FailoverEvent{
		GroupName: "g1", MasterName: "m1", EventType: models.EventTypeReplicaUp,
		FromIP: "10.0.0.3", FromPort: 6379, SentinelNodeName: "s1",
		Timestamp: float64(time.Now().Unix()),
	}

	result, err := ep.Process(ctx, event, false, 2)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !result.ShouldUpdateDNS {
		t.Fatal("first-come mode should trigger DNS on first report")
	}
}

func TestEventProcessor_SentinelDown(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(30)
	ep := NewEventProcessor(s)

	event := &models.FailoverEvent{
		GroupName: "g1", SentinelNodeName: "s1", EventType: models.EventTypeSentinelDown,
		FromIP: "10.0.0.5", FromPort: 26379,
		Timestamp: float64(time.Now().Unix()),
	}

	result, err := ep.Process(ctx, event, false, 1)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !result.ShouldUpdateDNS {
		t.Fatal("sentinel_down in first-come mode should trigger")
	}
}

func TestEventProcessor_DifferentEvents(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(30)
	ep := NewEventProcessor(s)

	ts := float64(time.Now().Unix())

	e1 := &models.FailoverEvent{
		GroupName: "g1", MasterName: "m1", EventType: models.EventTypeFailover,
		ToIP: "10.0.0.2", ToPort: 6379, SentinelNodeName: "s1", Timestamp: ts,
	}
	e2 := &models.FailoverEvent{
		GroupName: "g1", MasterName: "m1", EventType: models.EventTypeReplicaDown,
		FromIP: "10.0.0.3", FromPort: 6379, SentinelNodeName: "s1", Timestamp: ts,
	}

	r1, _ := ep.Process(ctx, e1, true, 2)
	r2, _ := ep.Process(ctx, e2, true, 2)

	if r1.ReportCount != 1 {
		t.Fatalf("e1 ReportCount = %d, want 1", r1.ReportCount)
	}
	if r2.ReportCount != 1 {
		t.Fatalf("e2 ReportCount = %d, want 1 (independent from e1)", r2.ReportCount)
	}
}
