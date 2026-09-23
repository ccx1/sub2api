package service

import (
	"context"
	"maps"
)

type accountProxyRegionWriteKey struct{}
type accountProxyRegionWriteFields struct{ mode, country bool }

func WithAccountProxyRegionWrite(ctx context.Context, requested map[string]any) context.Context {
	_, mode := requested[ProxyRegionModeExtraKey]
	_, country := requested[ProxyRegionCountryExtraKey]
	return context.WithValue(ctx, accountProxyRegionWriteKey{}, accountProxyRegionWriteFields{mode, country})
}

func AccountProxyRegionWriteFields(ctx context.Context) (bool, bool) {
	fields, _ := ctx.Value(accountProxyRegionWriteKey{}).(accountProxyRegionWriteFields)
	return fields.mode, fields.country
}

// 后台整对象刷新不得覆盖管理员在读取快照之后修改的地区设置。
func PreserveAccountProxyRegion(ctx context.Context, current, updated map[string]any) map[string]any {
	result := maps.Clone(updated)
	mode, country := AccountProxyRegionWriteFields(ctx)
	for key, supplied := range map[string]bool{ProxyRegionModeExtraKey: mode, ProxyRegionCountryExtraKey: country} {
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
