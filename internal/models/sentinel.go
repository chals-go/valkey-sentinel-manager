package models

import "time"

// Sentinel represents a registered Sentinel agent node.
type Sentinel struct {
	SentinelNodeName string  `json:"sentinel_node_name"`
	GroupName        string  `json:"group_name"`
	Host             string  `json:"host"`
	Port             int     `json:"port"`
	LastSeen         float64 `json:"last_seen"`
	RegisteredAt     float64 `json:"registered_at"`
}

// NewSentinel creates a Sentinel with the current timestamp as RegisteredAt.
func NewSentinel(nodeName, groupName, host string, port int) *Sentinel {
	return &Sentinel{
		SentinelNodeName: nodeName,
		GroupName:        groupName,
		Host:             host,
		Port:             port,
		RegisteredAt:     float64(time.Now().UnixMilli()) / 1000.0,
	}
}
