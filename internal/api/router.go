package api

import (
	"net/http"

	"github.com/chals-go/valkey-sentinel-manager/internal/core"
	"github.com/chals-go/valkey-sentinel-manager/internal/dns"
	"github.com/chals-go/valkey-sentinel-manager/internal/store"
)

// RegisterRoutes는 주어진 ServeMux에 모든 API 라우트를 등록한다.
// /api/v1/health 엔드포인트는 인증 없이 접근 가능하며,
// 나머지 엔드포인트는 TokenAuth 미들웨어로 보호된다.
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
