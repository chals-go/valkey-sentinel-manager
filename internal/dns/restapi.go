//go:build !dns_select || dns_restapi

package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

func init() {
	Register("restapi", "REST API", func(_ context.Context, cfg map[string]string) (Provider, error) {
		return NewRestAPIProvider(cfg)
	})
}

// endpointCfg는 REST API 엔드포인트 설정을 나타낸다.
type endpointCfg struct {
	Method string
	URL    string
	Body   string
}

func (e endpointCfg) isSet() bool {
	return e.Method != "" && e.URL != ""
}

// RestAPIProvider는 사용자 정의 REST API를 통해 DNS 레코드를 관리하는 범용 프로바이더이다.
type RestAPIProvider struct {
	baseURL   string
	headers   map[string]string
	client    *http.Client
	updateCfg endpointCfg
	multiCfg  endpointCfg
	deleteCfg endpointCfg
	healthCfg endpointCfg
}

// NewRestAPIProvider는 설정 맵으로부터 REST API 프로바이더를 생성한다.
func NewRestAPIProvider(cfg map[string]string) (*RestAPIProvider, error) {
	baseURL := strings.TrimRight(cfg["base_url"], "/")
	if baseURL == "" {
		return nil, fmt.Errorf("restapi: base_url is required")
	}

	headers := map[string]string{}
	if h := cfg["headers"]; h != "" {
		if err := json.Unmarshal([]byte(h), &headers); err != nil {
			return nil, fmt.Errorf("restapi: invalid headers JSON: %w", err)
		}
	}
	if _, ok := headers["Content-Type"]; !ok {
		headers["Content-Type"] = "application/json"
	}

	return &RestAPIProvider{
		baseURL: baseURL,
		headers: headers,
		client:  &http.Client{Timeout: 10 * time.Second},
		updateCfg: endpointCfg{
			Method: cfg["update_method"],
			URL:    cfg["update_url"],
			Body:   cfg["update_body"],
		},
		multiCfg: endpointCfg{
			Method: cfg["update_multi_method"],
			URL:    cfg["update_multi_url"],
			Body:   cfg["update_multi_body"],
		},
		deleteCfg: endpointCfg{
			Method: cfg["delete_method"],
			URL:    cfg["delete_url"],
			Body:   cfg["delete_body"],
		},
		healthCfg: endpointCfg{
			Method: cfg["health_method"],
			URL:    cfg["health_url"],
		},
	}, nil
}

