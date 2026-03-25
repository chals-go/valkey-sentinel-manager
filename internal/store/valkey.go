package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/chals-go/valkey-sentinel-manager/internal/models"
)

// Valkey key naming conventions:
//
//	smgr:event:dedup:{dedup_key}          — event dedup counter (TTL auto-expire)
//	smgr:event:history                    — recent event list (LPUSH/LTRIM)
//	smgr:lock:{key}                       — distributed lock (SET NX EX)
//	smgr:sentinel_node:{name}             — sentinel info (HASH)
//	smgr:sentinel_node:index              — sentinel name set (SET)
//	smgr:group:{master_name}              — cluster info (JSON string)
//	smgr:group:index                      — cluster master_name set (SET)
//	smgr:admin:password_hash              — admin password hash
//	smgr:api:token                        — API bearer token
//	smgr:slack:webhook_url                — Slack webhook URL
//	smgr:slack:channel                    — Slack channel
//	smgr:runtime:settings                 — runtime settings (JSON)
//	smgr:dns:providers                    — DNS provider configs (HASH, value=JSON)

const valkeyEventHistoryMax = 500

// ValkeyStore는 프로덕션 환경에서 사용하는 Valkey 기반 저장소 구현체이다.
type ValkeyStore struct {
	client      valkey.Client
	dedupWindow int // seconds
	encrypt     func(string) string
	decrypt     func(string) string
}

// NewValkeyStore는 단독 실행 중인 Valkey 인스턴스에 연결된 ValkeyStore를 생성하여 반환한다.
func NewValkeyStore(ctx context.Context, addr string, db int, password string, dedupWindowSeconds int, encrypt, decrypt func(string) string) (*ValkeyStore, error) {
	opts := valkey.ClientOption{
		InitAddress: []string{addr},
		SelectDB:    db,
		Password:    password,
	}
	client, err := valkey.NewClient(opts)
	if err != nil {
		return nil, fmt.Errorf("connecting to valkey %s: %w", addr, err)
	}
	if err := client.Do(ctx, client.B().Ping().Build()).Error(); err != nil {
		client.Close()
		return nil, fmt.Errorf("pinging valkey: %w", err)
	}
	slog.Info("Valkey connected", "addr", addr, "db", db)
	return &ValkeyStore{client: client, dedupWindow: dedupWindowSeconds, encrypt: encrypt, decrypt: decrypt}, nil
}

// NewValkeyStoreSentinel은 Valkey 센티널을 통해 연결된 ValkeyStore를 생성하여 반환한다.
func NewValkeyStoreSentinel(ctx context.Context, sentinelAddrs []string, masterName string, db int, password string, dedupWindowSeconds int, encrypt, decrypt func(string) string) (*ValkeyStore, error) {
	opts := valkey.ClientOption{
		InitAddress: sentinelAddrs,
		SelectDB:    db,
		Password:    password,
		Sentinel: valkey.SentinelOption{
			MasterSet: masterName,
		},
	}
	client, err := valkey.NewClient(opts)
	if err != nil {
		return nil, fmt.Errorf("connecting to valkey via sentinel: %w", err)
	}
	if err := client.Do(ctx, client.B().Ping().Build()).Error(); err != nil {
		client.Close()
		return nil, fmt.Errorf("pinging valkey: %w", err)
	}
	slog.Info("Valkey Sentinel connected", "master", masterName, "sentinels", sentinelAddrs)
	return &ValkeyStore{client: client, dedupWindow: dedupWindowSeconds, encrypt: encrypt, decrypt: decrypt}, nil
}

// Close는 Valkey 연결을 닫는다.
func (s *ValkeyStore) Close() error {
	s.client.Close()
	slog.Info("Valkey connection closed")
	return nil
}

// === Events ===

