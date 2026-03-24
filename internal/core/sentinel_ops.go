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

// SlaveInfo holds information about a replica reported by Sentinel.
type SlaveInfo struct {
	IP     string `json:"ip"`
	Port   int    `json:"port"`
	Flags  string `json:"flags"`
	Status string `json:"status"`
}

// MasterDetail holds master and replica information from Sentinel.
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

// TestFailoverResult holds the result of a test failover command.
type TestFailoverResult struct {
	Success     bool   `json:"success"`
	PrimaryIP   string `json:"primary_ip"`
	PrimaryPort int    `json:"primary_port"`
	Message     string `json:"message"`
}

// parseSentinelAddr splits "host:port" into host and port.
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

// PingSentinel checks connectivity to a Sentinel instance.
func PingSentinel(ctx context.Context, host string, port int, password string) bool {
	addr := fmt.Sprintf("%s:%d", host, port)
	client, err := newSentinelClient(addr, password)
	if err != nil {
		return false
	}
	defer client.Close()
	return client.Do(ctx, client.B().Ping().Build()).Error() == nil
}

// SentinelMonitor registers a new master on all given Sentinel instances.
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

// SentinelSetConfig updates down-after-milliseconds and failover-timeout on Sentinels.
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

// SentinelRemove removes a master from all given Sentinel instances.
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

// GetMasterDetail queries Sentinel for master + slave details.
// Tries each Sentinel address in order; returns nil if all fail.
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

// parseSentinelResult tries to parse a ValkeyResult as a map (RESP3) or flat array (RESP2).
func parseSentinelResult(resp valkey.ValkeyResult) (map[string]string, error) {
	if m, err := resp.AsStrMap(); err == nil {
		return m, nil
	}
	if items, err := resp.AsStrSlice(); err == nil {
		return flatSliceToMap(items), nil
	}
	return nil, fmt.Errorf("cannot parse sentinel response")
}

// parseSentinelMessage tries to parse a ValkeyMessage as a map (RESP3) or flat array (RESP2).
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

// TriggerTestFailover sends SENTINEL FAILOVER to force an immediate failover.
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

// flatSliceToMap converts [key, value, key, value, ...] into a map.
func flatSliceToMap(items []string) map[string]string {
	m := make(map[string]string, len(items)/2)
	for i := 0; i+1 < len(items); i += 2 {
		m[items[i]] = items[i+1]
	}
	return m
}
