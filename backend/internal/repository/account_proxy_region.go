package repository

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func preserveAccountProxyRegionExtraSQL(ctx context.Context, expression string) string {
	mode, country := service.AccountProxyRegionWriteFields(ctx)
	keys := []string{}
	if !mode {
		keys = append(keys, "'proxy_region_mode'")
	}
	if !country {
		keys = append(keys, "'proxy_region_country'")
	}
	// 导入地区兜底是账号级配置；除非调用方显式写入，否则凭据刷新不能覆盖。
	if !service.AccountProxyRegionFallbackCountryWriteField(ctx) {
		keys = append(keys, "'proxy_region_fallback_country'")
	}
	if len(keys) == 0 {
		return expression
	}
	array := "ARRAY[" + strings.Join(keys, ",") + "]::text[]"
	return "((" + expression + ") - " + array + ") || COALESCE((SELECT jsonb_object_agg(key,value) FROM jsonb_each(COALESCE(extra,'{}'::jsonb)) WHERE key = ANY(" + array + ")), '{}'::jsonb)"
}
