package models

import "time"

// Sentinel은 등록된 센티널 에이전트 노드의 정보를 담는 구조체이다.
type Sentinel struct {
	// SentinelNodeName은 센티널 노드를 식별하는 고유 이름이다.
	SentinelNodeName string `json:"sentinel_node_name"`
	// GroupName은 이 센티널 노드가 속한 클러스터 그룹 이름이다.
	GroupName string `json:"group_name"`
	// Host는 센티널 노드의 호스트 주소(IP 또는 도메인)이다.
	Host string `json:"host"`
	// Port는 센티널 노드의 포트 번호이다.
	Port int `json:"port"`
	// LastSeen은 센티널 노드가 마지막으로 확인된 시각(Unix 소수점 초)이다.
	LastSeen float64 `json:"last_seen"`
	// RegisteredAt은 센티널 노드가 처음 등록된 시각(Unix 소수점 초)이다.
	RegisteredAt float64 `json:"registered_at"`
}

// NewSentinel은 현재 시각을 RegisteredAt으로 설정하여 Sentinel을 생성하고 반환한다.
func NewSentinel(nodeName, groupName, host string, port int) *Sentinel {
	return &Sentinel{
		SentinelNodeName: nodeName,
		GroupName:        groupName,
		Host:             host,
		Port:             port,
		RegisteredAt:     float64(time.Now().UnixMilli()) / 1000.0,
	}
}
