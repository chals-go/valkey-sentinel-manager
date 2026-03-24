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

// ValkeyStore is a Valkey-backed Store implementation for production use.
type ValkeyStore struct {
	client      valkey.Client
	dedupWindow int // seconds
}

// NewValkeyStore creates a ValkeyStore connected to a standalone Valkey instance.
func NewValkeyStore(ctx context.Context, addr string, db int, password string, dedupWindowSeconds int) (*ValkeyStore, error) {
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
	return &ValkeyStore{client: client, dedupWindow: dedupWindowSeconds}, nil
}

// NewValkeyStoreSentinel creates a ValkeyStore connected via Valkey Sentinels.
func NewValkeyStoreSentinel(ctx context.Context, sentinelAddrs []string, masterName string, db int, password string, dedupWindowSeconds int) (*ValkeyStore, error) {
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
	return &ValkeyStore{client: client, dedupWindow: dedupWindowSeconds}, nil
}

// Close closes the Valkey connection.
func (s *ValkeyStore) Close() error {
	s.client.Close()
	slog.Info("Valkey connection closed")
	return nil
}

// === Events ===

// RecordEvent atomically increments the dedup counter and stores the event on first occurrence.
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

// GetEventCount returns the current report count for a dedup key.
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

// GetRecentEvents returns the most recent events up to limit.
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

// AcquireLock acquires a lock using SET NX EX.
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

// ReleaseLock releases a lock.
func (s *ValkeyStore) ReleaseLock(ctx context.Context, key string) error {
	lockKey := "smgr:lock:" + key
	if err := s.client.Do(ctx, s.client.B().Del().Key(lockKey).Build()).Error(); err != nil {
		return fmt.Errorf("releasing lock: %w", err)
	}
	return nil
}

// === Sentinels ===

// RegisterSentinel stores sentinel info as a hash.
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

// UnregisterSentinel removes a sentinel.
func (s *ValkeyStore) UnregisterSentinel(ctx context.Context, name string) (bool, error) {
	key := "smgr:sentinel_node:" + name
	deleted, err := s.client.Do(ctx, s.client.B().Del().Key(key).Build()).AsInt64()
	if err != nil {
		return false, fmt.Errorf("unregistering sentinel: %w", err)
	}
	s.client.Do(ctx, s.client.B().Srem().Key("smgr:sentinel_node:index").Member(name).Build())
	return deleted > 0, nil
}

// GetSentinel returns a sentinel by name.
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

// ListSentinels returns all sentinels, optionally filtered by group name.
func (s *ValkeyStore) ListSentinels(ctx context.Context, groupName string) ([]*models.Sentinel, error) {
	ids, err := s.client.Do(ctx, s.client.B().Smembers().Key("smgr:sentinel_node:index").Build()).AsStrSlice()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing sentinel index: %w", err)
	}
	var result []*models.Sentinel
	for _, id := range ids {
		sen, err := s.GetSentinel(ctx, id)
		if err != nil {
			continue
		}
		if groupName != "" && sen.GroupName != groupName {
			continue
		}
		result = append(result, sen)
	}
	return result, nil
}

// UpdateSentinelLastSeen updates the last_seen field.
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

// RegisterCluster stores a cluster as a JSON string.
func (s *ValkeyStore) RegisterCluster(ctx context.Context, c *models.Cluster) error {
	key := "smgr:group:" + c.MasterName
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshaling cluster: %w", err)
	}
	if err := s.client.Do(ctx, s.client.B().Set().Key(key).Value(string(data)).Build()).Error(); err != nil {
		return fmt.Errorf("registering cluster: %w", err)
	}
	if err := s.client.Do(ctx, s.client.B().Sadd().Key("smgr:group:index").Member(c.MasterName).Build()).Error(); err != nil {
		return fmt.Errorf("adding cluster to index: %w", err)
	}
	return nil
}

// UnregisterCluster removes a cluster.
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

// GetCluster returns a cluster by master name.
func (s *ValkeyStore) GetCluster(ctx context.Context, masterName string) (*models.Cluster, error) {
	key := "smgr:group:" + masterName
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(key).Build()).ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("getting cluster: %w", err)
	}
	var c models.Cluster
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return nil, fmt.Errorf("unmarshaling cluster: %w", err)
	}
	return &c, nil
}

// ListClusters returns all registered clusters.
func (s *ValkeyStore) ListClusters(ctx context.Context) ([]*models.Cluster, error) {
	ids, err := s.client.Do(ctx, s.client.B().Smembers().Key("smgr:group:index").Build()).AsStrSlice()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing cluster index: %w", err)
	}
	var result []*models.Cluster
	for _, id := range ids {
		c, err := s.GetCluster(ctx, id)
		if err != nil {
			continue
		}
		result = append(result, c)
	}
	return result, nil
}

// === Admin Auth ===

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

func (s *ValkeyStore) SetAdminPasswordHash(ctx context.Context, hash string) error {
	return s.client.Do(ctx, s.client.B().Set().Key("smgr:admin:password_hash").Value(hash).Build()).Error()
}

// === API Token ===

func (s *ValkeyStore) GetAPIToken(ctx context.Context) (string, error) {
	val, err := s.client.Do(ctx, s.client.B().Get().Key("smgr:api:token").Build()).ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return "", nil
		}
		return "", fmt.Errorf("getting api token: %w", err)
	}
	return val, nil
}

func (s *ValkeyStore) SetAPIToken(ctx context.Context, token string) error {
	return s.client.Do(ctx, s.client.B().Set().Key("smgr:api:token").Value(token).Build()).Error()
}

func (s *ValkeyStore) DeleteAPIToken(ctx context.Context) error {
	return s.client.Do(ctx, s.client.B().Del().Key("smgr:api:token").Build()).Error()
}

// === Slack ===

func (s *ValkeyStore) GetSlackWebhookURL(ctx context.Context) (string, error) {
	val, err := s.client.Do(ctx, s.client.B().Get().Key("smgr:slack:webhook_url").Build()).ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return "", nil
		}
		return "", fmt.Errorf("getting slack webhook url: %w", err)
	}
	return val, nil
}

func (s *ValkeyStore) SetSlackWebhookURL(ctx context.Context, url string) error {
	return s.client.Do(ctx, s.client.B().Set().Key("smgr:slack:webhook_url").Value(url).Build()).Error()
}

func (s *ValkeyStore) DeleteSlackWebhookURL(ctx context.Context) error {
	return s.client.Do(ctx, s.client.B().Del().Key("smgr:slack:webhook_url").Build()).Error()
}

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

func (s *ValkeyStore) SetSlackChannel(ctx context.Context, channel string) error {
	return s.client.Do(ctx, s.client.B().Set().Key("smgr:slack:channel").Value(channel).Build()).Error()
}

// === Runtime Settings ===

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

func (s *ValkeyStore) SaveRuntimeSettings(ctx context.Context, settings map[string]string) error {
	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshaling runtime settings: %w", err)
	}
	return s.client.Do(ctx, s.client.B().Set().Key("smgr:runtime:settings").Value(string(data)).Build()).Error()
}

// === DNS Provider Config ===

func (s *ValkeyStore) SaveDNSProviderConfig(ctx context.Context, name string, cfg map[string]string) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling dns config: %w", err)
	}
	return s.client.Do(ctx, s.client.B().Hset().Key("smgr:dns:providers").FieldValue().FieldValue(name, string(data)).Build()).Error()
}

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
