//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedAdminSettingsDefaultGroupsAcceptLegacyAndMultiple(t *testing.T) {
	for _, tc := range []struct {
		body string
		want []int64
	}{
		{`{"default_group_ids":{"openai":4}}`, []int64{4}},
		{`{"default_group_ids":{"openai":[4,5,4]}}`, []int64{4, 5}},
		{`{"default_group_ids":{"openai":[]}}`, nil},
	} {
		repo := &sharedDispatchSettingsRepo{current: service.SharedPoolSettings{MaxConcurrency: 10, SettlementMultiplier: 1}}
		h := &SharedPoolHandler{pool: service.NewSharedPoolService(repo, nil, sharedAdminSettingsGroups{}, nil, nil, nil, nil)}
		c, w := sharedTestContext(7, tc.body)
		h.AdminSaveSettings(c)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, tc.want, repo.saved.DefaultGroupIDs["openai"])
		var response struct {
			Data struct {
				DefaultGroupIDs map[string][]int64 `json:"default_group_ids"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.Equal(t, tc.want, response.Data.DefaultGroupIDs["openai"])
	}
}

type sharedDispatchSettingsRepo struct {
	service.SharedPoolRepository
	current service.SharedPoolSettings
	saved   *service.SharedPoolSettings
}

func (r *sharedDispatchSettingsRepo) SharedSettings(context.Context) (*service.SharedPoolSettings, error) {
	return &r.current, nil
}

func (r *sharedDispatchSettingsRepo) SaveSharedSettings(_ context.Context, settings *service.SharedPoolSettings) error {
	r.saved = settings
	return nil
}

func TestSharedDispatchSettingsLegacySavePreservesMultiplierAndExplicitZero(t *testing.T) {
	for _, tc := range []struct {
		body string
		want float64
	}{
		{`{"platform_rate_bps":500,"proxy_rate_bps":100,"max_concurrency":10,"default_group_ids":{},"subscription_group_ids":{}}`, 2.5},
		{`{"platform_rate_bps":500,"proxy_rate_bps":100,"max_concurrency":10,"default_group_ids":{},"subscription_group_ids":{},"settlement_multiplier":0}`, 0},
	} {
		repo := &sharedDispatchSettingsRepo{current: service.SharedPoolSettings{SettlementMultiplier: 2.5, DefaultGroupIDs: service.SharedPoolDefaultGroupIDs{"openai": {4}}}}
		h := &SharedPoolHandler{pool: service.NewSharedPoolService(repo, nil, nil, nil, nil, nil, nil)}
		c, w := sharedTestContext(7, tc.body)
		h.AdminSaveSettings(c)
		require.Equal(t, 200, w.Code)
		require.Equal(t, tc.want, repo.saved.SettlementMultiplier)
		require.Empty(t, repo.saved.DefaultGroupIDs)
	}
}

func TestSharedDispatchImportConsentComesOnlyFromForm(t *testing.T) {
	content := `{"name":"supply","platform":"gemini","type":"apikey","credentials":{"api_key":"test"},"dispatch_consent":true,"enabled":true,"settlement_multiplier":0,"extra":{"shared_pool_dispatch_consent":true,"shared_pool_settlement_multiplier":0}}`
	for _, consent := range []bool{false, true} {
		defaults := sharedImportDefaults{Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey, Concurrency: 1, DispatchConsent: consent}
		entries, err := parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Content: content}}, Defaults: defaults})
		require.NoError(t, err)
		require.Len(t, entries, 1)
		require.Equal(t, consent, entries[0].input.DispatchConsent)
		require.False(t, entries[0].input.Enabled)
	}
}

func TestSharedDispatchAdminAssignmentRejectsConsentAndAccountRate(t *testing.T) {
	h := &SharedPoolHandler{}
	for _, body := range []string{`{"group_ids":[1],"dispatch_consent":true}`, `{"group_ids":[1],"settlement_multiplier":1}`} {
		c, w := sharedTestContext(7, body)
		h.AdminAssign(c)
		require.Equal(t, 400, w.Code)
	}
}

type sharedAdminSettingsGroups struct{ service.GroupRepository }

func (sharedAdminSettingsGroups) GetByID(_ context.Context, id int64) (*service.Group, error) {
	return &service.Group{ID: id, Platform: service.PlatformOpenAI, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard}, nil
}

func TestSharedAdminSettingsPartialPreservesPriorityAndExplicitEmptyClearsMaps(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		priority   int
		clear      bool
	}{
		{"partial", `{"platform_rate_bps":600}`, 17, false},
		{"explicit zero and empty", `{"default_priority":0,"default_group_ids":{},"subscription_group_ids":{},"subscription_settlement_multipliers":{}}`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &sharedDispatchSettingsRepo{current: service.SharedPoolSettings{PlatformRateBPS: 500, ProxyRateBPS: 100, MaxConcurrency: 10,
				DefaultPriority: 17, SettlementMultiplier: 1, DefaultGroupIDs: service.SharedPoolDefaultGroupIDs{"openai": {4}},
				SubscriptionGroupIDs: service.SharedPoolSubscriptionGroupIDs{"openai": {"pro": {4}}}, SubscriptionSettlementMultipliers: map[string]map[string]float64{"openai": {"pro": 2}}}}
			h := &SharedPoolHandler{pool: service.NewSharedPoolService(repo, nil, sharedAdminSettingsGroups{}, nil, nil, nil, nil)}
			c, w := sharedTestContext(7, tc.body)
			h.AdminSaveSettings(c)
			require.Equal(t, 200, w.Code)
			require.Equal(t, tc.priority, repo.saved.DefaultPriority)
			if tc.clear {
				require.Empty(t, repo.saved.DefaultGroupIDs)
				require.Empty(t, repo.saved.SubscriptionGroupIDs)
				require.Empty(t, repo.saved.SubscriptionSettlementMultipliers)
			} else {
				require.Equal(t, repo.current.DefaultGroupIDs, repo.saved.DefaultGroupIDs)
				require.Equal(t, repo.current.SubscriptionGroupIDs, repo.saved.SubscriptionGroupIDs)
				require.Equal(t, repo.current.SubscriptionSettlementMultipliers, repo.saved.SubscriptionSettlementMultipliers)
			}
			require.Equal(t, []int64{4}, repo.current.DefaultGroupIDs["openai"], "decoding must not mutate the loaded settings")
		})
	}
}
