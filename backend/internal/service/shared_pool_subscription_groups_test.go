//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type sharedTierGroups struct {
	GroupRepository
	items map[int64]*Group
	err   error
}

func (r sharedTierGroups) GetByID(_ context.Context, id int64) (*Group, error) {
	if r.err != nil {
		return nil, r.err
	}
	if g := r.items[id]; g != nil {
		return g, nil
	}
	return nil, ErrGroupNotFound
}

func sharedTierGroup(id int64, platform string) *Group {
	return &Group{ID: id, Platform: platform, IsSharedPool: true, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard}
}

func TestSharedSubscriptionTierRecognition(t *testing.T) {
	for _, tc := range []struct{ platform, key, raw, want string }{
		{PlatformOpenAI, "plan_type", " ChatGPT_Pro ", "pro"},
		{PlatformOpenAI, "plan_type", "Pro-Lite", "prolite"},
		{PlatformOpenAI, "plan_type", "self_serve_business_prolite", "self_serve_business_prolite"},
		{PlatformOpenAI, "plan_type", "team", "team"},
		{PlatformOpenAI, "plan_type", "business", "business"},
		{PlatformOpenAI, "plan_type", "abnormal", ""},
		{PlatformOpenAI, "plan_type", "", ""},
		{PlatformGemini, "tier_id", "STANDARD", "gcp_standard"},
		{PlatformGemini, "tier_id", "google_ai_ultra", "google_ai_ultra"},
		{PlatformGemini, "tier_id", "google_one_unknown", ""},
		{PlatformAntigravity, "plan_type", "g1-pro-tier", "pro"},
		{PlatformAntigravity, "plan_type", "g1-ultra-tier", "ultra"},
		{PlatformAntigravity, "plan_type", "free-tier", "free"},
		{PlatformAntigravity, "plan_type", "", ""},
		{PlatformAnthropic, "plan_type", "pro", ""},
	} {
		t.Run(tc.platform+"/"+tc.raw, func(t *testing.T) {
			a := &Account{Platform: tc.platform, Type: AccountTypeOAuth, Credentials: map[string]any{tc.key: tc.raw}}
			require.Equal(t, tc.want, sharedAccountSubscriptionTier(a))
			a.Type = AccountTypeAPIKey
			require.Empty(t, sharedAccountSubscriptionTier(a))
		})
	}
	a := &Account{Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: map[string]any{"oauth_type": "code_assist", "tier_id": "google_ai_pro"}}
	require.Empty(t, sharedAccountSubscriptionTier(a), "不能跨 Gemini OAuth 通道匹配")
	a = &Account{Platform: PlatformAntigravity, Type: AccountTypeOAuth, Extra: map[string]any{"load_code_assist": map[string]any{
		"paidTier": map[string]any{"id": "g1-ultra-tier"}, "currentTier": map[string]any{"id": "free-tier"},
	}}}
	require.Equal(t, "ultra", sharedAccountSubscriptionTier(a))
}

func TestSharedSubscriptionGroupFallback(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Group)
		absent bool
	}{
		{name: "matched"},
		{name: "deleted", absent: true},
		{name: "inactive", change: func(g *Group) { g.Status = "disabled" }},
		{name: "wrong platform", change: func(g *Group) { g.Platform = PlatformAntigravity }},
		{name: "not shared", change: func(g *Group) { g.IsSharedPool = false }},
		{name: "exclusive", change: func(g *Group) { g.IsExclusive = true }},
		{name: "subscription group", change: func(g *Group) { g.SubscriptionType = SubscriptionTypeSubscription }},
		{name: "invalid billing type", change: func(g *Group) { g.SubscriptionType = "unsupported" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := sharedTierGroup(11, PlatformOpenAI)
			if tc.change != nil {
				tc.change(target)
			}
			groups := sharedTierGroups{items: map[int64]*Group{10: sharedTierGroup(10, PlatformOpenAI)}}
			if !tc.absent {
				groups.items[11] = target
			}
			s := &SharedPoolService{groups: groups}
			cfg := &SharedPoolSettings{DefaultGroupIDs: map[string]int64{PlatformOpenAI: 10}, SubscriptionGroupIDs: map[string]map[string]int64{PlatformOpenAI: {"plus": 11}}}
			a := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"plan_type": "plus"}}
			id, err := s.initialSharedGroup(context.Background(), cfg, a)
			require.NoError(t, err)
			want := int64(10)
			if tc.name == "matched" {
				want = 11
			}
			require.Equal(t, want, id)
		})
	}
}

func TestSharedSubscriptionDefaultsAndErrors(t *testing.T) {
	groups := sharedTierGroups{items: map[int64]*Group{10: sharedTierGroup(10, PlatformOpenAI), 11: sharedTierGroup(11, PlatformOpenAI)}}
	s := &SharedPoolService{groups: groups}
	cfg := &SharedPoolSettings{DefaultGroupIDs: map[string]int64{PlatformOpenAI: 10}, SubscriptionGroupIDs: map[string]map[string]int64{PlatformAntigravity: {"pro": 11}}}
	a := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"plan_type": "pro"}}
	id, err := s.initialSharedGroup(context.Background(), cfg, a)
	require.NoError(t, err)
	require.Equal(t, int64(10), id, "不能命中其它平台同名档位")
	cfg.SubscriptionGroupIDs[PlatformOpenAI] = map[string]int64{"pro": 11}
	a.Credentials["plan_type"] = "unknown"
	id, err = s.initialSharedGroup(context.Background(), cfg, a)
	require.NoError(t, err)
	require.Equal(t, int64(10), id)
	a.Credentials["plan_type"] = "pro"
	a.Type = AccountTypeAPIKey
	id, err = s.initialSharedGroup(context.Background(), cfg, a)
	require.NoError(t, err)
	require.Equal(t, int64(10), id, "API Key 不匹配订阅规则")
	groups.items[10].RequireOAuthOnly = true
	_, err = s.initialSharedGroup(context.Background(), cfg, a)
	require.Error(t, err)
	delete(groups.items, 10)
	_, err = s.initialSharedGroup(context.Background(), cfg, a)
	require.Error(t, err)
	dbErr := errors.New("database unavailable")
	s.groups = sharedTierGroups{err: dbErr}
	a.Type = AccountTypeOAuth
	_, err = s.initialSharedGroup(context.Background(), cfg, a)
	require.ErrorIs(t, err, dbErr)
}

