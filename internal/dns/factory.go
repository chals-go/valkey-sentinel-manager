package dns

import (
	"context"
	"fmt"
)

// NewProvider creates a DNS provider from a name and configuration map.
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
