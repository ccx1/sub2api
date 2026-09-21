//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type sharedCatalogGroupRepo struct {
	service.GroupRepository
	groups     []service.Group
	capacities map[int64]service.SharedPoolCapacity
}

func (r *sharedCatalogGroupRepo) ListActive(context.Context) ([]service.Group, error) {
	return r.groups, nil
}
func (r *sharedCatalogGroupRepo) SharedPoolAvailableCapacities(context.Context, []int64) (map[int64]service.SharedPoolCapacity, error) {
	return r.capacities, nil
}

type sharedCatalogUserRepo struct{ service.UserRepository }

func (*sharedCatalogUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	return &service.User{ID: 7}, nil
}

type sharedCatalogSubscriptionRepo struct {
	service.UserSubscriptionRepository
}

func (*sharedCatalogSubscriptionRepo) ListActiveByUserID(context.Context, int64) ([]service.UserSubscription, error) {
	return nil, nil
}

func sharedCatalogSnapshot(t *testing.T, expires time.Time) service.SharedPoolTicketAccountSnapshot {
	t.Helper()
	account := &service.Account{ID: 999, Status: service.StatusActive, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "must-not-leak-token"}, Extra: map[string]any{}}
	if !expires.IsZero() {
		account.Extra["codex_turn_ticket:custom"] = map[string]any{"state": "gAAAAA" + strings.Repeat("x", 286), "length": 292, "expires_at": expires}
	}
	snapshot := service.NewSharedPoolTicketAccountSnapshot(account, time.Now())
	require.NotNil(t, snapshot)
	snapshot.Available, snapshot.Concurrency = true, 3
	return *snapshot
}

func TestSharedPoolCatalogReturnsTicketComparisonUsingLiveSettings(t *testing.T) {
	now := time.Now()
	repo := &sharedCatalogGroupRepo{groups: []service.Group{
		{ID: 10, Name: "pool", IsSharedPool: true, Status: service.StatusActive, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard},
		{ID: 11, Name: "unavailable", IsSharedPool: true, Status: service.StatusActive, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard},
	}, capacities: map[int64]service.SharedPoolCapacity{
		10: {TotalAccounts: 10, AvailableAccounts: 7, ConcurrencyCapacity: 21, TicketAccounts: []service.SharedPoolTicketAccountSnapshot{
			sharedCatalogSnapshot(t, time.Time{}), sharedCatalogSnapshot(t, time.Time{}),
			sharedCatalogSnapshot(t, now.Add(time.Hour)), sharedCatalogSnapshot(t, now.Add(5*time.Minute)),
		}}, 11: {TotalAccounts: 2, AvailableAccounts: 0},
	}}
	cfg := &config.Config{}
	cfg.Gateway.OpenAICodexTicket = config.OpenAICodexTicketConfig{Enabled: true, Models: []string{"custom"}, FailClosed: true}
	settings := &sharedTicketSettingsStub{}
	h := &SharedPoolHandler{keys: service.NewAPIKeyService(nil, &sharedCatalogUserRepo{}, repo, &sharedCatalogSubscriptionRepo{}, nil, nil, cfg), ticketConfig: cfg, ticketSettings: settings}
	for _, enabled := range []bool{true, false} {
		settings.enabled = enabled
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/shared-pool/pools", nil)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		h.Pools(c)
		require.Equal(t, 200, w.Code)
		data := gjson.GetBytes(w.Body.Bytes(), "data")
		require.Len(t, data.Array(), 1, "全不可用池仍不公开")
		require.Equal(t, int64(10), data.Get("0.total_accounts").Int())
		if enabled {
			require.Equal(t, int64(5), data.Get("0.available_accounts").Int())
			require.Equal(t, int64(15), data.Get("0.concurrency_capacity").Int())
		} else {
			require.Equal(t, int64(7), data.Get("0.available_accounts").Int())
			require.Equal(t, int64(21), data.Get("0.concurrency_capacity").Int())
		}
		for _, forbidden := range []string{"gAAAAA", "must-not-leak-token", "credentials", "TicketAccounts", "999", "expires_at"} {
			require.NotContains(t, w.Body.String(), forbidden)
		}
	}
	require.Equal(t, 2, settings.calls, "每次请求只读取一次后台配置")
}

func TestSharedPoolCatalogKeepsUnknownCapacityAndOmitsUnrelatedTickets(t *testing.T) {
	view := newSharedPoolCatalogView(service.Group{Platform: service.PlatformOpenAI, ActiveAccountCount: 3}, config.OpenAICodexTicketConfig{Enabled: true}, time.Now())
	encoded, err := json.Marshal(view)
	require.NoError(t, err)
	require.Nil(t, view.TotalAccounts)
	require.Nil(t, view.ConcurrencyCapacity)
	require.Nil(t, view.CurrentConcurrency)
	require.NotContains(t, string(encoded), "ticket_progress")
	require.Contains(t, string(encoded), `"total_accounts":null`)
	require.Contains(t, string(encoded), `"current_concurrency":null`)
	view = newSharedPoolCatalogView(service.Group{Platform: service.PlatformGemini, SharedPoolCapacity: &service.SharedPoolCapacity{TotalAccounts: 2}}, config.OpenAICodexTicketConfig{Enabled: true}, time.Now())
	require.NotNil(t, view.TotalAccounts)
	require.EqualValues(t, 2, *view.TotalAccounts)
}

type sharedCatalogConcurrencyCache struct {
	service.ConcurrencyCache
	err error
}

func (c *sharedCatalogConcurrencyCache) GetAccountConcurrencyBatch(_ context.Context, ids []int64) (map[int64]int, error) {
	return map[int64]int{999: 8}, c.err
}

func TestSharedPoolCatalogCurrentConcurrencyDoesNotLeakAccountIDs(t *testing.T) {
	repo := &sharedCatalogGroupRepo{groups: []service.Group{{ID: 10, Name: "pool", IsSharedPool: true,
		Status: service.StatusActive, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard}},
		capacities: map[int64]service.SharedPoolCapacity{10: {TotalAccounts: 1, AvailableAccounts: 1,
			ConcurrencyCapacity: 3, AvailableAccountIDs: []int64{999}, UntrackedConcurrencyAccountIDs: []int64{998}}}}
	keys := service.NewAPIKeyService(nil, &sharedCatalogUserRepo{}, repo, &sharedCatalogSubscriptionRepo{}, nil, nil, &config.Config{})
	cache := &sharedCatalogConcurrencyCache{}
	keys.SetConcurrencyService(service.NewConcurrencyService(cache))
	h := &SharedPoolHandler{keys: keys}
	for _, failed := range []bool{false, true} {
		if failed {
			cache.err = errors.New("unavailable")
		}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/shared-pool/pools", nil)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		h.Pools(c)
		require.Equal(t, 200, w.Code)
		value := gjson.GetBytes(w.Body.Bytes(), "data.0.current_concurrency")
		if failed {
			require.Equal(t, "null", value.Raw)
		} else {
			require.EqualValues(t, 8, value.Int())
		}
		for _, forbidden := range []string{"999", "998", "AvailableAccountIDs", "UntrackedConcurrency", "account_ids"} {
			require.NotContains(t, w.Body.String(), forbidden)
		}
	}
}
