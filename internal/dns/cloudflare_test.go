package dns

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cloudflare/cloudflare-go/v4"
	"github.com/cloudflare/cloudflare-go/v4/option"
)

func TestCloudflareProvider_NewProvider(t *testing.T) {
	// Valid params.
	p, err := NewCloudflareProvider("test-token", "zone-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("provider should not be nil")
	}

	// Missing API token.
	_, err = NewCloudflareProvider("", "zone-id")
	if err == nil {
		t.Fatal("empty api_token should return error")
	}

	// Missing zone ID.
	_, err = NewCloudflareProvider("test-token", "")
	if err == nil {
		t.Fatal("empty zone_id should return error")
	}
}

// cfTestServer creates an httptest server that simulates Cloudflare DNS API.
// It stores records in memory and handles List, New, Update, Delete operations.
type cfTestServer struct {
	mu      sync.Mutex
	records map[string]cfTestRecord // id -> record
	nextID  int
	srv     *httptest.Server
}

type cfTestRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

func newCFTestServer(zoneID string) *cfTestServer {
	ts := &cfTestServer{
		records: make(map[string]cfTestRecord),
		nextID:  1,
	}

	mux := http.NewServeMux()

	// List DNS records: GET /zones/{zone_id}/dns_records
	mux.HandleFunc("GET /zones/"+zoneID+"/dns_records", func(w http.ResponseWriter, r *http.Request) {
		ts.mu.Lock()
		defer ts.mu.Unlock()

		// Autopager가 page=2 이후를 요청하면 빈 결과를 반환하여 페이징 종료.
		if page := r.URL.Query().Get("page"); page != "" && page != "1" {
			resp := map[string]any{
				"result":      []cfTestRecord{},
				"success":     true,
				"result_info": map[string]any{"total_count": 0, "page": 2, "per_page": 100, "total_pages": 1},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}

		nameFilter := r.URL.Query().Get("name.exact")
		typeFilter := r.URL.Query().Get("type")

		var result []cfTestRecord
		for _, rec := range ts.records {
			if nameFilter != "" && rec.Name != nameFilter {
				continue
			}
			if typeFilter != "" && rec.Type != typeFilter {
				continue
			}
			result = append(result, rec)
		}

		resp := map[string]any{
			"result":      result,
			"success":     true,
			"result_info": map[string]any{"total_count": len(result), "page": 1, "per_page": 100, "total_pages": 1},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Create DNS record: POST /zones/{zone_id}/dns_records
	mux.HandleFunc("POST /zones/"+zoneID+"/dns_records", func(w http.ResponseWriter, r *http.Request) {
		ts.mu.Lock()
		defer ts.mu.Unlock()

		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		json.Unmarshal(body, &req)

		id := ts.allocID()
		ttl := 0
		if v, ok := req["ttl"]; ok {
			if f, ok := v.(float64); ok {
				ttl = int(f)
			}
		}
		rec := cfTestRecord{
			ID:      id,
			Name:    strVal(req, "name"),
			Type:    strVal(req, "type"),
			Content: strVal(req, "content"),
			TTL:     ttl,
		}
		ts.records[id] = rec

		resp := map[string]any{"result": rec, "success": true}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	})

	// Update DNS record: PUT /zones/{zone_id}/dns_records/{id}
	mux.HandleFunc("PUT /zones/"+zoneID+"/dns_records/", func(w http.ResponseWriter, r *http.Request) {
		ts.mu.Lock()
		defer ts.mu.Unlock()

		parts := strings.Split(r.URL.Path, "/")
		recordID := parts[len(parts)-1]

		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		json.Unmarshal(body, &req)

		rec, ok := ts.records[recordID]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]any{"success": false})
			return
		}
		rec.Content = strVal(req, "content")
		rec.Name = strVal(req, "name")
		if v, ok := req["ttl"]; ok {
			if f, ok := v.(float64); ok {
				rec.TTL = int(f)
			}
		}
		ts.records[recordID] = rec

		resp := map[string]any{"result": rec, "success": true}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Delete DNS record: DELETE /zones/{zone_id}/dns_records/{id}
	mux.HandleFunc("DELETE /zones/"+zoneID+"/dns_records/", func(w http.ResponseWriter, r *http.Request) {
		ts.mu.Lock()
		defer ts.mu.Unlock()

		parts := strings.Split(r.URL.Path, "/")
		recordID := parts[len(parts)-1]

		delete(ts.records, recordID)
		resp := map[string]any{"result": map[string]string{"id": recordID}, "success": true}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Get Zone: GET /zones/{zone_id}
	mux.HandleFunc("GET /zones/"+zoneID, func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"result":  map[string]string{"id": zoneID, "name": "example.com"},
			"success": true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	ts.srv = httptest.NewServer(mux)
	return ts
}

func (ts *cfTestServer) allocID() string {
	id := ts.nextID
	ts.nextID++
	return strings.Replace("rec-000", "000", strings.Repeat("0", 3), 1)[:0] + "rec-" + itoa(id)
}

func itoa(i int) string {
	return strings.TrimLeft(strings.Replace("   ", " ", "", -1), " ") + string(rune('0'+i%10))
}

func strVal(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func (ts *cfTestServer) close() {
	ts.srv.Close()
}

func (ts *cfTestServer) recordCount() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return len(ts.records)
}

func (ts *cfTestServer) addRecord(name, typ, content string, ttl int) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	id := ts.allocID()
	ts.records[id] = cfTestRecord{ID: id, Name: name, Type: typ, Content: content, TTL: ttl}
}

func newTestCloudflareProvider(baseURL, zoneID string) *CloudflareProvider {
	client := cloudflare.NewClient(
		option.WithAPIToken("test-token"),
		option.WithBaseURL(baseURL),
	)
	return &CloudflareProvider{client: client, zoneID: zoneID}
}

func TestCloudflareProvider_UpdateRecord_Create(t *testing.T) {
	zoneID := "zone1"
	ts := newCFTestServer(zoneID)
	defer ts.close()

	p := newTestCloudflareProvider(ts.srv.URL, zoneID)

	err := p.UpdateRecord(context.Background(), "example.com", "primary", "A", "10.0.0.1", 300)
	if err != nil {
		t.Fatalf("UpdateRecord: %v", err)
	}
	if ts.recordCount() != 1 {
		t.Fatalf("record count = %d, want 1", ts.recordCount())
	}
}

func TestCloudflareProvider_UpdateRecord_Update(t *testing.T) {
	zoneID := "zone1"
	ts := newCFTestServer(zoneID)
	defer ts.close()

	// Pre-add a record.
	ts.addRecord("primary.example.com", "A", "10.0.0.1", 300)

	p := newTestCloudflareProvider(ts.srv.URL, zoneID)

	err := p.UpdateRecord(context.Background(), "example.com", "primary", "A", "10.0.0.2", 300)
	if err != nil {
		t.Fatalf("UpdateRecord: %v", err)
	}
	// Should still be 1 record (updated, not added).
	if ts.recordCount() != 1 {
		t.Fatalf("record count = %d, want 1", ts.recordCount())
	}
}

func TestCloudflareProvider_UpdateRecordValues(t *testing.T) {
	zoneID := "zone1"
	ts := newCFTestServer(zoneID)
	defer ts.close()

	ts.addRecord("replica.example.com", "A", "10.0.0.1", 300)

	p := newTestCloudflareProvider(ts.srv.URL, zoneID)

	err := p.UpdateRecordValues(context.Background(), "example.com", "replica", "A", []string{"10.0.0.2", "10.0.0.3"}, 300)
	if err != nil {
		t.Fatalf("UpdateRecordValues: %v", err)
	}
	if ts.recordCount() != 2 {
		t.Fatalf("record count = %d, want 2", ts.recordCount())
	}
}

func TestCloudflareProvider_UpdateRecordValues_Empty(t *testing.T) {
	zoneID := "zone1"
	ts := newCFTestServer(zoneID)
	defer ts.close()

	ts.addRecord("replica.example.com", "A", "10.0.0.1", 300)

	p := newTestCloudflareProvider(ts.srv.URL, zoneID)

	err := p.UpdateRecordValues(context.Background(), "example.com", "replica", "A", nil, 300)
	if err != nil {
		t.Fatalf("UpdateRecordValues empty: %v", err)
	}
	// Original record should still exist.
	if ts.recordCount() != 1 {
		t.Fatalf("record count = %d, want 1 (unchanged)", ts.recordCount())
	}
}

func TestCloudflareProvider_AddRecordValue(t *testing.T) {
	zoneID := "zone1"
	ts := newCFTestServer(zoneID)
	defer ts.close()

	ts.addRecord("replica.example.com", "A", "10.0.0.1", 300)

	p := newTestCloudflareProvider(ts.srv.URL, zoneID)

	err := p.AddRecordValue(context.Background(), "example.com", "replica", "A", "10.0.0.2", 300)
	if err != nil {
		t.Fatalf("AddRecordValue: %v", err)
	}
	if ts.recordCount() != 2 {
		t.Fatalf("record count = %d, want 2", ts.recordCount())
	}
}

func TestCloudflareProvider_AddRecordValue_Duplicate(t *testing.T) {
	zoneID := "zone1"
	ts := newCFTestServer(zoneID)
	defer ts.close()

	ts.addRecord("replica.example.com", "A", "10.0.0.1", 300)

	p := newTestCloudflareProvider(ts.srv.URL, zoneID)

	// Adding the same value should be a no-op.
	err := p.AddRecordValue(context.Background(), "example.com", "replica", "A", "10.0.0.1", 300)
	if err != nil {
		t.Fatalf("AddRecordValue duplicate: %v", err)
	}
	if ts.recordCount() != 1 {
		t.Fatalf("record count = %d, want 1 (no duplicate added)", ts.recordCount())
	}
}

func TestCloudflareProvider_RemoveRecordValue(t *testing.T) {
	zoneID := "zone1"
	ts := newCFTestServer(zoneID)
	defer ts.close()

	ts.addRecord("replica.example.com", "A", "10.0.0.1", 300)
	ts.addRecord("replica.example.com", "A", "10.0.0.2", 300)

	p := newTestCloudflareProvider(ts.srv.URL, zoneID)

	err := p.RemoveRecordValue(context.Background(), "example.com", "replica", "A", "10.0.0.1")
	if err != nil {
		t.Fatalf("RemoveRecordValue: %v", err)
	}
	if ts.recordCount() != 1 {
		t.Fatalf("record count = %d, want 1", ts.recordCount())
	}
}

func TestCloudflareProvider_RemoveRecordValue_Last(t *testing.T) {
	zoneID := "zone1"
	ts := newCFTestServer(zoneID)
	defer ts.close()

	ts.addRecord("replica.example.com", "A", "10.0.0.1", 300)

	p := newTestCloudflareProvider(ts.srv.URL, zoneID)

	err := p.RemoveRecordValue(context.Background(), "example.com", "replica", "A", "10.0.0.1")
	if err == nil {
		t.Fatal("removing last record value should return error")
	}
	// Record should still exist.
	if ts.recordCount() != 1 {
		t.Fatalf("record count = %d, want 1 (unchanged)", ts.recordCount())
	}
}

func TestCloudflareProvider_DeleteRecord(t *testing.T) {
	zoneID := "zone1"
	ts := newCFTestServer(zoneID)
	defer ts.close()

	ts.addRecord("primary.example.com", "A", "10.0.0.1", 300)
	ts.addRecord("primary.example.com", "A", "10.0.0.2", 300)

	p := newTestCloudflareProvider(ts.srv.URL, zoneID)

	err := p.DeleteRecord(context.Background(), "example.com", "primary", "A")
	if err != nil {
		t.Fatalf("DeleteRecord: %v", err)
	}
	if ts.recordCount() != 0 {
		t.Fatalf("record count = %d, want 0", ts.recordCount())
	}
}

func TestCloudflareProvider_VerifyRecord(t *testing.T) {
	zoneID := "zone1"
	ts := newCFTestServer(zoneID)
	defer ts.close()

	ts.addRecord("primary.example.com", "A", "10.0.0.1", 300)

	p := newTestCloudflareProvider(ts.srv.URL, zoneID)

	found, err := p.VerifyRecord(context.Background(), "example.com", "primary", "10.0.0.1")
	if err != nil {
		t.Fatalf("VerifyRecord: %v", err)
	}
	if !found {
		t.Fatal("VerifyRecord should return true for existing value")
	}

	found, err = p.VerifyRecord(context.Background(), "example.com", "primary", "10.0.0.99")
	if err != nil {
		t.Fatalf("VerifyRecord: %v", err)
	}
	if found {
		t.Fatal("VerifyRecord should return false for non-existing value")
	}
}

func TestCloudflareProvider_HealthCheck(t *testing.T) {
	zoneID := "zone1"
	ts := newCFTestServer(zoneID)
	defer ts.close()

	p := newTestCloudflareProvider(ts.srv.URL, zoneID)

	err := p.HealthCheck(context.Background())
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
}

func TestCloudflareProvider_HealthCheck_Fail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []map[string]any{{"code": 10000, "message": "unauthorized"}}})
	}))
	defer srv.Close()

	p := newTestCloudflareProvider(srv.URL, "bad-zone")
	err := p.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("HealthCheck with bad zone should return error")
	}
}
