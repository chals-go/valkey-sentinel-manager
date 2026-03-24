package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// BINDProvider manages DNS records via a BIND REST API.
type BINDProvider struct {
	apiURL string
	apiKey string
	client *http.Client
}

// NewBINDProvider creates a BIND DNS provider.
func NewBINDProvider(apiURL, apiKey string) *BINDProvider {
	return &BINDProvider{
		apiURL: strings.TrimRight(apiURL, "/"),
		apiKey: apiKey,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *BINDProvider) headers() map[string]string {
	h := map[string]string{"Content-Type": "application/json"}
	if p.apiKey != "" {
		h["Authorization"] = "Bearer " + p.apiKey
	}
	return h
}

func (p *BINDProvider) recordURL(zone, name, recordType string) string {
	base := fmt.Sprintf("%s/zones/%s/records/%s", p.apiURL, zone, name)
	if recordType != "" {
		return base + "/" + recordType
	}
	return base
}

func (p *BINDProvider) doRequest(ctx context.Context, method, url string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	for k, v := range p.headers() {
		req.Header.Set(k, v)
	}
	return p.client.Do(req)
}

func (p *BINDProvider) UpdateRecord(ctx context.Context, zone, name, recordType, value string, ttl int) error {
	resp, err := p.doRequest(ctx, http.MethodPut, p.recordURL(zone, name, recordType), map[string]any{
		"value": value,
		"ttl":   ttl,
	})
	if err != nil {
		return fmt.Errorf("bind update record: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("bind update record: HTTP %d", resp.StatusCode)
	}
	slog.Info("bind record updated", "record", name+"."+zone, "value", value)
	return nil
}

func (p *BINDProvider) UpdateRecordValues(ctx context.Context, zone, name, recordType string, values []string, ttl int) error {
	if len(values) == 0 {
		slog.Warn("empty values, keeping record", "record", name+"."+zone)
		return nil
	}
	fqdnName := name + "." + zone
	resp, err := p.doRequest(ctx, http.MethodPut, p.apiURL+"/dns/record", map[string]any{
		"zone":   zone,
		"name":   fqdnName,
		"type":   recordType,
		"ttl":    ttl,
		"values": values,
	})
	if err != nil {
		return fmt.Errorf("bind update record values: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("bind update record values: HTTP %d", resp.StatusCode)
	}
	slog.Info("bind multi-value replaced", "record", fqdnName, "values", values)
	return nil
}

func (p *BINDProvider) AddRecordValue(ctx context.Context, zone, name, recordType, value string, ttl int) error {
	resp, err := p.doRequest(ctx, http.MethodPost, p.recordURL(zone, name, recordType), map[string]any{
		"value": value,
		"ttl":   ttl,
	})
	if err != nil {
		return fmt.Errorf("bind add record value: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("bind add record value: HTTP %d", resp.StatusCode)
	}
	slog.Info("bind record value added", "record", name+"."+zone, "value", value)
	return nil
}

func (p *BINDProvider) RemoveRecordValue(ctx context.Context, zone, name, recordType, value string) error {
	url := p.recordURL(zone, name, recordType) + "?value=" + value
	resp, err := p.doRequest(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("bind remove record value: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("bind remove record value: HTTP %d", resp.StatusCode)
	}
	slog.Info("bind record value removed", "record", name+"."+zone, "value", value)
	return nil
}

func (p *BINDProvider) DeleteRecord(ctx context.Context, zone, name, recordType string) error {
	resp, err := p.doRequest(ctx, http.MethodDelete, p.recordURL(zone, name, recordType), nil)
	if err != nil {
		return fmt.Errorf("bind delete record: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("bind delete record: HTTP %d", resp.StatusCode)
	}
	return nil
}

func (p *BINDProvider) VerifyRecord(ctx context.Context, zone, name, expectedValue string) (bool, error) {
	resp, err := p.doRequest(ctx, http.MethodGet, p.recordURL(zone, name, ""), nil)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return false, nil
	}
	var data struct {
		Value  string   `json:"value"`
		Values []string `json:"values"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return false, err
	}
	values := data.Values
	if data.Value != "" {
		values = append(values, data.Value)
	}
	for _, v := range values {
		if v == expectedValue {
			return true, nil
		}
	}
	return false, nil
}

func (p *BINDProvider) HealthCheck(ctx context.Context) error {
	resp, err := p.doRequest(ctx, http.MethodGet, p.apiURL+"/health", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bind health check: HTTP %d", resp.StatusCode)
	}
	return nil
}
