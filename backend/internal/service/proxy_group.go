package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrProxyGroupNotFound  = infraerrors.NotFound("PROXY_GROUP_NOT_FOUND", "proxy group not found")
	ErrProxyGroupDuplicate = infraerrors.Conflict("PROXY_GROUP_DUPLICATE", "proxy group name already exists")
	ErrProxyGroupInUse     = infraerrors.Conflict("PROXY_GROUP_IN_USE", "proxy group is referenced by accounts")
)

type ProxyGroup struct {
	ID               int64     `json:"id"`
	Name             string    `json:"name"`
	ProxyCount       int64     `json:"proxy_count"`
	ActiveProxyCount int64     `json:"active_proxy_count"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type ProxyGroupAdminService interface {
	ListProxyGroups(context.Context) ([]ProxyGroup, error)
	CreateProxyGroup(context.Context, string) (*ProxyGroup, error)
	UpdateProxyGroup(context.Context, int64, string) (*ProxyGroup, error)
	DeleteProxyGroup(context.Context, int64) error
	AssignProxyGroup(context.Context, []int64, *int64) (int64, error)
}

type ProxyGroupRepository interface {
	ProxyGroupAdminService
	ProxyGroupExists(context.Context, int64) (bool, error)
}

type proxyGroupFilterKey struct{}

// WithProxyGroupFilter 的 0 表示未分组，缺省上下文表示所有代理。
func WithProxyGroupFilter(ctx context.Context, groupID int64) context.Context {
	return context.WithValue(ctx, proxyGroupFilterKey{}, groupID)
}

func ProxyGroupFilterFromContext(ctx context.Context) (int64, bool) {
	value, ok := ctx.Value(proxyGroupFilterKey{}).(int64)
	return value, ok
}
