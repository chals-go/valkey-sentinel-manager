//go:build !dns_select || dns_cloudflare

package dns

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cloudflare/cloudflare-go/v4"
	cfdns "github.com/cloudflare/cloudflare-go/v4/dns"
	"github.com/cloudflare/cloudflare-go/v4/option"
	"github.com/cloudflare/cloudflare-go/v4/zones"
)

func init() {
	Register("cloudflare", "Cloudflare", func(_ context.Context, cfg map[string]string) (Provider, error) {
		return NewCloudflareProvider(cfg["api_token"], cfg["zone_id"])
	})
}

// CloudflareProvider는 Cloudflare DNS API를 통해 DNS 레코드를 관리하는 프로바이더이다.
type CloudflareProvider struct {
	client *cloudflare.Client
	zoneID string
}

// NewCloudflareProvider는 Cloudflare DNS 프로바이더를 생성한다.
func NewCloudflareProvider(apiToken, zoneID string) (*CloudflareProvider, error) {
	if apiToken == "" {
		return nil, fmt.Errorf("cloudflare api token is required")
	}
	if zoneID == "" {
		return nil, fmt.Errorf("cloudflare zone id is required")
	}
	client := cloudflare.NewClient(option.WithAPIToken(apiToken))
	return &CloudflareProvider{client: client, zoneID: zoneID}, nil
}

type cfRecord struct {
	ID      string
	Content string
	TTL     int64
}

// listRecords는 지정된 name과 recordType에 해당하는 Cloudflare DNS 레코드 목록을 조회한다.
func (p *CloudflareProvider) listRecords(ctx context.Context, zone, name, recordType string) ([]cfRecord, error) {
	fullName := name + "." + zone
	pager := p.client.DNS.Records.ListAutoPaging(ctx, cfdns.RecordListParams{
		ZoneID: cloudflare.F(p.zoneID),
		Name: cloudflare.F(cfdns.RecordListParamsName{
			Exact: cloudflare.F(fullName),
		}),
		Type: cloudflare.F(cfdns.RecordListParamsType(recordType)),
	})

	var records []cfRecord
	for pager.Next() {
		r := pager.Current()
		records = append(records, cfRecord{
			ID:      r.ID,
			Content: r.Content,
			TTL:     int64(r.TTL),
		})
	}
	if err := pager.Err(); err != nil {
		return nil, fmt.Errorf("cloudflare list records %s.%s: %w", name, zone, err)
	}
	return records, nil
}

// UpdateRecord는 단일 값 DNS 레코드를 Cloudflare에 업서트한다.
func (p *CloudflareProvider) UpdateRecord(ctx context.Context, zone, name, recordType, value string, ttl int) error {
	existing, err := p.listRecords(ctx, zone, name, recordType)
	if err != nil {
		return err
	}

	fullName := name + "." + zone
	if len(existing) == 0 {
		_, err = p.client.DNS.Records.New(ctx, cfdns.RecordNewParams{
			ZoneID: cloudflare.F(p.zoneID),
			Body: cfdns.ARecordParam{
				Name:    cloudflare.F(fullName),
				Content: cloudflare.F(value),
				TTL:     cloudflare.F(cfdns.TTL(ttl)),
				Type:    cloudflare.F(cfdns.ARecordTypeA),
				Proxied: cloudflare.F(false),
			},
		})
		if err != nil {
			return fmt.Errorf("cloudflare create record: %w", err)
		}
	} else {
		_, err = p.client.DNS.Records.Update(ctx, existing[0].ID, cfdns.RecordUpdateParams{
			ZoneID: cloudflare.F(p.zoneID),
			Body: cfdns.ARecordParam{
				Name:    cloudflare.F(fullName),
				Content: cloudflare.F(value),
				TTL:     cloudflare.F(cfdns.TTL(ttl)),
				Type:    cloudflare.F(cfdns.ARecordTypeA),
				Proxied: cloudflare.F(false),
			},
		})
		if err != nil {
			return fmt.Errorf("cloudflare update record: %w", err)
		}
		// 동일 name에 여분 레코드가 있으면 삭제
		for _, r := range existing[1:] {
			_, _ = p.client.DNS.Records.Delete(ctx, r.ID, cfdns.RecordDeleteParams{
				ZoneID: cloudflare.F(p.zoneID),
			})
		}
	}
	slog.Info("cloudflare record updated", "record", fullName, "value", value)
	return nil
}

