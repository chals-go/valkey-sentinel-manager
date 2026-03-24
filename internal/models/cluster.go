// Package models defines the domain data structures.
package models

// DNSMapping holds DNS record information for a cluster endpoint.
type DNSMapping struct {
	Zone       string `json:"zone"`
	RecordName string `json:"record_name"`
	RecordType string `json:"record_type"`
	TTL        int    `json:"ttl"`
}

// Cluster represents a Valkey replication group configuration.
type Cluster struct {
	GroupName         string      `json:"group_name"`
	MasterName        string      `json:"master_name"`
	SentinelAddrs     []string    `json:"sentinel_addrs"`
	DNSProvider       string      `json:"dns_provider"`
	PrimaryDNS        DNSMapping  `json:"primary_dns"`
	PrimaryIP         string      `json:"primary_ip"`
	PrimaryPort       int         `json:"primary_port"`
	ReplicaDNS        *DNSMapping `json:"replica_dns,omitempty"`
	MultiReplica      bool        `json:"multi_replica"`
	RedisPassword     string      `json:"redis_password"`
	SentinelPassword  string      `json:"sentinel_password"`
	QuorumMode        bool        `json:"quorum_mode"`
	QuorumThreshold   int         `json:"quorum_threshold"`
	SentinelNodeNames []string    `json:"sentinel_node_names"`
	IsPaused          bool        `json:"is_paused"`
	PausedDownAfterMs int         `json:"paused_down_after_ms"`
}
