package store

import (
	"context"
	"sync"
	"time"

	"github.com/chals-go/valkey-sentinel-manager/internal/models"
)

const eventHistoryMax = 500

type dedupEntry struct {
	count     int
	event     *models.FailoverEvent
	expiresAt float64
}

// MemoryStore is an in-memory Store implementation for development and testing.
// Data is lost when the process exits.
type MemoryStore struct {
	dedupWindow float64 // seconds

	mu            sync.Mutex
	events        map[string]*dedupEntry
	eventHistory  []*models.FailoverEvent
	locks         map[string]float64 // key -> expires_at
	sentinels     map[string]*models.Sentinel
	clusters      map[string]*models.Cluster
	adminPwHash   string
	apiToken      string
	slackWebhook  string
	slackChannel  string
	runtimeConfig map[string]string
	dnsConfigs    map[string]map[string]string
}

// NewMemoryStore creates a new in-memory store.
func NewMemoryStore(dedupWindowSeconds int) *MemoryStore {
	return &MemoryStore{
		dedupWindow:   float64(dedupWindowSeconds),
		events:        make(map[string]*dedupEntry),
		locks:         make(map[string]float64),
		sentinels:     make(map[string]*models.Sentinel),
		clusters:      make(map[string]*models.Cluster),
		runtimeConfig: make(map[string]string),
		dnsConfigs:    make(map[string]map[string]string),
	}
}

func nowUnix() float64 {
	return float64(time.Now().UnixMilli()) / 1000.0
}

func (m *MemoryStore) cleanupExpired(now float64) {
	for k, v := range m.events {
		if v.expiresAt <= now {
			delete(m.events, k)
		}
	}
}

// RecordEvent records a failover event and returns the current report count.
func (m *MemoryStore) RecordEvent(_ context.Context, event *models.FailoverEvent) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := nowUnix()
	m.cleanupExpired(now)

	key := event.DedupKey()
	if entry, ok := m.events[key]; ok {
		entry.count++
		return entry.count, nil
	}

	m.events[key] = &dedupEntry{
		count:     1,
		event:     event,
		expiresAt: now + m.dedupWindow,
	}

	m.eventHistory = append([]*models.FailoverEvent{event}, m.eventHistory...)
	if len(m.eventHistory) > eventHistoryMax {
		m.eventHistory = m.eventHistory[:eventHistoryMax]
	}
	return 1, nil
}

// GetEventCount returns the current report count for a dedup key.
func (m *MemoryStore) GetEventCount(_ context.Context, dedupKey string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cleanupExpired(nowUnix())
	if entry, ok := m.events[dedupKey]; ok {
		return entry.count, nil
	}
	return 0, nil
}

// GetRecentEvents returns the most recent events up to limit.
func (m *MemoryStore) GetRecentEvents(_ context.Context, limit int) ([]*models.FailoverEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if limit > len(m.eventHistory) {
		limit = len(m.eventHistory)
	}
	result := make([]*models.FailoverEvent, limit)
	copy(result, m.eventHistory[:limit])
	return result, nil
}

// AcquireLock acquires a distributed lock.
func (m *MemoryStore) AcquireLock(_ context.Context, key string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := nowUnix()
	if exp, ok := m.locks[key]; ok && exp > now {
		return false, nil
	}
	m.locks[key] = now + ttl.Seconds()
	return true, nil
}

// ReleaseLock releases a distributed lock.
func (m *MemoryStore) ReleaseLock(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.locks, key)
	return nil
}

// RegisterSentinel registers a sentinel node.
func (m *MemoryStore) RegisterSentinel(_ context.Context, s *models.Sentinel) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sentinels[s.SentinelNodeName] = s
	return nil
}

// UnregisterSentinel removes a sentinel node.
func (m *MemoryStore) UnregisterSentinel(_ context.Context, name string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.sentinels[name]; ok {
		delete(m.sentinels, name)
		return true, nil
	}
	return false, nil
}

// GetSentinel returns a sentinel by name.
func (m *MemoryStore) GetSentinel(_ context.Context, name string) (*models.Sentinel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sentinels[name]
	if !ok {
		return nil, ErrNotFound
	}
	return s, nil
}

// ListSentinels returns all sentinels, optionally filtered by group name.
// Pass empty string for groupName to list all.
func (m *MemoryStore) ListSentinels(_ context.Context, groupName string) ([]*models.Sentinel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []*models.Sentinel
	for _, s := range m.sentinels {
		if groupName != "" && s.GroupName != groupName {
			continue
		}
		result = append(result, s)
	}
	return result, nil
}

// UpdateSentinelLastSeen updates the last seen timestamp.
func (m *MemoryStore) UpdateSentinelLastSeen(_ context.Context, name string, timestamp float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.sentinels[name]; ok {
		s.LastSeen = timestamp
	}
	return nil
}

