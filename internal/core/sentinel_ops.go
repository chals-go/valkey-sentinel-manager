package core

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/valkey-io/valkey-go"
)

// SlaveInfo는 센티널이 보고하는 레플리카 노드 정보를 담는 구조체이다.
type SlaveInfo struct {
	IP     string `json:"ip"`
	Port   int    `json:"port"`
	Flags  string `json:"flags"`
	Status string `json:"status"`
}

// MasterDetail는 센티널에서 조회한 마스터 및 레플리카 상세 정보를 담는 구조체이다.
type MasterDetail struct {
	Name            string      `json:"name"`
	MasterIP        string      `json:"master_ip"`
	MasterPort      int         `json:"master_port"`
	MasterStatus    string      `json:"master_status"`
	Slaves          []SlaveInfo `json:"slaves"`
	NumSentinels    int         `json:"num_sentinels"`
	Quorum          int         `json:"quorum"`
	DownAfterMs     int         `json:"down_after_ms"`
	FailoverTimeout int         `json:"failover_timeout"`
}

// TestFailoverResult는 테스트 페일오버 명령 실행 결과를 담는 구조체이다.
type TestFailoverResult struct {
	Success     bool   `json:"success"`
	PrimaryIP   string `json:"primary_ip"`
	PrimaryPort int    `json:"primary_port"`
	Message     string `json:"message"`
}

// parseSentinelAddr는 "host:port" 형식의 주소를 호스트와 포트로 분리한다.
func parseSentinelAddr(addr string) (string, int) {
	if idx := strings.LastIndex(addr, ":"); idx >= 0 {
		port, err := strconv.Atoi(addr[idx+1:])
		if err == nil {
			return addr[:idx], port
		}
	}
	return addr, 26379
}

func newSentinelClient(addr, password string) (valkey.Client, error) {
	host, port := parseSentinelAddr(addr)
	opts := valkey.ClientOption{
		InitAddress:       []string{fmt.Sprintf("%s:%d", host, port)},
		Password:          password,
		DisableCache:      true,
		ForceSingleClient: true,
		ConnWriteTimeout:  5 * time.Second,
	}
	return valkey.NewClient(opts)
}

// PingSentinel은 지정한 센티널 인스턴스의 연결 가능 여부를 확인한다.
func PingSentinel(ctx context.Context, host string, port int, password string) bool {
	addr := fmt.Sprintf("%s:%d", host, port)
	client, err := newSentinelClient(addr, password)
	if err != nil {
		return false
	}
	defer client.Close()
	return client.Do(ctx, client.B().Ping().Build()).Error() == nil
}

// SentinelMonitor는 주어진 모든 센티널 인스턴스에 새 마스터를 등록한다.
func SentinelMonitor(ctx context.Context, sentinelAddrs []string, masterName, masterIP string, masterPort, quorum int, authPass, sentinelPassword string, downAfterMs, failoverTimeout int) map[string]bool {
	results := make(map[string]bool, len(sentinelAddrs))

	for _, addr := range sentinelAddrs {
		client, err := newSentinelClient(addr, sentinelPassword)
		if err != nil {
			slog.Error("sentinel connect failed", "addr", addr, "error", err)
			results[addr] = false
			continue
		}

		ok := true
		// SENTINEL MONITOR
		err = client.Do(ctx, client.B().Arbitrary("SENTINEL", "MONITOR", masterName, masterIP, strconv.Itoa(masterPort), strconv.Itoa(quorum)).Build()).Error()
		if err != nil {
			slog.Error("SENTINEL MONITOR failed", "addr", addr, "master", masterName, "error", err)
			ok = false
		} else {
			slog.Info("SENTINEL MONITOR ok", "addr", addr, "master", masterName)

			if authPass != "" {
				client.Do(ctx, client.B().Arbitrary("SENTINEL", "SET", masterName, "auth-pass", authPass).Build())
			}
			client.Do(ctx, client.B().Arbitrary("SENTINEL", "SET", masterName, "down-after-milliseconds", strconv.Itoa(downAfterMs)).Build())
			client.Do(ctx, client.B().Arbitrary("SENTINEL", "SET", masterName, "failover-timeout", strconv.Itoa(failoverTimeout)).Build())

			// Try registering scripts (may fail if deny-scripts-reconfig=yes).
			if err := client.Do(ctx, client.B().Arbitrary("SENTINEL", "SET", masterName, "client-reconfig-script", "/usr/local/bin/sentinel-agent-reconfig").Build()).Error(); err != nil {
				slog.Warn("sentinel script SET failed", "addr", addr, "error", err)
			}
			if err := client.Do(ctx, client.B().Arbitrary("SENTINEL", "SET", masterName, "notification-script", "/usr/local/bin/sentinel-agent-notify").Build()).Error(); err != nil {
				slog.Warn("sentinel script SET failed", "addr", addr, "error", err)
			}
		}

		client.Close()
		results[addr] = ok
	}
	return results
}