// renderTemplate은 템플릿 문자열에서 $변수를 실제 값으로 치환한다.
// 긴 키부터 먼저 치환하여 $ip가 $ips보다 먼저 치환되는 문제를 방지한다.
func renderTemplate(tmpl string, vars map[string]string) string {
	// 긴 키부터 치환 (예: $ips → $ip 순서).
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	// Sort by length descending, then alphabetically.
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if len(keys[j]) > len(keys[i]) || (len(keys[j]) == len(keys[i]) && keys[j] < keys[i]) {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	result := tmpl
	for _, k := range keys {
		result = strings.ReplaceAll(result, "$"+k, vars[k])
	}
	return result
}

// buildVars는 Provider 메서드 파라미터로부터 치환용 변수 맵을 생성한다.
func buildVars(zone, name, recordType, ip string, ips []string, ttl int) map[string]string {
	domain := name
	if zone != "" {
		domain = name + "." + zone
	}

	ipsJSON := "[]"
	if len(ips) > 0 {
		b, _ := json.Marshal(ips)
		ipsJSON = string(b)
	}

	return map[string]string{
		"domain":      domain,
		"name":        name,
		"zone":        zone,
		"record_type": recordType,
		"ip":          ip,
		"ips":         ipsJSON,
		"ttl":         fmt.Sprintf("%d", ttl),
	}
}

func (p *RestAPIProvider) doRequest(ctx context.Context, method, url, body string) (*http.Response, error) {
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	for k, v := range p.headers {
		req.Header.Set(k, v)
	}
	return p.client.Do(req)
}

func (p *RestAPIProvider) callEndpoint(ctx context.Context, ep endpointCfg, vars map[string]string) error {
	url := p.baseURL + renderTemplate(ep.URL, vars)
	body := renderTemplate(ep.Body, vars)

	resp, err := p.doRequest(ctx, ep.Method, url, body)
	if err != nil {
		return fmt.Errorf("restapi request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("restapi: HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(respBody))
	}
	return nil
}

// UpdateRecord는 단일 값 DNS 레코드를 설정한다.
func (p *RestAPIProvider) UpdateRecord(ctx context.Context, zone, name, recordType, value string, ttl int) error {
	if !p.updateCfg.isSet() {
		return fmt.Errorf("restapi: update_record endpoint not configured")
	}
	vars := buildVars(zone, name, recordType, value, nil, ttl)
	if err := p.callEndpoint(ctx, p.updateCfg, vars); err != nil {
		return err
	}
	slog.Info("restapi record updated", "record", name+"."+zone, "value", value)
	return nil
}

// UpdateRecordValues는 다중 값 DNS 레코드의 모든 값을 교체한다.
func (p *RestAPIProvider) UpdateRecordValues(ctx context.Context, zone, name, recordType string, values []string, ttl int) error {
	if len(values) == 0 {
		slog.Warn("empty values, keeping record", "record", name+"."+zone)
		return nil
	}
	if p.multiCfg.isSet() {
		vars := buildVars(zone, name, recordType, values[0], values, ttl)
		if err := p.callEndpoint(ctx, p.multiCfg, vars); err != nil {
			return err
		}
		slog.Info("restapi multi-value updated", "record", name+"."+zone, "values", values)
		return nil
	}
	// Fallback: update endpoint로 마지막 IP 설정.
	if p.updateCfg.isSet() {
		slog.Warn("restapi: update_multi not configured, falling back to update with last IP", "record", name+"."+zone)
		return p.UpdateRecord(ctx, zone, name, recordType, values[len(values)-1], ttl)
	}
	return fmt.Errorf("restapi: no update endpoint configured")
}

// AddRecordValue는 다중 값 DNS 레코드에 값을 추가한다.
func (p *RestAPIProvider) AddRecordValue(ctx context.Context, zone, name, recordType, value string, ttl int) error {
	if p.updateCfg.isSet() {
		vars := buildVars(zone, name, recordType, value, nil, ttl)
		if err := p.callEndpoint(ctx, p.updateCfg, vars); err != nil {
			return err
		}
		slog.Info("restapi record value added (via update)", "record", name+"."+zone, "value", value)
		return nil
	}
	return fmt.Errorf("restapi: update endpoint not configured")
}

// RemoveRecordValue는 다중 값 DNS 레코드에서 특정 값을 제거한다.
func (p *RestAPIProvider) RemoveRecordValue(ctx context.Context, zone, name, recordType, value string) error {
	if !p.deleteCfg.isSet() {
		slog.Warn("restapi: delete endpoint not configured, skipping remove", "record", name+"."+zone, "value", value)
		return nil
	}
	vars := buildVars(zone, name, recordType, value, nil, 0)
	if err := p.callEndpoint(ctx, p.deleteCfg, vars); err != nil {
		return err
	}
	slog.Info("restapi record value removed", "record", name+"."+zone, "value", value)
	return nil
}

// DeleteRecord는 DNS 레코드 전체를 삭제한다.
func (p *RestAPIProvider) DeleteRecord(ctx context.Context, zone, name, recordType string) error {
	if !p.deleteCfg.isSet() {
		slog.Warn("restapi: delete endpoint not configured, skipping delete", "record", name+"."+zone)
		return nil
	}
	vars := buildVars(zone, name, recordType, "", nil, 0)
	if err := p.callEndpoint(ctx, p.deleteCfg, vars); err != nil {
		return err
	}
	return nil
}

// VerifyRecord는 DNS 조회를 통해 레코드가 기대하는 값을 가지고 있는지 확인한다.
func (p *RestAPIProvider) VerifyRecord(_ context.Context, zone, name, expectedValue string) (bool, error) {
	fqdn := name + "." + zone
	ips, err := net.LookupHost(fqdn)
	if err != nil {
		return false, fmt.Errorf("restapi verify: DNS lookup failed for %s: %w", fqdn, err)
	}
	for _, ip := range ips {
		if ip == expectedValue {
			return true, nil
		}
	}
	return false, nil
}

// HealthCheck는 헬스체크 엔드포인트 또는 base URL로 연결 상태를 확인한다.
func (p *RestAPIProvider) HealthCheck(ctx context.Context) error {
	method := p.healthCfg.Method
	url := p.healthCfg.URL
	if method == "" {
		method = http.MethodGet
	}
	if url == "" {
		url = "/"
	}
	fullURL := p.baseURL + url
	resp, err := p.doRequest(ctx, method, fullURL, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body) //nolint:errcheck
	if resp.StatusCode >= 400 {
		return fmt.Errorf("restapi health check: HTTP %d", resp.StatusCode)
	}
	return nil
}
