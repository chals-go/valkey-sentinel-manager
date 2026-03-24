package dns

import (
	"context"
	"fmt"
)

// NewProvider는 프로바이더 이름과 설정 맵으로부터 DNS 프로바이더를 생성한다.
// 지원하는 name 값은 "route53", "azure", "bind"이며, 그 외에는 오류를 반환한다.
func NewProvider(ctx context.Context, name string, cfg map[string]string) (Provider, error) {
	switch name {
	case "route53":
		return NewRoute53Provider(ctx,
			cfg["zone_id"],
			cfg["region"],
			cfg["access_key"],
			cfg["secret_key"],
		)
	case "azure":
		return NewAzureProvider(
			cfg["subscription_id"],
			cfg["resource_group"],
			cfg["zone_name"],
			cfg["auth_type"],
			cfg["client_id"],
			cfg["client_secret"],
			cfg["tenant_id"],
		)
	case "bind":
		return NewBINDProvider(cfg["api_url"], cfg["api_key"]), nil
	default:
		return nil, fmt.Errorf("unknown DNS provider: %s", name)
	}
}
