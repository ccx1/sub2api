package repository

import (
	"context"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

func attachCreatedProxyGroup(ctx context.Context, created *dbent.Proxy) {
	if created.GroupID == nil {
		return
	}
	group, err := created.QueryGroup().Only(ctx)
	switch {
	case err == nil:
		created.Edges.Group, created.GroupID = group, &group.ID
	case dbent.IsNotFound(err):
		created.GroupID = nil
	default:
		// 创建已提交，展示用元数据查询失败不能把成功写入误报为失败。
		logger.LegacyPrintf("repository.proxy", "load created proxy group metadata failed: proxy_id=%d err=%v", created.ID, err)
	}
}
