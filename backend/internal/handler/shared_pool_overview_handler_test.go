//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type sharedOverviewHandlerRepo struct {
	service.SharedPoolRepository
	err error
}

func (*sharedOverviewHandlerRepo) SharedSettings(context.Context) (*service.SharedPoolSettings, error) {
	return &service.SharedPoolSettings{SettlementMultiplier: 1, PlatformRateBPS: 500, ProxyRateBPS: 100}, nil
}

func (*sharedOverviewHandlerRepo) SharedUserRates(context.Context) ([]service.SharedPoolUserRate, error) {
	mine, other := 0.5, 9.0
	return []service.SharedPoolUserRate{{UserID: 7, SettlementMultiplier: &mine}, {UserID: 99, SettlementMultiplier: &other}}, nil
}

func (r *sharedOverviewHandlerRepo) SharedPoolOverviewAccounts(context.Context) ([]service.SharedPoolOverviewAccount, error) {
	return []service.SharedPoolOverviewAccount{
		{AccountID: 42, Platform: "openai", Tier: "pro", Valid: true, Available: true, TicketRequired: true, Concurrency: 3},
		{AccountID: 43, Platform: "openai", Tier: "pro", Valid: true, TicketRequired: true, Concurrency: 3},
		{AccountID: 44, Platform: "openai", Tier: "pro", TicketRequired: true, Concurrency: 3},
	}, r.err
}

func TestSharedPoolOverviewUsesAuthenticatedUserAndOnlyExposesAggregateFields(t *testing.T) {
	repo := &sharedOverviewHandlerRepo{}
	h := &SharedPoolHandler{pool: service.NewSharedPoolService(repo, nil, nil, nil, nil, nil, nil)}
	c, w := sharedTestContext(7, "")
	c.Request = httptest.NewRequest("GET", "/shared-pool/overview?user_id=99", nil)
	h.Overview(c)
	require.Equal(t, 200, w.Code)
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	data := envelope.Data
	require.Equal(t, 0.5, data["settlement_multiplier"])
	require.Equal(t, float64(3), data["total_accounts"])
	require.Equal(t, float64(2), data["available_accounts"])
	require.Equal(t, float64(1), data["schedulable_accounts"])
	require.Nil(t, data["current_concurrency"], "无并发统计依赖时不能伪造零占用")
	require.Len(t, data, 14, "公开字段白名单不包含内部容量和身份")
	require.Equal(t, float64(0), data["participating_accounts"])
	require.Equal(t, float64(0), data["participating_concurrency"])
	tier := data["tiers"].([]any)[0].(map[string]any)
	require.Len(t, tier, 12)
	require.Equal(t, false, tier["available"])
	require.Equal(t, "pro", tier["tier"])
	for _, field := range []string{"account_id", "owner_user_id", "credentials", "group_ids", "capacity", "email"} {
		require.NotContains(t, w.Body.String(), `"`+field+`"`)
	}
}

func TestSharedPoolOverviewDoesNotMaskStatisticsFailureAsEmptyPool(t *testing.T) {
	repo := &sharedOverviewHandlerRepo{err: errors.New("statistics unavailable")}
	h := &SharedPoolHandler{pool: service.NewSharedPoolService(repo, nil, nil, nil, nil, nil, nil)}
	c, w := sharedTestContext(7, "")
	h.Overview(c)
	require.Equal(t, 500, w.Code)
	require.NotContains(t, w.Body.String(), `"total_accounts":0`)
}
