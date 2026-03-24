package core

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/chals-go/valkey-sentinel-manager/internal/models"
	"github.com/chals-go/valkey-sentinel-manager/internal/store"
)

// SentinelHealthChecker는 등록된 모든 센티널 노드를 백그라운드에서 주기적으로 핑하고
// 노드 연결 상태를 맵으로 유지 관리하는 구조체이다.
type SentinelHealthChecker struct {
	store     store.Store
	mu        sync.RWMutex
	statuses  map[string]bool // node name -> connected
	prevState map[string]bool // previous tick state for transition detection
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// NewSentinelHealthChecker는 SentinelHealthChecker를 생성하여 반환한다.
func NewSentinelHealthChecker(s store.Store) *SentinelHealthChecker {
	return &SentinelHealthChecker{
		store:     s,
		statuses:  make(map[string]bool),
		prevState: make(map[string]bool),
	}
}

// Start는 백그라운드 헬스체크 고루틴을 시작한다.
func (hc *SentinelHealthChecker) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	hc.cancel = cancel
	hc.wg.Add(1)
	go hc.loop(ctx)
	slog.Info("sentinel health checker started")
}

// Stop은 백그라운드 고루틴을 취소하고 종료될 때까지 대기한다.
func (hc *SentinelHealthChecker) Stop() {
	if hc.cancel != nil {
		hc.cancel()
	}
	hc.wg.Wait()
	slog.Info("sentinel health checker stopped")
}

// GetAllStatuses는 현재 노드 연결 상태 맵의 복사본을 반환한다.
func (hc *SentinelHealthChecker) GetAllStatuses() map[string]bool {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	result := make(map[string]bool, len(hc.statuses))
	for k, v := range hc.statuses {
		result[k] = v
	}
	return result
}

// GetStatus는 특정 노드의 연결 상태와 존재 여부를 반환한다.
func (hc *SentinelHealthChecker) GetStatus(nodeName string) (connected bool, exists bool) {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	v, ok := hc.statuses[nodeName]
	return v, ok
}

func (hc *SentinelHealthChecker) loop(ctx context.Context) {
	defer hc.wg.Done()

	// Initial tick immediately.
	hc.tick(ctx)

	interval := hc.getInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hc.tick(ctx)
			// Re-read interval and reset ticker if changed.
			newInterval := hc.getInterval()
			if newInterval != interval {
				interval = newInterval
				ticker.Reset(interval)
				slog.Info("sentinel ping interval changed", "interval", interval)
			}
		}
	}
}

func (hc *SentinelHealthChecker) getInterval() time.Duration {
	rt, err := hc.store.GetRuntimeSettings(context.Background())
	if err == nil {
		if v, ok := rt["sentinel_ping_interval"]; ok {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				return time.Duration(n) * time.Second
			}
		}
	}
	return 5 * time.Second
}

