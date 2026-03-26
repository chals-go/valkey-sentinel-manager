// Package store는 클러스터, 센티널, 이벤트 등의 데이터를 저장하고 조회하는 저장소 인터페이스와 구현체를 제공한다.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/chals-go/valkey-sentinel-manager/internal/models"
)

// ErrNotFound는 요청한 리소스가 존재하지 않을 때 반환되는 오류이다.
var ErrNotFound = errors.New("not found")

// Store는 모든 데이터 영속성 작업을 위한 저장소 인터페이스이다.
type Store interface {
	// 이벤트

	// RecordEvent는 페일오버 이벤트를 기록하고 현재 보고 횟수를 반환한다.
	RecordEvent(ctx context.Context, event *models.FailoverEvent) (int, error)
	// GetEventCount는 지정한 중복 제거 키에 대한 현재 보고 횟수를 반환한다.
	GetEventCount(ctx context.Context, dedupKey string) (int, error)
	// GetRecentEvents는 최근 이벤트를 limit 개수만큼 반환한다.
	GetRecentEvents(ctx context.Context, limit int) ([]*models.FailoverEvent, error)

	// 분산 락

	// AcquireLock은 지정한 키에 대한 분산 락을 획득한다.
	AcquireLock(ctx context.Context, key string, ttl time.Duration) (bool, error)
	// ReleaseLock은 지정한 키에 대한 분산 락을 해제한다.
	ReleaseLock(ctx context.Context, key string) error

	// 센티널

	// SaveSentinel은 센티널 노드를 등록한다.
	SaveSentinel(ctx context.Context, s *models.Sentinel) error
	// DeleteSentinel은 센티널 노드를 제거한다.
	DeleteSentinel(ctx context.Context, name string) (bool, error)
	// GetSentinel은 이름으로 센티널을 조회한다.
	GetSentinel(ctx context.Context, name string) (*models.Sentinel, error)
	// ListSentinels는 모든 센티널 목록을 반환한다. groupName이 비어있으면 전체를 반환한다.
	ListSentinels(ctx context.Context, groupName string) ([]*models.Sentinel, error)
	// UpdateSentinelLastSeen은 센티널의 마지막 접속 시각을 갱신한다.
	UpdateSentinelLastSeen(ctx context.Context, name string, timestamp float64) error

	// 클러스터

	// SaveCluster는 클러스터를 등록한다.
	SaveCluster(ctx context.Context, c *models.Cluster) error
	// DeleteCluster는 master_name으로 클러스터를 제거한다.
	DeleteCluster(ctx context.Context, masterName string) (bool, error)
	// GetCluster는 master_name으로 클러스터를 조회한다.
	GetCluster(ctx context.Context, masterName string) (*models.Cluster, error)
	// ListClusters는 등록된 모든 클러스터를 반환한다.
	ListClusters(ctx context.Context) ([]*models.Cluster, error)

	// 관리자 인증

	// GetAdminPasswordHash는 저장된 관리자 비밀번호 해시를 반환한다.
	GetAdminPasswordHash(ctx context.Context) (string, error)
	// SetAdminPasswordHash는 관리자 비밀번호 해시를 저장한다.
	SetAdminPasswordHash(ctx context.Context, hash string) error

	// API 토큰

	// GetAPIToken은 저장된 API 토큰을 반환한다.
	GetAPIToken(ctx context.Context) (string, error)
	// SetAPIToken은 API 토큰을 저장한다.
	SetAPIToken(ctx context.Context, token string) error
	// DeleteAPIToken은 API 토큰을 삭제한다.
	DeleteAPIToken(ctx context.Context) error

	// Webhook (Notification)

	// SaveWebhook은 웹훅 엔드포인트를 저장한다.
	SaveWebhook(ctx context.Context, wh *models.WebhookEndpoint) error
	// GetWebhook은 지정된 ID의 웹훅을 반환한다.
	GetWebhook(ctx context.Context, id string) (*models.WebhookEndpoint, error)
	// ListWebhooks는 등록된 모든 웹훅 목록을 반환한다.
	ListWebhooks(ctx context.Context) ([]*models.WebhookEndpoint, error)
	// DeleteWebhook은 지정된 ID의 웹훅을 삭제한다.
	DeleteWebhook(ctx context.Context, id string) error

	// Slack 마이그레이션용 (deprecated, 시작 시 마이그레이션 후 삭제)

	// GetSlackWebhookURL은 기존 Slack 웹훅 URL을 반환한다 (마이그레이션 전용).
	GetSlackWebhookURL(ctx context.Context) (string, error)
	// GetSlackChannel은 기존 Slack 채널을 반환한다 (마이그레이션 전용).
	GetSlackChannel(ctx context.Context) (string, error)
	// DeleteSlackLegacy는 기존 Slack 설정 키를 삭제한다 (마이그레이션 전용).
	DeleteSlackLegacy(ctx context.Context) error

	// 런타임 설정

	// GetRuntimeSettings는 저장된 런타임 설정을 반환한다.
	GetRuntimeSettings(ctx context.Context) (map[string]string, error)
	// SaveRuntimeSettings는 런타임 설정 전체를 저장한다.
	SaveRuntimeSettings(ctx context.Context, settings map[string]string) error

	// DNS 공급자 설정

	// SaveDNSProviderConfig는 DNS 공급자 설정을 저장한다.
	SaveDNSProviderConfig(ctx context.Context, name string, cfg map[string]string) error
	// GetDNSProviderConfig는 지정한 DNS 공급자의 설정을 반환한다.
	GetDNSProviderConfig(ctx context.Context, name string) (map[string]string, error)
	// ListDNSProviderConfigs는 모든 DNS 공급자 설정을 반환한다.
	ListDNSProviderConfigs(ctx context.Context) (map[string]map[string]string, error)
	// DeleteDNSProviderConfig는 지정한 DNS 공급자 설정을 삭제한다.
	DeleteDNSProviderConfig(ctx context.Context, name string) (bool, error)

	// 생명주기

	// Close는 저장소 연결을 닫는다.
	Close() error
}
