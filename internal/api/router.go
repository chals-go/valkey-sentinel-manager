package api

import (
	"net/http"

	"github.com/chals-go/valkey-sentinel-manager/internal/core"
	"github.com/chals-go/valkey-sentinel-manager/internal/dns"
	"github.com/chals-go/valkey-sentinel-manager/internal/store"
)

// RegisterRoutes registers all API routes on the given mux.
func RegisterRoutes(mux *http.ServeMux, s store.Store, fm *core.FailoverManager, providers map[string]dns.Provider) {
	// Health — no auth required.
	mux.HandleFunc("GET /api/v1/health", HealthHandler(providers))

	// Token auth middleware for the rest.
	authMW := TokenAuth(s)

	// Events
	mux.Handle("POST /api/v1/events", authMW(CreateEventHandler(s, fm)))
	mux.Handle("GET /api/v1/events", authMW(ListEventsHandler(s)))

	// Clusters
	mux.Handle("GET /api/v1/clusters", authMW(ListClustersHandler(s)))
	mux.Handle("POST /api/v1/clusters", authMW(CreateClusterHandler(s)))
	mux.Handle("GET /api/v1/clusters/{masterName}", authMW(GetClusterHandler(s)))
	mux.Handle("DELETE /api/v1/clusters/{masterName}", authMW(DeleteClusterHandler(s)))

	// Sentinels
	mux.Handle("GET /api/v1/sentinels", authMW(ListSentinelsHandler(s)))
	mux.Handle("POST /api/v1/sentinels", authMW(CreateSentinelHandler(s)))
	mux.Handle("GET /api/v1/sentinels/{name}", authMW(GetSentinelHandler(s)))
	mux.Handle("DELETE /api/v1/sentinels/{name}", authMW(DeleteSentinelHandler(s)))
}
