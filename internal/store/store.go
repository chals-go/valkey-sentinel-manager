// Package store defines the storage interface and its implementations.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/chals-go/valkey-sentinel-manager/internal/models"
)

// ErrNotFound indicates that the requested resource does not exist.
var ErrNotFound = errors.New("not found")

// Store is the interface for all data persistence operations.
type Store interface {
	// Events
	RecordEvent(ctx context.Context, event *models.FailoverEvent) (int, error)
	GetEventCount(ctx context.Context, dedupKey string) (int, error)
	GetRecentEvents(ctx context.Context, limit int) ([]*models.FailoverEvent, error)

	// Distributed lock
	AcquireLock(ctx context.Context, key string, ttl time.Duration) (bool, error)
	ReleaseLock(ctx context.Context, key string) error

	// Sentinels
	RegisterSentinel(ctx context.Context, s *models.Sentinel) error
	UnregisterSentinel(ctx context.Context, name string) (bool, error)
	GetSentinel(ctx context.Context, name string) (*models.Sentinel, error)
	ListSentinels(ctx context.Context, groupName string) ([]*models.Sentinel, error)
	UpdateSentinelLastSeen(ctx context.Context, name string, timestamp float64) error

	// Clusters
	RegisterCluster(ctx context.Context, c *models.Cluster) error
	UnregisterCluster(ctx context.Context, masterName string) (bool, error)
	GetCluster(ctx context.Context, masterName string) (*models.Cluster, error)
	ListClusters(ctx context.Context) ([]*models.Cluster, error)

	// Admin auth
	GetAdminPasswordHash(ctx context.Context) (string, error)
	SetAdminPasswordHash(ctx context.Context, hash string) error

	// API token
	GetAPIToken(ctx context.Context) (string, error)
	SetAPIToken(ctx context.Context, token string) error
	DeleteAPIToken(ctx context.Context) error

	// Slack
	GetSlackWebhookURL(ctx context.Context) (string, error)
	SetSlackWebhookURL(ctx context.Context, url string) error
	DeleteSlackWebhookURL(ctx context.Context) error
	GetSlackChannel(ctx context.Context) (string, error)
	SetSlackChannel(ctx context.Context, channel string) error

	// Runtime settings
	GetRuntimeSettings(ctx context.Context) (map[string]string, error)
	SaveRuntimeSettings(ctx context.Context, settings map[string]string) error

	// DNS provider config
	SaveDNSProviderConfig(ctx context.Context, name string, cfg map[string]string) error
	GetDNSProviderConfig(ctx context.Context, name string) (map[string]string, error)
	ListDNSProviderConfigs(ctx context.Context) (map[string]map[string]string, error)
	DeleteDNSProviderConfig(ctx context.Context, name string) (bool, error)

	// Lifecycle
	Close() error
}
