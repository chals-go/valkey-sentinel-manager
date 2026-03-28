// Package dns는 Route53, Azure DNS, REST API 등 다양한 DNS 프로바이더를 통한 DNS 레코드 관리를 제공한다.
package dns

import "context"

// Provider는 DNS 레코드 관리를 위한 인터페이스이다.
type Provider interface {
	// UpdateRecord는 단일 값 DNS 레코드를 설정한다.
	UpdateRecord(ctx context.Context, zone, name, recordType, value string, ttl int) error

	// UpdateRecordValues는 다중 값 DNS 레코드의 모든 값을 교체한다.
	UpdateRecordValues(ctx context.Context, zone, name, recordType string, values []string, ttl int) error

	// AddRecordValue는 다중 값 DNS 레코드에 값을 추가한다.
	AddRecordValue(ctx context.Context, zone, name, recordType, value string, ttl int) error

	// RemoveRecordValue는 다중 값 DNS 레코드에서 특정 값을 제거한다.
	RemoveRecordValue(ctx context.Context, zone, name, recordType, value string) error

	// DeleteRecord는 DNS 레코드 전체를 삭제한다.
	DeleteRecord(ctx context.Context, zone, name, recordType string) error

	// VerifyRecord는 DNS 레코드가 기대하는 값을 가지고 있는지 확인한다.
	VerifyRecord(ctx context.Context, zone, name, expectedValue string) (bool, error)

	// HealthCheck는 DNS 프로바이더 연결 상태를 확인한다.
	HealthCheck(ctx context.Context) error
}