// SentinelSetConfig는 센티널 인스턴스들의 down-after-milliseconds 및 failover-timeout 설정을 갱신한다.
func SentinelSetConfig(ctx context.Context, sentinelAddrs []string, masterName, sentinelPassword string, downAfterMs, failoverTimeout int) map[string]bool {
	results := make(map[string]bool, len(sentinelAddrs))
	for _, addr := range sentinelAddrs {
		client, err := newSentinelClient(addr, sentinelPassword)
		if err != nil {
			results[addr] = false
			continue
		}
		err1 := client.Do(ctx, client.B().Arbitrary("SENTINEL", "SET", masterName, "down-after-milliseconds", strconv.Itoa(downAfterMs)).Build()).Error()
		err2 := client.Do(ctx, client.B().Arbitrary("SENTINEL", "SET", masterName, "failover-timeout", strconv.Itoa(failoverTimeout)).Build()).Error()
		client.Close()
		results[addr] = err1 == nil && err2 == nil
	}
	return results
}

// SentinelApplyConfig는 센티널 마스터에 down-after-ms, failover-timeout, 스크립트를 일괄 적용한다.
func SentinelApplyConfig(ctx context.Context, sentinelAddrs []string, masterName, sentinelPassword string, downAfterMs, failoverTimeout int) {
	for _, addr := range sentinelAddrs {
		client, err := newSentinelClient(addr, sentinelPassword)
		if err != nil {
			continue
		}
		client.Do(ctx, client.B().Arbitrary("SENTINEL", "SET", masterName, "down-after-milliseconds", strconv.Itoa(downAfterMs)).Build())
		client.Do(ctx, client.B().Arbitrary("SENTINEL", "SET", masterName, "failover-timeout", strconv.Itoa(failoverTimeout)).Build())
		client.Do(ctx, client.B().Arbitrary("SENTINEL", "SET", masterName, "client-reconfig-script", "/usr/local/bin/sentinel-agent-reconfig").Build())
		client.Do(ctx, client.B().Arbitrary("SENTINEL", "SET", masterName, "notification-script", "/usr/local/bin/sentinel-agent-notify").Build())
		client.Close()
	}
}

// SentinelRemove는 주어진 모든 센티널 인스턴스에서 마스터 등록을 해제한다.
func SentinelRemove(ctx context.Context, sentinelAddrs []string, masterName, sentinelPassword string) map[string]bool {
	results := make(map[string]bool, len(sentinelAddrs))
	for _, addr := range sentinelAddrs {
		client, err := newSentinelClient(addr, sentinelPassword)
		if err != nil {
			results[addr] = false
			continue
		}
		err = client.Do(ctx, client.B().Arbitrary("SENTINEL", "REMOVE", masterName).Build()).Error()
		client.Close()
		if err != nil {
			slog.Error("SENTINEL REMOVE failed", "addr", addr, "master", masterName, "error", err)
			results[addr] = false
		} else {
			slog.Info("SENTINEL REMOVE ok", "addr", addr, "master", masterName)
			results[addr] = true
		}
	}
	return results
}

