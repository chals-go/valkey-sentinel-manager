// Package dns defines the DNS provider interface and implementations.
package dns

import "context"

// Provider is the interface for DNS record management.
type Provider interface {
	// UpdateRecord sets a single-value DNS record.
	UpdateRecord(ctx context.Context, zone, name, recordType, value string, ttl int) error

	// UpdateRecordValues replaces all values of a multi-value DNS record.
	UpdateRecordValues(ctx context.Context, zone, name, recordType string, values []string, ttl int) error

	// AddRecordValue appends a value to a multi-value DNS record.
	AddRecordValue(ctx context.Context, zone, name, recordType, value string, ttl int) error

	// RemoveRecordValue removes a value from a multi-value DNS record.
	RemoveRecordValue(ctx context.Context, zone, name, recordType, value string) error

	// DeleteRecord removes a DNS record entirely.
	DeleteRecord(ctx context.Context, zone, name, recordType string) error

	// VerifyRecord checks that a DNS record has the expected value.
	VerifyRecord(ctx context.Context, zone, name, expectedValue string) (bool, error)

	// HealthCheck verifies the DNS provider connection.
	HealthCheck(ctx context.Context) error
}