// RecordEvent는 중복 제거 카운터를 원자적으로 증가시키고, 최초 발생 시 이벤트를 저장한다.
func (s *ValkeyStore) RecordEvent(ctx context.Context, event *models.FailoverEvent) (int, error) {
	dedupKey := "smgr:event:dedup:" + event.DedupKey()

	count, err := s.client.Do(ctx, s.client.B().Incr().Key(dedupKey).Build()).AsInt64()
	if err != nil {
		return 0, fmt.Errorf("incrementing event dedup counter: %w", err)
	}

	if count == 1 {
		s.client.Do(ctx, s.client.B().Expire().Key(dedupKey).Seconds(int64(s.dedupWindow)).Build())

		data, err := json.Marshal(event)
		if err != nil {
			return 1, fmt.Errorf("marshaling event: %w", err)
		}
		s.client.Do(ctx, s.client.B().Lpush().Key("smgr:event:history").Element(string(data)).Build())
		s.client.Do(ctx, s.client.B().Ltrim().Key("smgr:event:history").Start(0).Stop(valkeyEventHistoryMax-1).Build())
	}
	return int(count), nil
}

// GetEventCount는 지정한 중복 제거 키에 대한 현재 보고 횟수를 반환한다.
func (s *ValkeyStore) GetEventCount(ctx context.Context, dedupKey string) (int, error) {
	val, err := s.client.Do(ctx, s.client.B().Get().Key("smgr:event:dedup:"+dedupKey).Build()).ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("getting event count: %w", err)
	}
	n, _ := strconv.Atoi(val)
	return n, nil
}

// GetRecentEvents는 최근 이벤트를 limit 개수만큼 반환한다.
func (s *ValkeyStore) GetRecentEvents(ctx context.Context, limit int) ([]*models.FailoverEvent, error) {
	raw, err := s.client.Do(ctx, s.client.B().Lrange().Key("smgr:event:history").Start(0).Stop(int64(limit-1)).Build()).AsStrSlice()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("getting recent events: %w", err)
	}
	events := make([]*models.FailoverEvent, 0, len(raw))
	for _, r := range raw {
		var e models.FailoverEvent
		if err := json.Unmarshal([]byte(r), &e); err != nil {
			slog.Warn("skipping malformed event", "error", err)
			continue
		}
		events = append(events, &e)
	}
	return events, nil
}

// === Distributed Lock ===

// AcquireLock은 SET NX EX 명령을 사용하여 분산 락을 획득한다.
func (s *ValkeyStore) AcquireLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	lockKey := "smgr:lock:" + key
	resp := s.client.Do(ctx, s.client.B().Set().Key(lockKey).Value("1").Nx().Ex(ttl).Build())
	if err := resp.Error(); err != nil {
		if valkey.IsValkeyNil(err) {
			return false, nil
		}
		return false, fmt.Errorf("acquiring lock: %w", err)
	}
	return true, nil
}

// ReleaseLock은 지정한 키에 대한 분산 락을 해제한다.
func (s *ValkeyStore) ReleaseLock(ctx context.Context, key string) error {
	lockKey := "smgr:lock:" + key
	if err := s.client.Do(ctx, s.client.B().Del().Key(lockKey).Build()).Error(); err != nil {
		return fmt.Errorf("releasing lock: %w", err)
	}
	return nil
}

// === Sentinels ===

