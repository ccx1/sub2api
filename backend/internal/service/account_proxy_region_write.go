package service

import (
	"context"
	"maps"
)

type accountProxyRegionWriteKey struct{}
type accountProxyRegionWriteFields struct{ mode, country, fallbackCountry bool }

func WithAccountProxyRegionWrite(ctx context.Context, requested map[string]any) context.Context {
	_, mode := requested[ProxyRegionModeExtraKey]
	_, country := requested[ProxyRegionCountryExtraKey]
	_, fallbackCountry := requested[ProxyRegionFallbackCountryExtraKey]
	return context.WithValue(ctx, accountProxyRegionWriteKey{}, accountProxyRegionWriteFields{mode, country, fallbackCountry})
}

func AccountProxyRegionWriteFields(ctx context.Context) (bool, bool) {
	fields, _ := ctx.Value(accountProxyRegionWriteKey{}).(accountProxyRegionWriteFields)
	return fields.mode, fields.country
}

func AccountProxyRegionFallbackCountryWriteField(ctx context.Context) bool {
	fields, _ := ctx.Value(accountProxyRegionWriteKey{}).(accountProxyRegionWriteFields)
	return fields.fallbackCountry
}

// 后台整对象刷新不得覆盖管理员在读取快照之后修改的地区设置。
func PreserveAccountProxyRegion(ctx context.Context, current, updated map[string]any) map[string]any {
	result := maps.Clone(updated)
	fields, _ := ctx.Value(accountProxyRegionWriteKey{}).(accountProxyRegionWriteFields)
	for key, supplied := range map[string]bool{
		ProxyRegionModeExtraKey: fields.mode, ProxyRegionCountryExtraKey: fields.country,
		ProxyRegionFallbackCountryExtraKey: fields.fallbackCountry,
	} {
		if supplied {
			continue
		}
		delete(result, key)
		if value, exists := current[key]; exists {
			if result == nil {
				result = make(map[string]any)
			}
			result[key] = value
		}
	}
	return result
}