// GetMasterDetail는 센티널에 마스터와 슬레이브 상세 정보를 조회한다.
// 센티널 주소를 순서대로 시도하며, 모두 실패하면 nil을 반환한다.
func GetMasterDetail(ctx context.Context, sentinelAddrs []string, masterName, sentinelPassword string) *MasterDetail {
	for _, addr := range sentinelAddrs {
		detail, err := getMasterDetailFromAddr(ctx, addr, masterName, sentinelPassword)
		if err != nil {
			slog.Warn("sentinel query failed, trying next", "addr", addr, "master", masterName, "error", err)
			continue
		}
		return detail
	}
	slog.Warn("all sentinels failed for master detail", "master", masterName, "addrs", sentinelAddrs)
	return nil
}

// SentinelMasterInfo는 SENTINEL MASTERS 응답의 개별 마스터 정보이다.
type SentinelMasterInfo struct {
	Name   string `json:"name"`
	IP     string `json:"ip"`
	Port   int    `json:"port"`
	Quorum int    `json:"quorum"`
	Status string `json:"status"`
}

// ListSentinelMasters는 센티널에서 모니터링 중인 모든 마스터 목록을 조회한다.
func ListSentinelMasters(ctx context.Context, sentinelAddrs []string, sentinelPassword string) []SentinelMasterInfo {
	for _, addr := range sentinelAddrs {
		client, err := newSentinelClient(addr, sentinelPassword)
		if err != nil {
			continue
		}
		resp, err := client.Do(ctx, client.B().Arbitrary("SENTINEL", "MASTERS").Build()).ToArray()
		client.Close()
		if err != nil {
			slog.Warn("SENTINEL MASTERS failed", "addr", addr, "error", err)
			continue
		}
		var masters []SentinelMasterInfo
		for _, m := range resp {
			info, err := parseSentinelMessage(m)
			if err != nil {
				continue
			}
			port, _ := strconv.Atoi(info["port"])
			quorum, _ := strconv.Atoi(info["quorum"])
			flags := info["flags"]
			status := "ok"
			if !strings.Contains(flags, "master") || strings.Contains(flags, "s_down") || strings.Contains(flags, "o_down") {
				status = flags
			}
			masters = append(masters, SentinelMasterInfo{
				Name:   info["name"],
				IP:     info["ip"],
				Port:   port,
				Quorum: quorum,
				Status: status,
			})
		}
		return masters
	}
	return nil
}

// parseSentinelResult는 ValkeyResult를 맵(RESP3) 또는 플랫 배열(RESP2)로 파싱한다.
func parseSentinelResult(resp valkey.ValkeyResult) (map[string]string, error) {
	if m, err := resp.AsStrMap(); err == nil {
		return m, nil
	}
	if items, err := resp.AsStrSlice(); err == nil {
		return flatSliceToMap(items), nil
	}
	return nil, fmt.Errorf("cannot parse sentinel response")
}

// parseSentinelMessage는 ValkeyMessage를 맵(RESP3) 또는 플랫 배열(RESP2)로 파싱한다.
func parseSentinelMessage(msg valkey.ValkeyMessage) (map[string]string, error) {
	if m, err := msg.AsStrMap(); err == nil {
		return m, nil
	}
	if items, err := msg.AsStrSlice(); err == nil {
		return flatSliceToMap(items), nil
	}
	return nil, fmt.Errorf("cannot parse sentinel message")
}

