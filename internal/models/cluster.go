// Package models는 클러스터, 이벤트, 센티널 등 도메인 모델을 정의한다.
package models

// DNSMapping은 클러스터 엔드포인트의 DNS 레코드 정보를 담는 구조체이다.
type DNSMapping struct {
	// Zone은 DNS 레코드가 속한 호스팅 존 이름이다.
	Zone string `json:"zone"`
	// RecordName은 DNS 레코드의 이름(서브도메인 포함)이다.
	RecordName string `json:"record_name"`
	// RecordType은 DNS 레코드 유형으로, 예를 들어 "A" 또는 "CNAME"이다.
	RecordType string `json:"record_type"`
	// TTL은 DNS 레코드의 유효 시간(초 단위)이다.
	TTL int `json:"ttl"`
}

// Cluster는 Valkey 복제 그룹의 설정 정보를 담는 구조체이다.
type Cluster struct {
	// GroupName은 클러스터를 식별하는 고유 그룹 이름이다.
	GroupName string `json:"group_name"`
	// MasterName은 센티널이 관리하는 마스터 노드의 이름이다.
	MasterName string `json:"master_name"`
	// SentinelAddrs는 센티널 노드의 주소 목록("host:port" 형식)이다.
	SentinelAddrs []string `json:"sentinel_addrs"`
	// DNSProvider는 DNS 업데이트에 사용할 프로바이더 이름이다(예: "route53").
	DNSProvider string `json:"dns_provider"`
	// PrimaryDNS는 프라이머리(마스터) 노드에 대한 DNS 매핑 정보이다.
	PrimaryDNS DNSMapping `json:"primary_dns"`
	// PrimaryIP는 현재 프라이머리 노드의 IP 주소이다.
	PrimaryIP string `json:"primary_ip"`
	// PrimaryPort는 현재 프라이머리 노드의 포트 번호이다.
	PrimaryPort int `json:"primary_port"`
	// ReplicaDNS는 레플리카 노드에 대한 DNS 매핑 정보이며, 설정되지 않을 수 있다.
	ReplicaDNS *DNSMapping `json:"replica_dns,omitempty"`
	// MultiReplica는 복수의 레플리카를 허용하는지 여부를 나타낸다.
	MultiReplica bool `json:"multi_replica"`
	// RedisPassword는 Valkey(Redis) 노드 인증에 사용하는 비밀번호이다.
	RedisPassword string `json:"redis_password"`
	// RedisUsername은 Valkey ACL 인증에 사용하는 사용자명이다 (Valkey 7+, 빈 문자열이면 requirepass 사용).
	RedisUsername string `json:"redis_username,omitempty"`
	// SentinelPassword는 센티널 노드 인증에 사용하는 비밀번호이다.
	SentinelPassword string `json:"sentinel_password"`
	// QuorumMode가 true이면 쿼럼 기반 페일오버 로직을 활성화한다.
	QuorumMode bool `json:"quorum_mode"`
	// QuorumThreshold는 페일오버를 결정하기 위해 필요한 최소 센티널 동의 수이다.
	QuorumThreshold int `json:"quorum_threshold"`
	// SentinelNodeNames는 이 클러스터에 등록된 센티널 노드 이름 목록이다.
	SentinelNodeNames []string `json:"sentinel_node_names"`
	// IsPaused가 true이면 이 클러스터의 페일오버 처리가 일시 중지된 상태이다.
	IsPaused bool `json:"is_paused"`
	// PausedDownAfterMs는 일시 중지 상태에서 사용할 down-after-milliseconds 값이다.
	PausedDownAfterMs int `json:"paused_down_after_ms"`
}
