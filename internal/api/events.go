package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/chals-go/valkey-sentinel-manager/internal/core"
	"github.com/chals-go/valkey-sentinel-manager/internal/models"
	"github.com/chals-go/valkey-sentinel-manager/internal/store"
)

type eventCreateRequest struct {
	GroupName        string `json:"group_name"`
	MasterName       string `json:"master_name"`
	EventType        string `json:"event_type"`
	Role             string `json:"role"`
	State            string `json:"state"`
	FromIP           string `json:"from_ip"`
	FromPort         int    `json:"from_port"`
	ToIP             string `json:"to_ip"`
	ToPort           int    `json:"to_port"`
	SentinelNodeName string `json:"sentinel_node_name"`
}

// CreateEventHandler receives a failover event from sentinel-agent.
func CreateEventHandler(s store.Store, fm *core.FailoverManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req eventCreateRequest
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		event := models.NewFailoverEvent(
			req.GroupName, req.MasterName, req.Role, req.State,
			req.FromIP, req.FromPort, req.ToIP, req.ToPort,
			req.SentinelNodeName, models.EventType(req.EventType),
		)

		slog.Info("event received",
			"type", event.EventType,
			"cluster", event.GroupName,
			"master", event.MasterName,
			"sentinel", event.SentinelNodeName,
		)

		// Process in background goroutine with timeout.
		go func() {
			bgCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			if _, err := fm.HandleEvent(bgCtx, event); err != nil {
				slog.Error("event processing failed", "error", err)
			}
		}()

		writeJSON(w, http.StatusAccepted, Response{
			Status:  "ok",
			Data:    map[string]string{"event_id": event.DedupKey(), "event_type": string(event.EventType)},
			Message: "event received",
		})
	}
}

// ListEventsHandler returns recent events.
func ListEventsHandler(s store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		events, err := s.GetRecentEvents(r.Context(), 50)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get events")
			return
		}
		writeJSON(w, http.StatusOK, Response{Status: "ok", Data: events, Message: "ok"})
	}
}
