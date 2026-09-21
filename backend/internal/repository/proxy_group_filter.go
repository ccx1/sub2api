package repository

import (
	"context"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/proxy"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func filterProxyGroup(ctx context.Context, query *dbent.ProxyQuery) *dbent.ProxyQuery {
	id, present := service.ProxyGroupFilterFromContext(ctx)
	if !present {
		return query
	}
	if id == 0 {
		return query.Where(proxy.GroupIDIsNil())
	}
	return query.Where(proxy.GroupIDEQ(id))
}
