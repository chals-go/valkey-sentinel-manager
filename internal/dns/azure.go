package dns

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
)

// AzureProvider manages DNS records via Azure DNS.
type AzureProvider struct {
	client        *armdns.RecordSetsClient
	zonesClient   *armdns.ZonesClient
	resourceGroup string
	zoneName      string
}

// NewAzureProvider creates an Azure DNS provider.
func NewAzureProvider(subscriptionID, resourceGroup, zoneName, authType, clientID, clientSecret, tenantID string) (*AzureProvider, error) {
	var cred *azidentity.DefaultAzureCredential
	var spCred *azidentity.ClientSecretCredential
	var err error

	switch authType {
	case "service_principal":
		spCred, err = azidentity.NewClientSecretCredential(tenantID, clientID, clientSecret, nil)
		if err != nil {
			return nil, fmt.Errorf("azure service principal auth: %w", err)
		}
		rsClient, err := armdns.NewRecordSetsClient(subscriptionID, spCred, nil)
		if err != nil {
			return nil, err
		}
		zClient, err := armdns.NewZonesClient(subscriptionID, spCred, nil)
		if err != nil {
			return nil, err
		}
		return &AzureProvider{client: rsClient, zonesClient: zClient, resourceGroup: resourceGroup, zoneName: zoneName}, nil
	default:
		cred, err = azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return nil, fmt.Errorf("azure default auth: %w", err)
		}
		rsClient, err := armdns.NewRecordSetsClient(subscriptionID, cred, nil)
		if err != nil {
			return nil, err
		}
		zClient, err := armdns.NewZonesClient(subscriptionID, cred, nil)
		if err != nil {
			return nil, err
		}
		return &AzureProvider{client: rsClient, zonesClient: zClient, resourceGroup: resourceGroup, zoneName: zoneName}, nil
	}
}

func (p *AzureProvider) zone(z string) string {
	if z != "" {
		return z
	}
	return p.zoneName
}

func (p *AzureProvider) UpdateRecord(ctx context.Context, zone, name, recordType, value string, ttl int) error {
	rs := armdns.RecordSet{
		Properties: &armdns.RecordSetProperties{
			TTL:      to.Ptr(int64(ttl)),
			ARecords: []*armdns.ARecord{{IPv4Address: to.Ptr(value)}},
		},
	}
	_, err := p.client.CreateOrUpdate(ctx, p.resourceGroup, p.zone(zone), name, armdns.RecordType(recordType), rs, nil)
	if err != nil {
		return fmt.Errorf("azure update record: %w", err)
	}
	slog.Info("azure record updated", "record", name+"."+zone, "value", value)
	return nil
}

func (p *AzureProvider) UpdateRecordValues(ctx context.Context, zone, name, recordType string, values []string, ttl int) error {
	if len(values) == 0 {
		slog.Warn("empty values, keeping record", "record", name+"."+zone)
		return nil
	}
	aRecords := make([]*armdns.ARecord, len(values))
	for i, v := range values {
		aRecords[i] = &armdns.ARecord{IPv4Address: to.Ptr(v)}
	}
	rs := armdns.RecordSet{
		Properties: &armdns.RecordSetProperties{
			TTL:      to.Ptr(int64(ttl)),
			ARecords: aRecords,
		},
	}
	_, err := p.client.CreateOrUpdate(ctx, p.resourceGroup, p.zone(zone), name, armdns.RecordType(recordType), rs, nil)
	if err != nil {
		return fmt.Errorf("azure update record values: %w", err)
	}
	slog.Info("azure multi-value replaced", "record", name+"."+zone, "values", values)
	return nil
}

func (p *AzureProvider) getExistingARecords(ctx context.Context, zone, name, recordType string) ([]*armdns.ARecord, int64) {
	resp, err := p.client.Get(ctx, p.resourceGroup, p.zone(zone), name, armdns.RecordType(recordType), nil)
	if err != nil {
		return nil, 0
	}
	ttl := int64(0)
	if resp.Properties != nil && resp.Properties.TTL != nil {
		ttl = *resp.Properties.TTL
	}
	if resp.Properties != nil {
		return resp.Properties.ARecords, ttl
	}
	return nil, ttl
}

func (p *AzureProvider) AddRecordValue(ctx context.Context, zone, name, recordType, value string, ttl int) error {
	existing, currentTTL := p.getExistingARecords(ctx, zone, name, recordType)
	for _, r := range existing {
		if r.IPv4Address != nil && *r.IPv4Address == value {
			return nil
		}
	}
	if currentTTL > 0 {
		ttl = int(currentTTL)
	}
	existing = append(existing, &armdns.ARecord{IPv4Address: to.Ptr(value)})
	rs := armdns.RecordSet{
		Properties: &armdns.RecordSetProperties{
			TTL:      to.Ptr(int64(ttl)),
			ARecords: existing,
		},
	}
	_, err := p.client.CreateOrUpdate(ctx, p.resourceGroup, p.zone(zone), name, armdns.RecordType(recordType), rs, nil)
	return err
}

func (p *AzureProvider) RemoveRecordValue(ctx context.Context, zone, name, recordType, value string) error {
	existing, currentTTL := p.getExistingARecords(ctx, zone, name, recordType)
	var newRecords []*armdns.ARecord
	for _, r := range existing {
		if r.IPv4Address != nil && *r.IPv4Address != value {
			newRecords = append(newRecords, r)
		}
	}
	if len(newRecords) == len(existing) {
		return nil // not found
	}
	if len(newRecords) == 0 {
		return fmt.Errorf("cannot remove last record value")
	}
	ttl := currentTTL
	if ttl == 0 {
		ttl = 3
	}
	rs := armdns.RecordSet{
		Properties: &armdns.RecordSetProperties{
			TTL:      to.Ptr(ttl),
			ARecords: newRecords,
		},
	}
	_, err := p.client.CreateOrUpdate(ctx, p.resourceGroup, p.zone(zone), name, armdns.RecordType(recordType), rs, nil)
	return err
}

func (p *AzureProvider) DeleteRecord(ctx context.Context, zone, name, recordType string) error {
	_, err := p.client.Delete(ctx, p.resourceGroup, p.zone(zone), name, armdns.RecordType(recordType), nil)
	return err
}

func (p *AzureProvider) VerifyRecord(ctx context.Context, zone, name, expectedValue string) (bool, error) {
	existing, _ := p.getExistingARecords(ctx, zone, name, "A")
	for _, r := range existing {
		if r.IPv4Address != nil && *r.IPv4Address == expectedValue {
			return true, nil
		}
	}
	return false, nil
}

func (p *AzureProvider) HealthCheck(ctx context.Context) error {
	_, err := p.zonesClient.Get(ctx, p.resourceGroup, p.zoneName, nil)
	return err
}
