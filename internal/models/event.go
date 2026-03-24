package models

import (
	"crypto/sha256"
	"fmt"
	"time"
)

// EventType defines the type of failover event.
type EventType string

const (
	EventTypeFailover     EventType = "failover"
	EventTypeReplicaDown  EventType = "replica_down"
	EventTypeReplicaUp    EventType = "replica_up"
	EventTypeSentinelDown EventType = "sentinel_down"
	EventTypeSentinelUp   EventType = "sentinel_up"
)

// FailoverEvent represents a failover event received from a Sentinel agent.
type FailoverEvent struct {
	GroupName        string    `json:"group_name"`
	MasterName       string    `json:"master_name"`
	Role             string    `json:"role"`
	State            string    `json:"state"`
	FromIP           string    `json:"from_ip"`
	FromPort         int       `json:"from_port"`
	ToIP             string    `json:"to_ip"`
	ToPort           int       `json:"to_port"`
	SentinelNodeName string    `json:"sentinel_node_name"`
	EventType        EventType `json:"event_type"`
	Timestamp        float64   `json:"timestamp"`
}

// NewFailoverEvent creates a FailoverEvent with the current timestamp.
func NewFailoverEvent(groupName, masterName, role, state, fromIP string, fromPort int, toIP string, toPort int, sentinelNodeName string, eventType EventType) *FailoverEvent {
	return &FailoverEvent{
		GroupName:        groupName,
		MasterName:       masterName,
		Role:             role,
		State:            state,
		FromIP:           fromIP,
		FromPort:         fromPort,
		ToIP:             toIP,
		ToPort:           toPort,
		SentinelNodeName: sentinelNodeName,
		EventType:        eventType,
		Timestamp:        float64(time.Now().UnixMilli()) / 1000.0,
	}
}

// DedupKey returns a SHA-256 hash for event deduplication.
//
// Hash input varies by event type:
//   - failover:     group_name:master_name:to_ip:to_port
//   - replica_down: group_name:master_name:replica_down:from_ip
//   - replica_up:   group_name:master_name:replica_up:from_ip
func (e *FailoverEvent) DedupKey() string {
	var raw string
	switch e.EventType {
	case EventTypeReplicaDown:
		raw = fmt.Sprintf("%s:%s:replica_down:%s", e.GroupName, e.MasterName, e.FromIP)
	case EventTypeReplicaUp:
		raw = fmt.Sprintf("%s:%s:replica_up:%s", e.GroupName, e.MasterName, e.FromIP)
	case EventTypeSentinelDown:
		raw = fmt.Sprintf("%s:sentinel_down:%s", e.GroupName, e.SentinelNodeName)
	case EventTypeSentinelUp:
		raw = fmt.Sprintf("%s:sentinel_up:%s", e.GroupName, e.SentinelNodeName)
	default:
		raw = fmt.Sprintf("%s:%s:%s:%d", e.GroupName, e.MasterName, e.ToIP, e.ToPort)
	}
	h := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", h)
}