func getMasterDetailFromAddr(ctx context.Context, addr, masterName, sentinelPassword string) (*MasterDetail, error) {
	client, err := newSentinelClient(addr, sentinelPassword)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	// SENTINEL MASTER <name>
	masterResp := client.Do(ctx, client.B().Arbitrary("SENTINEL", "MASTER", masterName).Build())
	masterInfo, err := parseSentinelResult(masterResp)
	if err != nil {
		return nil, fmt.Errorf("SENTINEL MASTER: %w", err)
	}

	// SENTINEL SLAVES <name>
	slavesResp, err := client.Do(ctx, client.B().Arbitrary("SENTINEL", "SLAVES", masterName).Build()).ToArray()
	if err != nil {
		return nil, fmt.Errorf("SENTINEL SLAVES: %w", err)
	}

	var slaves []SlaveInfo
	for _, s := range slavesResp {
		info, err := parseSentinelMessage(s)
		if err != nil {
			continue
		}
		flags := info["flags"]
		isOK := strings.Contains(flags, "slave") && !strings.Contains(flags, "s_down") && !strings.Contains(flags, "o_down")
		status := "ok"
		if !isOK {
			status = flags
		}
		port, _ := strconv.Atoi(info["port"])
		slaves = append(slaves, SlaveInfo{
			IP:     info["ip"],
			Port:   port,
			Flags:  flags,
			Status: status,
		})
	}

	masterFlags := masterInfo["flags"]
	masterStatus := masterFlags
	if strings.Contains(masterFlags, "master") {
		masterStatus = "ok"
	}
	masterPort, _ := strconv.Atoi(masterInfo["port"])
	numSentinels, _ := strconv.Atoi(masterInfo["num-other-sentinels"])
	qrm, _ := strconv.Atoi(masterInfo["quorum"])
	downMs, _ := strconv.Atoi(masterInfo["down-after-milliseconds"])
	if downMs == 0 {
		downMs = 5000
	}
	foTimeout, _ := strconv.Atoi(masterInfo["failover-timeout"])
	if foTimeout == 0 {
		foTimeout = 30000
	}

	return &MasterDetail{
		Name:            masterName,
		MasterIP:        masterInfo["ip"],
		MasterPort:      masterPort,
		MasterStatus:    masterStatus,
		Slaves:          slaves,
		NumSentinels:    numSentinels + 1,
		Quorum:          qrm,
		DownAfterMs:     downMs,
		FailoverTimeout: foTimeout,
	}, nil
}

// TriggerTestFailover는 SENTINEL FAILOVER 명령을 전송하여 즉시 페일오버를 강제 실행한다.
func TriggerTestFailover(ctx context.Context, sentinelAddrs []string, masterName, sentinelPassword string) *TestFailoverResult {
	detail := GetMasterDetail(ctx, sentinelAddrs, masterName, sentinelPassword)
	currentPrimary := ""
	if detail != nil {
		currentPrimary = fmt.Sprintf("%s:%d", detail.MasterIP, detail.MasterPort)
	}

	for _, addr := range sentinelAddrs {
		client, err := newSentinelClient(addr, sentinelPassword)
		if err != nil {
			continue
		}
		err = client.Do(ctx, client.B().Arbitrary("SENTINEL", "FAILOVER", masterName).Build()).Error()
		client.Close()
		if err != nil {
			slog.Debug("SENTINEL FAILOVER failed", "addr", addr, "error", err)
			continue
		}

		slog.Info("SENTINEL FAILOVER sent", "addr", addr, "master", masterName, "primary", currentPrimary)
		pip := ""
		piport := 0
		if detail != nil {
			pip = detail.MasterIP
			piport = detail.MasterPort
		}
		return &TestFailoverResult{
			Success:     true,
			PrimaryIP:   pip,
			PrimaryPort: piport,
			Message:     fmt.Sprintf("SENTINEL FAILOVER sent (via %s). Current primary %s will be replaced.", addr, currentPrimary),
		}
	}

	pip := ""
	piport := 0
	if detail != nil {
		pip = detail.MasterIP
		piport = detail.MasterPort
	}
	return &TestFailoverResult{
		Success:     false,
		PrimaryIP:   pip,
		PrimaryPort: piport,
		Message:     "All sentinels failed to execute FAILOVER.",
	}
}

// flatSliceToMap은 [key, value, key, value, ...] 형태의 슬라이스를 맵으로 변환한다.
func flatSliceToMap(items []string) map[string]string {
	m := make(map[string]string, len(items)/2)
	for i := 0; i+1 < len(items); i += 2 {
		m[items[i]] = items[i+1]
	}
	return m
}