// RegisterSentinel은 센티널 정보를 해시로 저장하여 등록한다.
func (s *ValkeyStore) RegisterSentinel(ctx context.Context, sen *models.Sentinel) error {
	key := "smgr:sentinel_node:" + sen.SentinelNodeName
	fields := map[string]string{
		"sentinel_node_name": sen.SentinelNodeName,
		"group_name":         sen.GroupName,
		"host":               sen.Host,
		"port":               strconv.Itoa(sen.Port),
		"last_seen":          strconv.FormatFloat(sen.LastSeen, 'f', -1, 64),
		"registered_at":      strconv.FormatFloat(sen.RegisteredAt, 'f', -1, 64),
	}
	args := make([]string, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	cmd := s.client.B().Hset().Key(key).FieldValue()
	for k, v := range fields {
		cmd = cmd.FieldValue(k, v)
	}
	if err := s.client.Do(ctx, cmd.Build()).Error(); err != nil {
		return fmt.Errorf("registering sentinel: %w", err)
	}
	if err := s.client.Do(ctx, s.client.B().Sadd().Key("smgr:sentinel_node:index").Member(sen.SentinelNodeName).Build()).Error(); err != nil {
		return fmt.Errorf("adding sentinel to index: %w", err)
	}
	return nil
}

// UnregisterSentinel은 센티널을 제거한다.
func (s *ValkeyStore) UnregisterSentinel(ctx context.Context, name string) (bool, error) {
	key := "smgr:sentinel_node:" + name
	deleted, err := s.client.Do(ctx, s.client.B().Del().Key(key).Build()).AsInt64()
	if err != nil {
		return false, fmt.Errorf("unregistering sentinel: %w", err)
	}
	s.client.Do(ctx, s.client.B().Srem().Key("smgr:sentinel_node:index").Member(name).Build())
	return deleted > 0, nil
}

// GetSentinel은 이름으로 센티널을 조회한다.
func (s *ValkeyStore) GetSentinel(ctx context.Context, name string) (*models.Sentinel, error) {
	key := "smgr:sentinel_node:" + name
	data, err := s.client.Do(ctx, s.client.B().Hgetall().Key(key).Build()).AsStrMap()
	if err != nil {
		return nil, fmt.Errorf("getting sentinel: %w", err)
	}
	if len(data) == 0 {
		return nil, ErrNotFound
	}
	return hashToSentinel(data), nil
}

// ListSentinels는 모든 센티널 목록을 반환한다. groupName이 비어있으면 전체를 반환한다.
// DoMulti 파이프라인으로 일괄 조회하여 N+1 쿼리를 방지한다.
func (s *ValkeyStore) ListSentinels(ctx context.Context, groupName string) ([]*models.Sentinel, error) {
	ids, err := s.client.Do(ctx, s.client.B().Smembers().Key("smgr:sentinel_node:index").Build()).AsStrSlice()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing sentinel index: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	cmds := make(valkey.Commands, len(ids))
	for i, id := range ids {
		cmds[i] = s.client.B().Hgetall().Key("smgr:sentinel_node:" + id).Build()
	}
	results := s.client.DoMulti(ctx, cmds...)
	var out []*models.Sentinel
	for _, resp := range results {
		data, err := resp.AsStrMap()
		if err != nil || len(data) == 0 {
			continue
		}
		sen := hashToSentinel(data)
		if groupName != "" && sen.GroupName != groupName {
			continue
		}
		out = append(out, sen)
	}
	return out, nil
}

// UpdateSentinelLastSeen은 센티널의 last_seen 필드를 갱신한다.
func (s *ValkeyStore) UpdateSentinelLastSeen(ctx context.Context, name string, timestamp float64) error {
	key := "smgr:sentinel_node:" + name
	exists, err := s.client.Do(ctx, s.client.B().Exists().Key(key).Build()).AsBool()
	if err != nil || !exists {
		return nil
	}
	ts := strconv.FormatFloat(timestamp, 'f', -1, 64)
	if err := s.client.Do(ctx, s.client.B().Hset().Key(key).FieldValue().FieldValue("last_seen", ts).Build()).Error(); err != nil {
		return fmt.Errorf("updating sentinel last_seen: %w", err)
	}
	return nil
}

// === Clusters ===

// RegisterCluster는 클러스터를 JSON 문자열로 직렬화하여 저장한다.
func (s *ValkeyStore) RegisterCluster(ctx context.Context, c *models.Cluster) error {
	key := "smgr:group:" + c.MasterName
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshaling cluster: %w", err)
	}
	val := string(data)
	if s.encrypt != nil {
		val = s.encrypt(val)
	}
	if err := s.client.Do(ctx, s.client.B().Set().Key(key).Value(val).Build()).Error(); err != nil {
		return fmt.Errorf("registering cluster: %w", err)
	}
	if err := s.client.Do(ctx, s.client.B().Sadd().Key("smgr:group:index").Member(c.MasterName).Build()).Error(); err != nil {
		return fmt.Errorf("adding cluster to index: %w", err)
	}
	return nil
}

// UnregisterCluster는 master_name으로 클러스터를 제거한다.
func (s *ValkeyStore) UnregisterCluster(ctx context.Context, masterName string) (bool, error) {
	key := "smgr:group:" + masterName
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(key).Build()).ToString()
	if err != nil || raw == "" {
		return false, nil
	}
	s.client.Do(ctx, s.client.B().Del().Key(key).Build())
	s.client.Do(ctx, s.client.B().Srem().Key("smgr:group:index").Member(masterName).Build())
	return true, nil
}

