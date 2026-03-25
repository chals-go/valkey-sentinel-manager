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

// MemoryStore는 개발 및 테스트 용도의 인메모리 저장소 구현체이다.
// 프로세스가 종료되면 모든 데이터는 소실된다.
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
	webhooks      map[string]*models.WebhookEndpoint
	runtimeConfig map[string]string
	dnsConfigs    map[string]map[string]string
}

// NewMemoryStore는 새로운 인메모리 저장소를 생성하여 반환한다.
// dedupWindowSeconds는 이벤트 중복 제거 윈도우 크기(초 단위)이다.
func NewMemoryStore(dedupWindowSeconds int) *MemoryStore {
	return &MemoryStore{
		dedupWindow:   float64(dedupWindowSeconds),
		events:        make(map[string]*dedupEntry),
		locks:         make(map[string]float64),
		sentinels:     make(map[string]*models.Sentinel),
		clusters:      make(map[string]*models.Cluster),
		webhooks:      make(map[string]*models.WebhookEndpoint),
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

// RecordEvent는 페일오버 이벤트를 기록하고 현재 보고 횟수를 반환한다.
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

// GetEventCount는 지정한 중복 제거 키에 대한 현재 보고 횟수를 반환한다.
func (m *MemoryStore) GetEventCount(_ context.Context, dedupKey string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cleanupExpired(nowUnix())
	if entry, ok := m.events[dedupKey]; ok {
		return entry.count, nil
	}
	return 0, nil
}

// GetRecentEvents는 최근 이벤트를 limit 개수만큼 반환한다.
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

// AcquireLock은 지정한 키에 대한 분산 락을 획득한다.
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

// ReleaseLock은 지정한 키에 대한 분산 락을 해제한다.
func (m *MemoryStore) ReleaseLock(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.locks, key)
	return nil
}

// RegisterSentinel은 센티널 노드를 등록한다.
func (m *MemoryStore) RegisterSentinel(_ context.Context, s *models.Sentinel) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sentinels[s.SentinelNodeName] = s
	return nil
}

// UnregisterSentinel은 센티널 노드를 제거한다.
func (m *MemoryStore) UnregisterSentinel(_ context.Context, name string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.sentinels[name]; ok {
		delete(m.sentinels, name)
		return true, nil
	}
	return false, nil
}

// GetSentinel은 이름으로 센티널을 조회한다.
func (m *MemoryStore) GetSentinel(_ context.Context, name string) (*models.Sentinel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sentinels[name]
	if !ok {
		return nil, ErrNotFound
	}
	return s, nil
}

// ListSentinels는 모든 센티널 목록을 반환한다. groupName이 비어있으면 전체를 반환한다.
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

// UpdateSentinelLastSeen은 센티널의 마지막 접속 시각을 갱신한다.
func (m *MemoryStore) UpdateSentinelLastSeen(_ context.Context, name string, timestamp float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.sentinels[name]; ok {
		s.LastSeen = timestamp
	}
	return nil
}

// RegisterCluster는 master_name을 고유 키로 사용하여 클러스터를 등록한다.
func (m *MemoryStore) RegisterCluster(_ context.Context, c *models.Cluster) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.clusters[c.MasterName] = c
	return nil
}

// UnregisterCluster는 master_name으로 클러스터를 제거한다.
func (m *MemoryStore) UnregisterCluster(_ context.Context, masterName string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.clusters[masterName]; ok {
		delete(m.clusters, masterName)
		return true, nil
	}
	return false, nil
}

// GetCluster는 master_name으로 클러스터를 조회한다.
func (m *MemoryStore) GetCluster(_ context.Context, masterName string) (*models.Cluster, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	c, ok := m.clusters[masterName]
	if !ok {
		return nil, ErrNotFound
	}
	return c, nil
}

// ListClusters는 등록된 모든 클러스터를 반환한다.
func (m *MemoryStore) ListClusters(_ context.Context) ([]*models.Cluster, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]*models.Cluster, 0, len(m.clusters))
	for _, c := range m.clusters {
		result = append(result, c)
	}
	return result, nil
}

// GetAdminPasswordHash는 저장된 관리자 비밀번호 해시를 반환한다.
func (m *MemoryStore) GetAdminPasswordHash(_ context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.adminPwHash, nil
}

