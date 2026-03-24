package models

import (
	"crypto/sha256"
	"fmt"
	"time"
)

// EventType은 페일오버 이벤트의 종류를 나타내는 문자열 타입이다.
type EventType string

const (
	// EventTypeFailover는 마스터 페일오버 이벤트를 나타낸다.
	EventTypeFailover EventType = "failover"
	// EventTypeReplicaDown은 레플리카 노드가 다운된 이벤트를 나타낸다.
	EventTypeReplicaDown EventType = "replica_down"
	// EventTypeReplicaUp은 레플리카 노드가 복구된 이벤트를 나타낸다.
	EventTypeReplicaUp EventType = "replica_up"
	// EventTypeSentinelDown은 센티널 노드가 다운된 이벤트를 나타낸다.
	EventTypeSentinelDown EventType = "sentinel_down"
	// EventTypeSentinelUp은 센티널 노드가 복구된 이벤트를 나타낸다.
	EventTypeSentinelUp EventType = "sentinel_up"
)

// FailoverEvent는 센티널 에이전트로부터 수신한 페일오버 이벤트를 담는 구조체이다.
type FailoverEvent struct {
	// GroupName은 이벤트가 발생한 클러스터의 그룹 이름이다.
	GroupName string `json:"group_name"`
	// MasterName은 센티널이 관리하는 마스터 이름이다.
	MasterName string `json:"master_name"`
	// Role은 이벤트와 관련된 노드의 역할(예: "master", "replica")이다.
	Role string `json:"role"`
	// State는 이벤트 발생 시점의 상태 문자열이다.
	State string `json:"state"`
	// FromIP는 페일오버 이전 노드의 IP 주소이다.
	FromIP string `json:"from_ip"`
	// FromPort는 페일오버 이전 노드의 포트 번호이다.
	FromPort int `json:"from_port"`
	// ToIP는 페일오버 이후 새 마스터 노드의 IP 주소이다.
	ToIP string `json:"to_ip"`
	// ToPort는 페일오버 이후 새 마스터 노드의 포트 번호이다.
	ToPort int `json:"to_port"`
	// SentinelNodeName은 이벤트를 보고한 센티널 노드의 이름이다.
	SentinelNodeName string `json:"sentinel_node_name"`
	// EventType은 이벤트의 종류를 나타낸다.
	EventType EventType `json:"event_type"`
	// Timestamp는 이벤트 발생 시각을 Unix 밀리초 기반 소수점 초로 표현한 값이다.
	Timestamp float64 `json:"timestamp"`
}

// NewFailoverEvent는 현재 타임스탬프를 설정하여 FailoverEvent를 생성하고 반환한다.
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

// DedupKey는 이벤트 중복 제거를 위한 SHA-256 해시 문자열을 반환한다.
//
// 이벤트 유형에 따라 해시 입력값이 달라진다:
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