// UpdateRecordValues는 다중 값 DNS 레코드의 모든 값을 Cloudflare에서 교체한다.
func (p *CloudflareProvider) UpdateRecordValues(ctx context.Context, zone, name, recordType string, values []string, ttl int) error {
	if len(values) == 0 {
		slog.Warn("empty values, keeping record", "record", name+"."+zone)
		return nil
	}

	existing, err := p.listRecords(ctx, zone, name, recordType)
	if err != nil {
		return err
	}

	fullName := name + "." + zone

	// 기존 레코드 전부 삭제
	for _, r := range existing {
		_, _ = p.client.DNS.Records.Delete(ctx, r.ID, cfdns.RecordDeleteParams{
			ZoneID: cloudflare.F(p.zoneID),
		})
	}

	// 새 값 각각 생성
	for _, v := range values {
		_, err := p.client.DNS.Records.New(ctx, cfdns.RecordNewParams{
			ZoneID: cloudflare.F(p.zoneID),
			Body: cfdns.ARecordParam{
				Name:    cloudflare.F(fullName),
				Content: cloudflare.F(v),
				TTL:     cloudflare.F(cfdns.TTL(ttl)),
				Type:    cloudflare.F(cfdns.ARecordTypeA),
				Proxied: cloudflare.F(false),
			},
		})
		if err != nil {
			return fmt.Errorf("cloudflare create record value %s: %w", v, err)
		}
	}
	slog.Info("cloudflare multi-value replaced", "record", fullName, "values", values)
	return nil
}

// AddRecordValue는 Cloudflare의 다중 값 DNS 레코드에 값을 추가한다.
func (p *CloudflareProvider) AddRecordValue(ctx context.Context, zone, name, recordType, value string, ttl int) error {
	existing, err := p.listRecords(ctx, zone, name, recordType)
	if err != nil {
		return err
	}
	for _, r := range existing {
		if r.Content == value {
			slog.Info("cloudflare value already exists", "record", name+"."+zone, "value", value)
			return nil
		}
	}
	if len(existing) > 0 && existing[0].TTL > 0 {
		ttl = int(existing[0].TTL)
	}

	fullName := name + "." + zone
	_, err = p.client.DNS.Records.New(ctx, cfdns.RecordNewParams{
		ZoneID: cloudflare.F(p.zoneID),
		Body: cfdns.ARecordParam{
			Name:    cloudflare.F(fullName),
			Content: cloudflare.F(value),
			TTL:     cloudflare.F(cfdns.TTL(ttl)),
			Type:    cloudflare.F(cfdns.ARecordTypeA),
			Proxied: cloudflare.F(false),
		},
	})
	if err != nil {
		return fmt.Errorf("cloudflare add record value: %w", err)
	}
	slog.Info("cloudflare value added", "record", fullName, "value", value)
	return nil
}

// RemoveRecordValue는 Cloudflare의 다중 값 DNS 레코드에서 특정 값을 제거한다.
func (p *CloudflareProvider) RemoveRecordValue(ctx context.Context, zone, name, recordType, value string) error {
	existing, err := p.listRecords(ctx, zone, name, recordType)
	if err != nil {
		return err
	}

	var targetID string
	for _, r := range existing {
		if r.Content == value {
			targetID = r.ID
			break
		}
	}
	if targetID == "" {
		return nil
	}
	if len(existing) == 1 {
		slog.Warn("cannot remove last record value", "record", name+"."+zone)
		return fmt.Errorf("cannot remove last record value")
	}

	_, err = p.client.DNS.Records.Delete(ctx, targetID, cfdns.RecordDeleteParams{
		ZoneID: cloudflare.F(p.zoneID),
	})
	if err != nil {
		return fmt.Errorf("cloudflare remove record value: %w", err)
	}
	slog.Info("cloudflare value removed", "record", name+"."+zone, "value", value)
	return nil
}

// DeleteRecord는 Cloudflare에서 DNS 레코드 전체를 삭제한다.
func (p *CloudflareProvider) DeleteRecord(ctx context.Context, zone, name, recordType string) error {
	existing, err := p.listRecords(ctx, zone, name, recordType)
	if err != nil {
		return err
	}
	for _, r := range existing {
		if _, err := p.client.DNS.Records.Delete(ctx, r.ID, cfdns.RecordDeleteParams{
			ZoneID: cloudflare.F(p.zoneID),
		}); err != nil {
			return fmt.Errorf("cloudflare delete record %s: %w", r.ID, err)
		}
	}
	return nil
}

// VerifyRecord는 Cloudflare에서 A 레코드가 기대하는 값을 가지고 있는지 확인한다.
func (p *CloudflareProvider) VerifyRecord(ctx context.Context, zone, name, expectedValue string) (bool, error) {
	records, err := p.listRecords(ctx, zone, name, "A")
	if err != nil {
		return false, err
	}
	for _, r := range records {
		if r.Content == expectedValue {
			return true, nil
		}
	}
	return false, nil
}

// HealthCheck는 Cloudflare의 Zone 조회를 통해 연결 상태를 확인한다.
func (p *CloudflareProvider) HealthCheck(ctx context.Context) error {
	_, err := p.client.Zones.Get(ctx, zones.ZoneGetParams{
		ZoneID: cloudflare.F(p.zoneID),
	})
	return err
}
