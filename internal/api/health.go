package api

import (
	"net/http"

	"github.com/chals-go/valkey-sentinel-manager/internal/dns"
)

// HealthHandler returns the health check handler.
func HealthHandler(providers map[string]dns.Provider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providerStatus := make(map[string]bool, len(providers))
		for name, p := range providers {
			err := p.HealthCheck(r.Context())
			providerStatus[name] = err == nil
		}

		allHealthy := true
		healthyCount := 0
		for _, ok := range providerStatus {
			if ok {
				healthyCount++
			} else {
				allHealthy = false
			}
		}
		if len(providerStatus) == 0 {
			allHealthy = true
		}

		status := "ok"
		msg := "healthy"
		if !allHealthy {
			status = "error"
			msg = "some dns providers unhealthy"
		}

		writeJSON(w, http.StatusOK, Response{
			Status: status,
			Data: map[string]any{
				"service":                "sentinel-manager",
				"dns_providers_count":    len(providerStatus),
				"dns_providers_healthy":  healthyCount,
			},
			Message: msg,
		})
	}
}
