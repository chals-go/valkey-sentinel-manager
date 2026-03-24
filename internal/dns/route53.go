package dns

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"
)

// Route53Provider는 AWS Route53을 통해 DNS 레코드를 관리하는 프로바이더이다.
type Route53Provider struct {
	client *route53.Client
	zoneID string
}

// NewRoute53Provider는 Route53 DNS 프로바이더를 생성한다.
// region, accessKey, secretKey가 비어 있으면 AWS 기본 자격 증명 체인을 사용한다.
func NewRoute53Provider(ctx context.Context, zoneID, region, accessKey, secretKey string) (*Route53Provider, error) {
	var opts []func(*awsconfig.LoadOptions) error
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	if accessKey != "" && secretKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("loading aws config: %w", err)
	}

	return &Route53Provider{
		client: route53.NewFromConfig(cfg),
		zoneID: zoneID,
	}, nil
}

func fqdn(recordName, zone string) string {
	name := recordName + "." + zone
	if !strings.HasSuffix(name, ".") {
		name += "."
	}
	return name
}

func (p *Route53Provider) upsert(ctx context.Context, zone, name, recordType string, values []string, ttl int) error {
	records := make([]types.ResourceRecord, len(values))
	for i, v := range values {
		records[i] = types.ResourceRecord{Value: aws.String(v)}
	}

	_, err := p.client.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(p.zoneID),
		ChangeBatch: &types.ChangeBatch{
			Changes: []types.Change{{
				Action: types.ChangeActionUpsert,
				ResourceRecordSet: &types.ResourceRecordSet{
					Name:            aws.String(fqdn(name, zone)),
					Type:            types.RRType(recordType),
					TTL:             aws.Int64(int64(ttl)),
					ResourceRecords: records,
				},
			}},
		},
	})
	return err
}

func (p *Route53Provider) getRecords(ctx context.Context, zone, name, recordType string) ([]string, int64, error) {
	f := fqdn(name, zone)
	resp, err := p.client.ListResourceRecordSets(ctx, &route53.ListResourceRecordSetsInput{
		HostedZoneId:    aws.String(p.zoneID),
		StartRecordName: aws.String(f),
		StartRecordType: types.RRType(recordType),
		MaxItems:        aws.Int32(1),
	})
	if err != nil {
		return nil, 0, err
	}
	for _, rs := range resp.ResourceRecordSets {
		if strings.TrimSuffix(aws.ToString(rs.Name), ".") == strings.TrimSuffix(f, ".") &&
			string(rs.Type) == recordType {
			var vals []string
			for _, r := range rs.ResourceRecords {
				vals = append(vals, aws.ToString(r.Value))
			}
			return vals, aws.ToInt64(rs.TTL), nil
		}
	}
	return nil, 0, nil
}

// UpdateRecord는 단일 값 DNS 레코드를 Route53에 업서트한다.
func (p *Route53Provider) UpdateRecord(ctx context.Context, zone, name, recordType, value string, ttl int) error {
	if err := p.upsert(ctx, zone, name, recordType, []string{value}, ttl); err != nil {
		return fmt.Errorf("route53 update record: %w", err)
	}
	slog.Info("route53 record updated", "record", name+"."+zone, "value", value)
	return nil
}

// UpdateRecordValues는 다중 값 DNS 레코드의 모든 값을 Route53에서 교체한다.
// values가 비어 있으면 기존 레코드를 유지한다.
func (p *Route53Provider) UpdateRecordValues(ctx context.Context, zone, name, recordType string, values []string, ttl int) error {
	if len(values) == 0 {
		slog.Warn("empty values, keeping record", "record", name+"."+zone)
		return nil
	}
	if err := p.upsert(ctx, zone, name, recordType, values, ttl); err != nil {
		return fmt.Errorf("route53 update record values: %w", err)
	}
	slog.Info("route53 multi-value replaced", "record", name+"."+zone, "values", values)
	return nil
}

// AddRecordValue는 Route53의 다중 값 DNS 레코드에 값을 추가한다.
// 이미 존재하는 값이면 무시한다.
func (p *Route53Provider) AddRecordValue(ctx context.Context, zone, name, recordType, value string, ttl int) error {
	existing, currentTTL, err := p.getRecords(ctx, zone, name, recordType)
	if err != nil {
		return fmt.Errorf("route53 get records: %w", err)
	}
	for _, v := range existing {
		if v == value {
			slog.Info("route53 value already exists", "record", name+"."+zone, "value", value)
			return nil
		}
	}
	if currentTTL > 0 {
		ttl = int(currentTTL)
	}
	existing = append(existing, value)
	return p.upsert(ctx, zone, name, recordType, existing, ttl)
}

// RemoveRecordValue는 Route53의 다중 값 DNS 레코드에서 특정 값을 제거한다.
// 마지막 남은 값이라면 오류를 반환한다.
func (p *Route53Provider) RemoveRecordValue(ctx context.Context, zone, name, recordType, value string) error {
	existing, currentTTL, err := p.getRecords(ctx, zone, name, recordType)
	if err != nil {
		return fmt.Errorf("route53 get records: %w", err)
	}
	var found bool
	var newVals []string
	for _, v := range existing {
		if v == value {
			found = true
		} else {
			newVals = append(newVals, v)
		}
	}
	if !found {
		return nil
	}
	if len(newVals) == 0 {
		slog.Warn("cannot remove last record value", "record", name+"."+zone)
		return fmt.Errorf("cannot remove last record value")
	}
	ttl := int(currentTTL)
	if ttl == 0 {
		ttl = 3
	}
	return p.upsert(ctx, zone, name, recordType, newVals, ttl)
}

// DeleteRecord는 Route53에서 DNS 레코드 전체를 삭제한다.
// 레코드가 존재하지 않으면 오류 없이 반환한다.
func (p *Route53Provider) DeleteRecord(ctx context.Context, zone, name, recordType string) error {
	existing, ttl, err := p.getRecords(ctx, zone, name, recordType)
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		return nil
	}
	records := make([]types.ResourceRecord, len(existing))
	for i, v := range existing {
		records[i] = types.ResourceRecord{Value: aws.String(v)}
	}
	_, err = p.client.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(p.zoneID),
		ChangeBatch: &types.ChangeBatch{
			Changes: []types.Change{{
				Action: types.ChangeActionDelete,
				ResourceRecordSet: &types.ResourceRecordSet{
					Name:            aws.String(fqdn(name, zone)),
					Type:            types.RRType(recordType),
					TTL:             aws.Int64(ttl),
					ResourceRecords: records,
				},
			}},
		},
	})
	return err
}

// VerifyRecord는 Route53에서 A 레코드가 기대하는 값을 가지고 있는지 확인한다.
func (p *Route53Provider) VerifyRecord(ctx context.Context, zone, name, expectedValue string) (bool, error) {
	vals, _, err := p.getRecords(ctx, zone, name, "A")
	if err != nil {
		return false, err
	}
	for _, v := range vals {
		if v == expectedValue {
			return true, nil
		}
	}
	return false, nil
}

// HealthCheck는 Route53의 호스팅 존 조회를 통해 연결 상태를 확인한다.
func (p *Route53Provider) HealthCheck(ctx context.Context) error {
	_, err := p.client.GetHostedZone(ctx, &route53.GetHostedZoneInput{
		Id: aws.String(p.zoneID),
	})
	return err
}