func (hc *SentinelHealthChecker) tick(ctx context.Context) {
	sentinels, err := hc.store.ListSentinels(ctx, "")
	if err != nil {
		slog.Warn("health checker: failed to list sentinels", "error", err)
		return
	}

	if len(sentinels) == 0 {
		return
	}

	// Ping all sentinels concurrently (bounded).
	type pingResult struct {
		name      string
		group     string
		host      string
		port      int
		connected bool
	}

	results := make(chan pingResult, len(sentinels))
	sem := make(chan struct{}, 10) // max 10 concurrent pings

	for _, s := range sentinels {
		sem <- struct{}{}
		go func(name, group, host string, port int) {
			defer func() { <-sem }()
			connected := PingSentinel(ctx, host, port, "")
			results <- pingResult{name: name, group: group, host: host, port: port, connected: connected}
		}(s.SentinelNodeName, s.GroupName, s.Host, s.Port)
	}

	// Collect results.
	newStatuses := make(map[string]bool, len(sentinels))
	nodeGroups := make(map[string]string)    // node -> group
	nodeAddrs := make(map[string]string)     // node -> host:port

	for i := 0; i < len(sentinels); i++ {
		r := <-results
		newStatuses[r.name] = r.connected
		nodeGroups[r.name] = r.group
		nodeAddrs[r.name] = r.host + ":" + strconv.Itoa(r.port)

		if r.connected {
			now := float64(time.Now().UnixMilli()) / 1000.0
			hc.store.UpdateSentinelLastSeen(ctx, r.name, now)
		}
	}

	// Detect transitions under lock, collect actions, execute outside lock.
	type transition struct {
		group, name, addr string
		eventType         models.EventType
		down              bool
	}
	var transitions []transition

	hc.mu.Lock()
	for name, connected := range newStatuses {
		wasConnected, existed := hc.prevState[name]
		if existed && wasConnected && !connected {
			transitions = append(transitions, transition{nodeGroups[name], name, nodeAddrs[name], models.EventTypeSentinelDown, true})
		} else if existed && !wasConnected && connected {
			transitions = append(transitions, transition{nodeGroups[name], name, nodeAddrs[name], models.EventTypeSentinelUp, false})
		}
	}
	hc.prevState = make(map[string]bool, len(newStatuses))
	for k, v := range newStatuses {
		hc.prevState[k] = v
	}
	hc.statuses = newStatuses
	hc.mu.Unlock()

	// Execute transitions outside lock to avoid deadlock.
	for _, t := range transitions {
		go hc.recordSentinelEvent(t.group, t.name, t.addr, t.eventType)
		if t.down {
			go hc.sendDownAlert(t.group, t.name, t.addr)
		} else {
			go hc.sendUpAlert(t.group, t.name, t.addr)
		}
	}
}

func (hc *SentinelHealthChecker) recordSentinelEvent(groupName, nodeName, addr string, eventType models.EventType) {
	ctx := context.Background()
	host := addr
	port := 0
	if idx := len(addr) - 1; idx > 0 {
		for i := len(addr) - 1; i >= 0; i-- {
			if addr[i] == ':' {
				host = addr[:i]
				p, _ := strconv.Atoi(addr[i+1:])
				port = p
				break
			}
		}
	}

	event := &models.FailoverEvent{
		GroupName:        groupName,
		MasterName:       "",
		SentinelNodeName: nodeName,
		EventType:        eventType,
		FromIP:           host,
		FromPort:         port,
		Timestamp:        float64(time.Now().UnixMilli()) / 1000.0,
	}
	if _, err := hc.store.RecordEvent(ctx, event); err != nil {
		slog.Warn("failed to record sentinel event", "error", err)
	}
	slog.Info("sentinel event recorded", "type", eventType, "node", nodeName, "addr", addr)
}

func (hc *SentinelHealthChecker) sendDownAlert(groupName, nodeName, addr string) {
	ctx := context.Background()

	// Check if alert is enabled for this group.
	rt, err := hc.store.GetRuntimeSettings(ctx)
	if err != nil {
		return
	}
	if rt["sentinel_alert:"+groupName] != "true" {
		return
	}

	webhookURL, _ := hc.store.GetSlackWebhookURL(ctx)
	if webhookURL == "" {
		return
	}
	channel, _ := hc.store.GetSlackChannel(ctx)

	slog.Warn("sentinel node down, sending alert", "group", groupName, "node", nodeName, "addr", addr)
	SendSentinelDownSlack(ctx, webhookURL, channel, nodeName, addr, groupName)
}

func (hc *SentinelHealthChecker) sendUpAlert(groupName, nodeName, addr string) {
	ctx := context.Background()

	rt, err := hc.store.GetRuntimeSettings(ctx)
	if err != nil {
		return
	}
	if rt["sentinel_alert:"+groupName] != "true" {
		return
	}

	webhookURL, _ := hc.store.GetSlackWebhookURL(ctx)
	if webhookURL == "" {
		return
	}
	channel, _ := hc.store.GetSlackChannel(ctx)

	slog.Info("sentinel node up, sending alert", "group", groupName, "node", nodeName, "addr", addr)
	SendSentinelUpSlack(ctx, webhookURL, channel, nodeName, addr, groupName)
}
