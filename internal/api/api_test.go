package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chals-go/valkey-sentinel-manager/internal/core"
	"github.com/chals-go/valkey-sentinel-manager/internal/dns"
	"github.com/chals-go/valkey-sentinel-manager/internal/store"
)

func setupTestMux() (*http.ServeMux, store.Store) {
	s := store.NewMemoryStore(30)
	// Set up a test API token so auth passes.
	s.SetAPIToken(nil, "test-token-123")
	ep := core.NewEventProcessor(s)
	fm := core.NewFailoverManager(s, ep, map[string]dns.Provider{})
	mux := http.NewServeMux()
	RegisterRoutes(mux, s, fm, map[string]dns.Provider{})
	return mux, s
}

func authHeader(req *http.Request) {
	req.Header.Set("Authorization", "Bearer test-token-123")
}

func TestHealthEndpoint(t *testing.T) {
	mux, _ := setupTestMux()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp Response
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Status != "ok" {
		t.Fatalf("status = %q, want ok", resp.Status)
	}
}

func TestTokenAuth_NoToken(t *testing.T) {
	// Fresh store with no tokens at all.
	s := store.NewMemoryStore(30)
	ep := core.NewEventProcessor(s)
	fm := core.NewFailoverManager(s, ep, map[string]dns.Provider{})
	mux := http.NewServeMux()
	RegisterRoutes(mux, s, fm, map[string]dns.Provider{})

	// No token configured — should DENY access (401).
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (no token = deny)", rec.Code, http.StatusUnauthorized)
	}
}

func TestTokenAuth_WithToken(t *testing.T) {
	mux, s := setupTestMux()

	// Set a token.
	s.SetAPIToken(nil, "smgr_testtoken123")

	// Request without auth header → 401.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	// Request with wrong token → 401.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	req.Header.Set("Authorization", "Bearer wrong_token")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	// Request with correct token → 200.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	req.Header.Set("Authorization", "Bearer smgr_testtoken123")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("correct token: status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestClusterCRUD(t *testing.T) {
	mux, _ := setupTestMux()

	// Create cluster.
	body := `{
		"group_name": "test-group",
		"master_name": "test-master",
		"sentinel_addrs": ["127.0.0.1:26379"],
		"dns_provider": "bind",
		"primary_dns": {"zone": "example.com", "record_name": "primary", "record_type": "A", "ttl": 3},
		"quorum_mode": true,
		"quorum_threshold": 2
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/clusters", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	authHeader(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	// List clusters.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil)
	authHeader(req)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status = %d", rec.Code)
	}

	// Get cluster.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/clusters/test-master", nil)
	authHeader(req)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: status = %d", rec.Code)
	}

	// Duplicate → 409.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/clusters", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	authHeader(req)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate: status = %d, want %d", rec.Code, http.StatusConflict)
	}

	// Delete cluster.
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/clusters/test-master", nil)
	authHeader(req)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: status = %d", rec.Code)
	}

	// Get after delete → 404.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/clusters/test-master", nil)
	authHeader(req)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get deleted: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestSentinelCRUD(t *testing.T) {
	mux, _ := setupTestMux()

	// Create sentinel.
	body := `{"sentinel_node_name": "s1", "group_name": "grp1", "host": "10.0.0.1", "port": 26379}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sentinels", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	authHeader(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, body: %s", rec.Code, rec.Body.String())
	}

	// List sentinels.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/sentinels", nil)
	authHeader(req)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status = %d", rec.Code)
	}

	// Get sentinel.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/sentinels/s1", nil)
	authHeader(req)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: status = %d", rec.Code)
	}

	// Duplicate → 409.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/sentinels", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	authHeader(req)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate: status = %d, want %d", rec.Code, http.StatusConflict)
	}

	// Delete sentinel.
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/sentinels/s1", nil)
	authHeader(req)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: status = %d", rec.Code)
	}

	// Get after delete → 404.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/sentinels/s1", nil)
	authHeader(req)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get deleted: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestEventCreate(t *testing.T) {
	mux, _ := setupTestMux()

	body := `{
		"group_name": "g1", "master_name": "m1", "event_type": "failover",
		"role": "leader", "state": "promoted",
		"from_ip": "10.0.0.1", "from_port": 6379,
		"to_ip": "10.0.0.2", "to_port": 6379,
		"sentinel_node_name": "s1"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/events", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	authHeader(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	var resp Response
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Status != "ok" {
		t.Fatalf("resp status = %q", resp.Status)
	}
}

func TestListEvents(t *testing.T) {
	mux, _ := setupTestMux()

	// Create an event first.
	body := `{"group_name":"g1","master_name":"m1","event_type":"failover","from_ip":"10.0.0.1","from_port":6379,"to_ip":"10.0.0.2","to_port":6379,"sentinel_node_name":"s1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/events", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	authHeader(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// List events.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	authHeader(req)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("list events: status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp Response
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Status != "ok" {
		t.Fatalf("resp status = %q", resp.Status)
	}
}

func TestCreateEvent_InvalidJSON(t *testing.T) {
	mux, _ := setupTestMux()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/events", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	authHeader(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateCluster_InvalidJSON(t *testing.T) {
	mux, _ := setupTestMux()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/clusters", bytes.NewBufferString("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	authHeader(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateSentinel_InvalidJSON(t *testing.T) {
	mux, _ := setupTestMux()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sentinels", bytes.NewBufferString("bad"))
	req.Header.Set("Content-Type", "application/json")
	authHeader(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestDeleteCluster_NotFound(t *testing.T) {
	mux, _ := setupTestMux()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/clusters/nonexistent", nil)
	authHeader(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestDeleteSentinel_NotFound(t *testing.T) {
	mux, _ := setupTestMux()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/sentinels/nonexistent", nil)
	authHeader(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
