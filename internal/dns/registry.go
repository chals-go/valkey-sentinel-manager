package dns

import (
	"context"
	"fmt"
)

// ProviderFactory는 설정 맵으로부터 DNS Provider를 생성하는 함수 타입이다.
type ProviderFactory func(ctx context.Context, cfg map[string]string) (Provider, error)

// ProviderInfo는 사용 가능한 DNS Provider의 메타 정보이다.
type ProviderInfo struct {
	Type        string // "route53", "azure", "restapi", "cloudflare"
	DisplayName string // "Route53", "Azure DNS", "REST API", "Cloudflare"
}

var (
	factories    = map[string]ProviderFactory{}
	providerList []ProviderInfo
)

// Register는 DNS Provider를 레지스트리에 등록한다. 각 Provider 파일의 init()에서 호출된다.
func Register(name, displayName string, factory ProviderFactory) {
	factories[name] = factory
	providerList = append(providerList, ProviderInfo{Type: name, DisplayName: displayName})
}

// AvailableProviders는 빌드에 포함된 DNS Provider 목록을 반환한다.
func AvailableProviders() []ProviderInfo {
	return providerList
}

// IsProviderAvailable는 지정된 Provider가 빌드에 포함되어 있는지 확인한다.
func IsProviderAvailable(name string) bool {
	_, ok := factories[name]
	return ok
}

// NewProvider는 레지스트리에서 팩토리를 찾아 DNS Provider를 생성한다.
func NewProvider(ctx context.Context, name string, cfg map[string]string) (Provider, error) {
	factory, ok := factories[name]
	if !ok {
		return nil, fmt.Errorf("unknown or excluded DNS provider: %s", name)
	}
	return factory(ctx, cfg)
}
