// Package core는 센티널 이벤트 처리, 페일오버 관리, 헬스체크 등 핵심 비즈니스 로직을 제공한다.
package core

import (
	"context"
	"log/slog"

	"github.com/chals-go/valkey-sentinel-manager/internal/models"
	"github.com/chals-go/valkey-sentinel-manager/internal/store"
)

// EventProcessResult는 단일 이벤트 처리 결과를 담는 구조체이다.
type EventProcessResult struct {
	Event           *models.FailoverEvent
	IsDuplicate     bool
	ReportCount     int
	QuorumReached   bool
	ShouldUpdateDNS bool
}

// EventProcessor는 이벤트 중복 제거와 쿼럼 판정을 담당하는 구조체이다.
type EventProcessor struct {
	store store.Store
}

// NewEventProcessor는 EventProcessor를 생성하여 반환한다.
func NewEventProcessor(s store.Store) *EventProcessor {
	return &EventProcessor{store: s}
}

// Process는 이벤트를 기록하고 DNS 업데이트 필요 여부를 판정한다.
//
// 동작 방식:
//   - 이벤트를 기록하고 현재 보고 횟수를 가져온다.
//   - 쿼럼 모드: report_count == quorum_threshold 일 때 DNS 업데이트를 트리거한다.
//   - 선착순 모드(quorum_mode=false): 첫 번째 보고에서만 트리거한다.
func (p *EventProcessor) Process(ctx context.Context, event *models.FailoverEvent, quorumMode bool, quorumThreshold int) (*EventProcessResult, error) {
	// Update sentinel last_seen.
	_ = p.store.UpdateSentinelLastSeen(ctx, event.SentinelNodeName, event.Timestamp)

	reportCount, err := p.store.RecordEvent(ctx, event)
	if err != nil {
		return nil, err
	}

	isDuplicate := reportCount > 1
	if isDuplicate {
		slog.Info("duplicate event received",
			"dedup_key", event.DedupKey(),
			"sentinel", event.SentinelNodeName,
			"count", reportCount,
		)
	}

	quorumReached := reportCount >= quorumThreshold

	targetCount := quorumThreshold
	if !quorumMode {
		targetCount = 1
	}
	shouldUpdateDNS := reportCount == targetCount

	if shouldUpdateDNS {
		mode := "quorum"
		if !quorumMode {
			mode = "first-come"
		}
		slog.Info("DNS update triggered",
			"cluster", event.GroupName,
			"master", event.MasterName,
			"new_ip", event.ToIP,
			"new_port", event.ToPort,
			"mode", mode,
		)
	}

	return &EventProcessResult{
		Event:           event,
		IsDuplicate:     isDuplicate,
		ReportCount:     reportCount,
		QuorumReached:   quorumReached,
		ShouldUpdateDNS: shouldUpdateDNS,
	}, nil
}