type sharedTierSettingsRepo struct {
	sharedPoolRepoStub
	cfg     *SharedPoolSettings
	created *Account
	saved   bool
}

func (r *sharedTierSettingsRepo) SharedSettings(context.Context) (*SharedPoolSettings, error) {
	return r.cfg, nil
}
func (r *sharedTierSettingsRepo) SaveSharedSettings(_ context.Context, cfg *SharedPoolSettings) error {
	r.saved, r.cfg = true, cfg
	return nil
}
func (r *sharedTierSettingsRepo) CreateSharedAccount(_ context.Context, a *Account, ownerID int64, _ string) error {
	a.ID = 1
	r.created = a
	r.record = SharedPoolAccountRecord{AccountID: 1, OwnerUserID: ownerID}
	return nil
}

type sharedTierCreatedAccounts struct {
	AccountRepository
	repo *sharedTierSettingsRepo
}

func (r sharedTierCreatedAccounts) GetByID(context.Context, int64) (*Account, error) {
	return r.repo.created, nil
}

func TestSharedSubscriptionAssignmentOnCreateAndFirstEnable(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{true: "create enabled", false: "first enable"}[enabled], func(t *testing.T) {
			r := &sharedTierSettingsRepo{cfg: &SharedPoolSettings{MaxConcurrency: 10, DefaultPriority: 17,
				SettlementMultiplier: 1,
				DefaultGroupIDs:      map[string]int64{PlatformGemini: 10}, SubscriptionGroupIDs: map[string]map[string]int64{PlatformGemini: {"gcp_enterprise": 11}}}}
			s := &SharedPoolService{repo: r, accounts: sharedTierCreatedAccounts{repo: r}, earnings: sharedPoolTotalsStub{}, groups: sharedTierGroups{items: map[int64]*Group{
				10: sharedTierGroup(10, PlatformGemini), 11: sharedTierGroup(11, PlatformGemini),
			}}}
			_, err := s.Create(context.Background(), 7, SharedPoolAccountInput{Name: "shared", Platform: PlatformGemini, Type: AccountTypeOAuth,
				Concurrency: 1, Enabled: enabled, DispatchConsent: enabled, Credentials: map[string]any{"access_token": "test", "tier_id": "ENTERPRISE", "oauth_type": "code_assist"}})
			require.NoError(t, err)
			require.Equal(t, 17, r.created.Priority, "新共享账号使用管理员配置的默认优先级")
			if enabled {
				require.Equal(t, []int64{11}, r.created.GroupIDs)
			} else {
				require.Empty(t, r.created.GroupIDs)
				require.NoError(t, s.SetEnabled(context.Background(), 7, 1, true, new(true)))
				require.Equal(t, []int64{11}, *r.stateGroups)
			}
		})
	}
}

func TestSharedSubscriptionSettingsValidation(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		rules                map[string]map[string]int64
		noDefault, wantError bool
	}{
		{name: "canonical alias", rules: map[string]map[string]int64{PlatformOpenAI: {"ChatGPT_Pro": 11}}},
		{name: "duplicate alias", rules: map[string]map[string]int64{PlatformOpenAI: {"pro": 11, "chatgpt_pro": 11}}, wantError: true},
		{name: "unknown tier", rules: map[string]map[string]int64{PlatformOpenAI: {"unknown": 11}}, wantError: true},
		{name: "unknown target", rules: map[string]map[string]int64{PlatformOpenAI: {"plus": 99}}, wantError: true},
		{name: "missing default", rules: map[string]map[string]int64{PlatformOpenAI: {"plus": 11}}, noDefault: true, wantError: true},
		{name: "legacy omitted"},
		{name: "clear rules", rules: map[string]map[string]int64{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &sharedTierSettingsRepo{}
			s := &SharedPoolService{repo: r, groups: sharedTierGroups{items: map[int64]*Group{10: sharedTierGroup(10, PlatformOpenAI), 11: sharedTierGroup(11, PlatformOpenAI)}}}
			cfg := &SharedPoolSettings{MaxConcurrency: 5, DefaultGroupIDs: map[string]int64{PlatformOpenAI: 10}, SubscriptionGroupIDs: tc.rules}
			if tc.noDefault {
				cfg.DefaultGroupIDs = nil
			}
			err := s.SaveSettings(context.Background(), cfg)
			if tc.wantError {
				require.Error(t, err)
				require.False(t, r.saved)
				return
			}
			require.NoError(t, err)
			require.True(t, r.saved)
			if tc.name == "canonical alias" {
				require.Equal(t, int64(11), r.cfg.SubscriptionGroupIDs[PlatformOpenAI]["pro"])
			}
			if tc.name == "legacy omitted" {
				require.Nil(t, r.cfg.SubscriptionGroupIDs)
			}
		})
	}
}
