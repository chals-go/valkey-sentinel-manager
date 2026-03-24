package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/chals-go/valkey-sentinel-manager/internal/models"
	"github.com/chals-go/valkey-sentinel-manager/internal/store"
)

type sentinelCreateRequest struct {
	SentinelNodeName string `json:"sentinel_node_name"`
	GroupName        string `json:"group_name"`
	Host             string `json:"host"`
	Port             int    `json:"port"`
}

// ListSentinelsHandler returns registered sentinels.
func ListSentinelsHandler(s store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupName := r.URL.Query().Get("group_name")
		sentinels, err := s.ListSentinels(r.Context(), groupName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list sentinels")
			return
		}
		writeJSON(w, http.StatusOK, Response{Status: "ok", Data: sentinels, Message: "ok"})
	}
}

// CreateSentinelHandler registers a new sentinel.
func CreateSentinelHandler(s store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req sentinelCreateRequest
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		_, err := s.GetSentinel(r.Context(), req.SentinelNodeName)
		if err == nil {
			writeError(w, http.StatusConflict, "sentinel already registered: "+req.SentinelNodeName)
			return
		}

		sentinel := models.NewSentinel(req.SentinelNodeName, req.GroupName, req.Host, req.Port)
		if err := s.RegisterSentinel(r.Context(), sentinel); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to register sentinel")
			return
		}
		slog.Info("sentinel registered", "name", sentinel.SentinelNodeName, "group", sentinel.GroupName)
		writeJSON(w, http.StatusCreated, Response{Status: "ok", Data: sentinel, Message: "sentinel registered"})
	}
}

// GetSentinelHandler returns a sentinel by name.
func GetSentinelHandler(s store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		sentinel, err := s.GetSentinel(r.Context(), name)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, "sentinel not found: "+name)
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to get sentinel")
			return
		}
		writeJSON(w, http.StatusOK, Response{Status: "ok", Data: sentinel, Message: "ok"})
	}
}

// DeleteSentinelHandler unregisters a sentinel.
func DeleteSentinelHandler(s store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		removed, err := s.UnregisterSentinel(r.Context(), name)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to delete sentinel")
			return
		}
		if !removed {
			writeError(w, http.StatusNotFound, "sentinel not found: "+name)
			return
		}
		slog.Info("sentinel unregistered", "name", name)
		writeJSON(w, http.StatusOK, Response{Status: "ok", Message: "sentinel unregistered"})
	}
}