// GetCluster는 master_name으로 클러스터를 조회한다.
func (s *ValkeyStore) GetCluster(ctx context.Context, masterName string) (*models.Cluster, error) {
	key := "smgr:group:" + masterName
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(key).Build()).ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("getting cluster: %w", err)
	}
	if s.decrypt != nil {
		raw = s.decrypt(raw)
	}
	var c models.Cluster
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return nil, fmt.Errorf("unmarshaling cluster: %w", err)
	}
	return &c, nil
}

// ListClusters는 등록된 모든 클러스터를 반환한다.
// DoMulti 파이프라인으로 일괄 조회하여 N+1 쿼리를 방지한다.
func (s *ValkeyStore) ListClusters(ctx context.Context) ([]*models.Cluster, error) {
	ids, err := s.client.Do(ctx, s.client.B().Smembers().Key("smgr:group:index").Build()).AsStrSlice()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing cluster index: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	cmds := make(valkey.Commands, len(ids))
	for i, id := range ids {
		cmds[i] = s.client.B().Get().Key("smgr:group:" + id).Build()
	}
	results := s.client.DoMulti(ctx, cmds...)
	var out []*models.Cluster
	for _, resp := range results {
		raw, err := resp.ToString()
		if err != nil {
			continue
		}
		if s.decrypt != nil {
			raw = s.decrypt(raw)
		}
		var c models.Cluster
		if json.Unmarshal([]byte(raw), &c) != nil {
			continue
		}
		out = append(out, &c)
	}
	return out, nil
}

// === Admin Auth ===

// GetAdminPasswordHash는 저장된 관리자 비밀번호 해시를 반환한다.
func (s *ValkeyStore) GetAdminPasswordHash(ctx context.Context) (string, error) {
	val, err := s.client.Do(ctx, s.client.B().Get().Key("smgr:admin:password_hash").Build()).ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return "", nil
		}
		return "", fmt.Errorf("getting admin password hash: %w", err)
	}
	return val, nil
}

// SetAdminPasswordHash는 관리자 비밀번호 해시를 저장한다.
func (s *ValkeyStore) SetAdminPasswordHash(ctx context.Context, hash string) error {
	return s.client.Do(ctx, s.client.B().Set().Key("smgr:admin:password_hash").Value(hash).Build()).Error()
}

// === API Token ===

// GetAPIToken은 저장된 API 토큰을 반환한다.
func (s *ValkeyStore) GetAPIToken(ctx context.Context) (string, error) {
	val, err := s.client.Do(ctx, s.client.B().Get().Key("smgr:api:token").Build()).ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return "", nil
		}
		return "", fmt.Errorf("getting api token: %w", err)
	}
	if s.decrypt != nil {
		val = s.decrypt(val)
	}
	return val, nil
}

// SetAPIToken은 API 토큰을 저장한다.
func (s *ValkeyStore) SetAPIToken(ctx context.Context, token string) error {
	if s.encrypt != nil {
		token = s.encrypt(token)
	}
	return s.client.Do(ctx, s.client.B().Set().Key("smgr:api:token").Value(token).Build()).Error()
}

// DeleteAPIToken은 API 토큰을 삭제한다.
func (s *ValkeyStore) DeleteAPIToken(ctx context.Context) error {
	return s.client.Do(ctx, s.client.B().Del().Key("smgr:api:token").Build()).Error()
}

// === Slack ===

// GetSlackWebhookURL은 저장된 Slack 웹훅 URL을 반환한다.
func (s *ValkeyStore) GetSlackWebhookURL(ctx context.Context) (string, error) {
	val, err := s.client.Do(ctx, s.client.B().Get().Key("smgr:slack:webhook_url").Build()).ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return "", nil
		}
		return "", fmt.Errorf("getting slack webhook url: %w", err)
	}
	if s.decrypt != nil {
		val = s.decrypt(val)
	}
	return val, nil
}

// SetSlackWebhookURL은 Slack 웹훅 URL을 저장한다.
func (s *ValkeyStore) SetSlackWebhookURL(ctx context.Context, url string) error {
	if s.encrypt != nil {
		url = s.encrypt(url)
	}
	return s.client.Do(ctx, s.client.B().Set().Key("smgr:slack:webhook_url").Value(url).Build()).Error()
}

// DeleteSlackWebhookURL은 Slack 웹훅 URL을 삭제한다.
func (s *ValkeyStore) DeleteSlackWebhookURL(ctx context.Context) error {
	return s.client.Do(ctx, s.client.B().Del().Key("smgr:slack:webhook_url").Build()).Error()
}