// SetAdminPasswordHash는 관리자 비밀번호 해시를 저장한다.
func (m *MemoryStore) SetAdminPasswordHash(_ context.Context, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.adminPwHash = hash
	return nil
}

// GetAPIToken은 저장된 API 토큰을 반환한다.
func (m *MemoryStore) GetAPIToken(_ context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.apiToken, nil
}

// SetAPIToken은 API 토큰을 저장한다.
func (m *MemoryStore) SetAPIToken(_ context.Context, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.apiToken = token
	return nil
}

// DeleteAPIToken은 API 토큰을 삭제한다.
func (m *MemoryStore) DeleteAPIToken(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.apiToken = ""
	return nil
}

// SaveWebhook은 웹훅 엔드포인트를 ID를 키로 하여 저장한다.
func (m *MemoryStore) SaveWebhook(_ context.Context, wh *models.WebhookEndpoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.webhooks[wh.ID] = wh
	return nil
}

// GetWebhook은 ID로 웹훅 엔드포인트를 조회한다. 없으면 ErrNotFound를 반환한다.
func (m *MemoryStore) GetWebhook(_ context.Context, id string) (*models.WebhookEndpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	wh, ok := m.webhooks[id]
	if !ok {
		return nil, ErrNotFound
	}
	return wh, nil
}

// ListWebhooks는 저장된 모든 웹훅 엔드포인트를 반환한다.
func (m *MemoryStore) ListWebhooks(_ context.Context) ([]*models.WebhookEndpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*models.WebhookEndpoint, 0, len(m.webhooks))
	for _, wh := range m.webhooks {
		result = append(result, wh)
	}
	return result, nil
}

// DeleteWebhook은 ID로 웹훅 엔드포인트를 삭제한다.
func (m *MemoryStore) DeleteWebhook(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.webhooks, id)
	return nil
}

// GetSlackWebhookURL은 마이그레이션 레거시 메서드로, 인메모리 저장소에는 레거시 데이터가 없으므로 ErrNotFound를 반환한다.
func (m *MemoryStore) GetSlackWebhookURL(_ context.Context) (string, error) {
	return "", ErrNotFound
}

// GetSlackChannel은 마이그레이션 레거시 메서드로, 인메모리 저장소에는 레거시 데이터가 없으므로 ErrNotFound를 반환한다.
func (m *MemoryStore) GetSlackChannel(_ context.Context) (string, error) {
	return "", ErrNotFound
}

// DeleteSlackLegacy는 마이그레이션 레거시 메서드로, 인메모리 저장소에서는 아무 동작도 하지 않는다.
func (m *MemoryStore) DeleteSlackLegacy(_ context.Context) error {
	return nil
}

// GetRuntimeSettings는 저장된 런타임 설정을 반환한다.
func (m *MemoryStore) GetRuntimeSettings(_ context.Context) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make(map[string]string, len(m.runtimeConfig))
	for k, v := range m.runtimeConfig {
		result[k] = v
	}
	return result, nil
}

// SaveRuntimeSettings는 런타임 설정 전체를 교체하여 저장한다.
func (m *MemoryStore) SaveRuntimeSettings(_ context.Context, settings map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.runtimeConfig = make(map[string]string, len(settings))
	for k, v := range settings {
		m.runtimeConfig[k] = v
	}
	return nil
}

// SaveDNSProviderConfig는 DNS 공급자 설정을 저장한다.
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

// GetDNSProviderConfig는 지정한 DNS 공급자의 설정을 반환한다.
func (m *MemoryStore) GetDNSProviderConfig(_ context.Context, name string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, ok := m.dnsConfigs[name]
	if !ok {
		return nil, ErrNotFound
	}
	return cfg, nil
}

// ListDNSProviderConfigs는 모든 DNS 공급자 설정을 반환한다.
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

// DeleteDNSProviderConfig는 지정한 DNS 공급자 설정을 삭제한다.
func (m *MemoryStore) DeleteDNSProviderConfig(_ context.Context, name string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.dnsConfigs[name]; ok {
		delete(m.dnsConfigs, name)
		return true, nil
	}
	return false, nil
}

// Close는 인메모리 저장소에서는 아무 동작도 하지 않는다.
func (m *MemoryStore) Close() error {
	return nil
}
