// Package core implements the business logic for failover event processing.
package core

import (
	"context"
	"log/slog"

	"github.com/chals-go/valkey-sentinel-manager/internal/models"
	"github.com/chals-go/valkey-sentinel-manager/internal/store"
)

// EventProcessResult holds the outcome of processing a single event.
type EventProcessResult struct {
	Event           *models.FailoverEvent
	IsDuplicate     bool
	ReportCount     int
	QuorumReached   bool
	ShouldUpdateDNS bool
}

// EventProcessor handles event deduplication and quorum decisions.
type EventProcessor struct {
	store store.Store
}

// NewEventProcessor creates an EventProcessor.
func NewEventProcessor(s store.Store) *EventProcessor {
	return &EventProcessor{store: s}
}

// Process records an event and determines whether DNS should be updated.
//
// Logic:
//   - Record the event and get the current report count.
//   - Quorum mode: trigger DNS update when report_count == quorum_threshold.
//   - First-come mode (quorum_mode=false): trigger on the first report only.
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