// GetSlackChannel은 저장된 Slack 채널 이름을 반환한다.
func (s *ValkeyStore) GetSlackChannel(ctx context.Context) (string, error) {
	val, err := s.client.Do(ctx, s.client.B().Get().Key("smgr:slack:channel").Build()).ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return "", nil
		}
		return "", fmt.Errorf("getting slack channel: %w", err)
	}
	return val, nil
}

// SetSlackChannel은 Slack 채널 이름을 저장한다.
func (s *ValkeyStore) SetSlackChannel(ctx context.Context, channel string) error {
	return s.client.Do(ctx, s.client.B().Set().Key("smgr:slack:channel").Value(channel).Build()).Error()
}

// === Runtime Settings ===

// GetRuntimeSettings는 저장된 런타임 설정을 반환한다.
func (s *ValkeyStore) GetRuntimeSettings(ctx context.Context) (map[string]string, error) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key("smgr:runtime:settings").Build()).ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("getting runtime settings: %w", err)
	}
	var result map[string]string
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("unmarshaling runtime settings: %w", err)
	}
	return result, nil
}

// SaveRuntimeSettings는 런타임 설정 전체를 저장한다.
func (s *ValkeyStore) SaveRuntimeSettings(ctx context.Context, settings map[string]string) error {
	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshaling runtime settings: %w", err)
	}
	return s.client.Do(ctx, s.client.B().Set().Key("smgr:runtime:settings").Value(string(data)).Build()).Error()
}

// === DNS Provider Config ===

// SaveDNSProviderConfig는 DNS 공급자 설정을 저장한다.
func (s *ValkeyStore) SaveDNSProviderConfig(ctx context.Context, name string, cfg map[string]string) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling dns config: %w", err)
	}
	return s.client.Do(ctx, s.client.B().Hset().Key("smgr:dns:providers").FieldValue().FieldValue(name, string(data)).Build()).Error()
}

// GetDNSProviderConfig는 지정한 DNS 공급자의 설정을 반환한다.
func (s *ValkeyStore) GetDNSProviderConfig(ctx context.Context, name string) (map[string]string, error) {
	raw, err := s.client.Do(ctx, s.client.B().Hget().Key("smgr:dns:providers").Field(name).Build()).ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("getting dns config: %w", err)
	}
	var result map[string]string
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("unmarshaling dns config: %w", err)
	}
	return result, nil
}

// ListDNSProviderConfigs는 모든 DNS 공급자 설정을 반환한다.
func (s *ValkeyStore) ListDNSProviderConfigs(ctx context.Context) (map[string]map[string]string, error) {
	rawMap, err := s.client.Do(ctx, s.client.B().Hgetall().Key("smgr:dns:providers").Build()).AsStrMap()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return map[string]map[string]string{}, nil
		}
		return nil, fmt.Errorf("listing dns configs: %w", err)
	}
	result := make(map[string]map[string]string, len(rawMap))
	for name, raw := range rawMap {
		var cfg map[string]string
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			slog.Warn("skipping malformed dns config", "name", name, "error", err)
			continue
		}
		result[name] = cfg
	}
	return result, nil
}

// DeleteDNSProviderConfig는 지정한 DNS 공급자 설정을 삭제한다.
func (s *ValkeyStore) DeleteDNSProviderConfig(ctx context.Context, name string) (bool, error) {
	deleted, err := s.client.Do(ctx, s.client.B().Hdel().Key("smgr:dns:providers").Field(name).Build()).AsInt64()
	if err != nil {
		return false, fmt.Errorf("deleting dns config: %w", err)
	}
	return deleted > 0, nil
}

// === Helpers ===

func hashToSentinel(data map[string]string) *models.Sentinel {
	port, _ := strconv.Atoi(data["port"])
	if port == 0 {
		port = 26379
	}
	lastSeen, _ := strconv.ParseFloat(data["last_seen"], 64)
	registeredAt, _ := strconv.ParseFloat(data["registered_at"], 64)

	return &models.Sentinel{
		SentinelNodeName: data["sentinel_node_name"],
		GroupName:        data["group_name"],
		Host:             data["host"],
		Port:             port,
		LastSeen:         lastSeen,
		RegisteredAt:     registeredAt,
	}
}
