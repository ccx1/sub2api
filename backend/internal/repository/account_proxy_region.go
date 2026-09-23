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
	if len(keys) == 0 {
		return expression
	}
	array := "ARRAY[" + strings.Join(keys, ",") + "]::text[]"
	return "((" + expression + ") - " + array + ") || COALESCE((SELECT jsonb_object_agg(key,value) FROM jsonb_each(COALESCE(extra,'{}'::jsonb)) WHERE key = ANY(" + array + ")), '{}'::jsonb)"
}