// RegisterCluster registers a cluster using master_name as the unique key.
func (m *MemoryStore) RegisterCluster(_ context.Context, c *models.Cluster) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.clusters[c.MasterName] = c
	return nil
}

// UnregisterCluster removes a cluster by master name.
func (m *MemoryStore) UnregisterCluster(_ context.Context, masterName string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.clusters[masterName]; ok {
		delete(m.clusters, masterName)
		return true, nil
	}
	return false, nil
}

// GetCluster returns a cluster by master name.
func (m *MemoryStore) GetCluster(_ context.Context, masterName string) (*models.Cluster, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	c, ok := m.clusters[masterName]
	if !ok {
		return nil, ErrNotFound
	}
	return c, nil
}

// ListClusters returns all registered clusters.
func (m *MemoryStore) ListClusters(_ context.Context) ([]*models.Cluster, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]*models.Cluster, 0, len(m.clusters))
	for _, c := range m.clusters {
		result = append(result, c)
	}
	return result, nil
}

// GetAdminPasswordHash returns the stored admin password hash.
func (m *MemoryStore) GetAdminPasswordHash(_ context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.adminPwHash, nil
}

// SetAdminPasswordHash stores the admin password hash.
func (m *MemoryStore) SetAdminPasswordHash(_ context.Context, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.adminPwHash = hash
	return nil
}

// GetAPIToken returns the stored API token.
func (m *MemoryStore) GetAPIToken(_ context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.apiToken, nil
}

// SetAPIToken stores the API token.
func (m *MemoryStore) SetAPIToken(_ context.Context, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.apiToken = token
	return nil
}

// DeleteAPIToken removes the API token.
func (m *MemoryStore) DeleteAPIToken(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.apiToken = ""
	return nil
}

// GetSlackWebhookURL returns the stored Slack webhook URL.
func (m *MemoryStore) GetSlackWebhookURL(_ context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.slackWebhook, nil
}

// SetSlackWebhookURL stores the Slack webhook URL.
func (m *MemoryStore) SetSlackWebhookURL(_ context.Context, url string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.slackWebhook = url
	return nil
}

// DeleteSlackWebhookURL removes the Slack webhook URL.
func (m *MemoryStore) DeleteSlackWebhookURL(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.slackWebhook = ""
	return nil
}

// GetSlackChannel returns the stored Slack channel.
func (m *MemoryStore) GetSlackChannel(_ context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.slackChannel, nil
}

// SetSlackChannel stores the Slack channel name.
func (m *MemoryStore) SetSlackChannel(_ context.Context, channel string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.slackChannel = channel
	return nil
}

// GetRuntimeSettings returns the stored runtime settings.
func (m *MemoryStore) GetRuntimeSettings(_ context.Context) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make(map[string]string, len(m.runtimeConfig))
	for k, v := range m.runtimeConfig {
		result[k] = v
	}
	return result, nil
}

// SaveRuntimeSettings replaces all runtime settings.
func (m *MemoryStore) SaveRuntimeSettings(_ context.Context, settings map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.runtimeConfig = make(map[string]string, len(settings))
	for k, v := range settings {
		m.runtimeConfig[k] = v
	}
	return nil
}

// SaveDNSProviderConfig stores a DNS provider configuration.
func (m *MemoryStore) SaveDNSProviderConfig(_ context.Context, name string, cfg map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	c := make(map[string]string, len(cfg))
	for k, v := range cfg {
		c[k] = v
	}
	m.dnsConfigs[name] = c
	return nil
}

// GetDNSProviderConfig returns the configuration for a DNS provider.
func (m *MemoryStore) GetDNSProviderConfig(_ context.Context, name string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, ok := m.dnsConfigs[name]
	if !ok {
		return nil, ErrNotFound
	}
	return cfg, nil
}

// ListDNSProviderConfigs returns all DNS provider configurations.
func (m *MemoryStore) ListDNSProviderConfigs(_ context.Context) (map[string]map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make(map[string]map[string]string, len(m.dnsConfigs))
	for name, cfg := range m.dnsConfigs {
		c := make(map[string]string, len(cfg))
		for k, v := range cfg {
			c[k] = v
		}
		result[name] = c
	}
	return result, nil
}

// DeleteDNSProviderConfig removes a DNS provider configuration.
func (m *MemoryStore) DeleteDNSProviderConfig(_ context.Context, name string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.dnsConfigs[name]; ok {
		delete(m.dnsConfigs, name)
		return true, nil
	}
	return false, nil
}

// Close is a no-op for the in-memory store.
func (m *MemoryStore) Close() error {
	return nil
}
