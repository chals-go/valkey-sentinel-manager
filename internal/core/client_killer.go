package core

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/valkey-io/valkey-go"
)

const (
	clientKillMaxRetries = 2
	clientKillRetryDelay = 3 * time.Second
	clientKillTimeout    = 3 * time.Second
)

// KillOldPrimaryClients는 구 primary에 CLIENT KILL TYPE normal을 비동기로 실행한다.
// 기존 애플리케이션 커넥션을 강제 종료하여 클라이언트가 DNS를 다시 resolve하도록 유도한다.
// best-effort: 연결 실패 시 3초 대기 후 최대 2회 재시도하며, 실패해도 failover를 중단하지 않는다.
func KillOldPrimaryClients(addr string, port int, username, password string) {
	target := fmt.Sprintf("%s:%d", addr, port)

	for attempt := 0; attempt <= clientKillMaxRetries; attempt++ {
		if attempt > 0 {
			slog.Info("CLIENT KILL retry", "target", target, "attempt", attempt)
			time.Sleep(clientKillRetryDelay)
		}

		killed, err := executeClientKill(target, username, password)
		if err != nil {
			slog.Warn("CLIENT KILL failed", "target", target, "attempt", attempt, "error", err)
			continue
		}

		slog.Info("CLIENT KILL success", "target", target, "killed_connections", killed)
		return
	}

	slog.Error("CLIENT KILL exhausted all retries", "target", target, "max_retries", clientKillMaxRetries)
}

// executeClientKill은 대상 Valkey 인스턴스에 CLIENT KILL TYPE normal을 실행한다.
func executeClientKill(addr, username, password string) (int64, error) {
	opts := valkey.ClientOption{
		InitAddress:       []string{addr},
		Password:          password,
		DisableCache:      true,
		ForceSingleClient: true,
	}
	if username != "" {
		opts.Username = username
	}

	client, err := valkey.NewClient(opts)
	if err != nil {
		return 0, fmt.Errorf("connect to %s: %w", addr, err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), clientKillTimeout)
	defer cancel()

	resp := client.Do(ctx, client.B().Arbitrary("CLIENT", "KILL", "TYPE", "normal").Build())
	if resp.Error() != nil {
		return 0, fmt.Errorf("CLIENT KILL on %s: %w", addr, resp.Error())
	}

	killed, err := resp.AsInt64()
	if err != nil {
		slog.Info("CLIENT KILL response not integer (may be OK response)", "addr", addr)
		return 0, nil
	}
	return killed, nil
}
