package service

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateRandomProxyReuseDetectsExitAndConfigurationChanges(t *testing.T) {
	for _, change := range []string{"proxy", "address", "pool", "mode", "disabled", "unschedulable", "cooldown"} {
		t.Run(change, func(t *testing.T) {
			current := pluginDirectoryAccount(RandomProxyEmptyPoolPolicyReject)
			original := &Proxy{ID: 7, Status: StatusActive, Protocol: "http", Host: "first.example", Port: 8080}
			bound := *current
			bound.Extra = map[string]any{ProxyModeExtraKey: ProxyModeRandom}
			bound.ProxyID, bound.Proxy = &original.ID, original
			selected := *original
			switch change {
			case "proxy":
				selected.ID = 8
			case "address":
				selected.Host = "second.example"
			case "pool":
				current.Extra[RandomProxyPoolScopeExtraKey] = RandomProxyPoolSelected
				current.Extra[RandomProxyPoolIDsExtraKey] = []int64{8}
				selected.ID = 8
			case "mode":
				delete(current.Extra, ProxyModeExtraKey)
			case "disabled":
				current.Status = StatusDisabled
			case "unschedulable":
				current.Schedulable = false
			case "cooldown":
				now := time.Now().UTC()
				current.Extra[DailyCooldownExtraKey] = map[string]any{
					"enabled": true, "start": now.Add(-time.Hour).Format("15:04"),
					"end": now.Add(time.Hour).Format("15:04"), "timezone": "UTC",
				}
			}
			repo := &balancedAccountProxyStub{pluginDirectoryProxyRepo: pluginDirectoryProxyRepo{account: current, proxy: &selected}}
			require.ErrorIs(t, ValidateRandomProxyForReuse(context.Background(), &bound, repo), ErrRandomProxyChanged)
			require.Equal(t, original, bound.Proxy, "旧连接的账号不能偷偷改成新出口")
			require.Nil(t, current.ProxyID)
		})
	}
}

type shadowProxyReuseRepo struct {
	balancedAccountProxyStub
	parent *Account
}

func (r *shadowProxyReuseRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	if r.parent != nil && r.parent.ID == id {
		parent := *r.parent
		return &parent, nil
	}
	return r.pluginDirectoryProxyRepo.GetByID(ctx, id)
}

func TestValidateRandomProxyReuseShadowFollowsParentCooldownBoundaries(t *testing.T) {
	for _, mode := range []string{"fixed", "random"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				parent := &Account{ID: 100, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
					Extra: map[string]any{DailyCooldownExtraKey: map[string]any{
						"enabled": true, "start": "23:00", "end": "08:00", "timezone": "UTC",
					}}}
				current := pluginDirectoryAccount(RandomProxyEmptyPoolPolicyReject)
				current.ParentAccountID = &parent.ID
				if mode == "fixed" {
					delete(current.Extra, ProxyModeExtraKey)
				}
				proxy := &Proxy{ID: 7, Status: StatusActive}
				bound := *current
				bound.ProxyID, bound.Proxy = &proxy.ID, proxy
				repo := &shadowProxyReuseRepo{parent: parent, balancedAccountProxyStub: balancedAccountProxyStub{
					pluginDirectoryProxyRepo: pluginDirectoryProxyRepo{account: current, proxy: proxy},
				}}
				now := time.Now().UTC()
				start := time.Date(now.Year(), now.Month(), now.Day(), 23, 0, 0, 0, time.UTC)
				time.Sleep(start.Add(-time.Second).Sub(now))
				require.NoError(t, ValidateRandomProxyForReuse(context.Background(), &bound, repo))
				time.Sleep(time.Second)
				require.ErrorIs(t, ValidateRandomProxyForReuse(context.Background(), &bound, repo), ErrRandomProxyChanged)
				time.Sleep(9*time.Hour - time.Second)
				require.ErrorIs(t, ValidateRandomProxyForReuse(context.Background(), &bound, repo), ErrRandomProxyChanged)
				time.Sleep(time.Second)
				require.NoError(t, ValidateRandomProxyForReuse(context.Background(), &bound, repo))
				require.Equal(t, StatusActive, parent.Status)
				require.False(t, parent.Schedulable, "母账号自身停调不影响影子的独立配额")
				if mode == "random" {
					require.Len(t, repo.selections, 2, "冷却中禁止在复用校验内重新选路")
				} else {
					require.Empty(t, repo.selections)
				}
			})
		})
	}
}

func TestValidateRandomProxyReuseShadowRejectsUnusableParent(t *testing.T) {
	for _, issue := range []string{"missing", "disabled", "non_oauth", "transport_cooldown"} {
		t.Run(issue, func(t *testing.T) {
			parent := &Account{ID: 100, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive}
			current := pluginDirectoryAccount(RandomProxyEmptyPoolPolicyReject)
			current.ParentAccountID = &parent.ID
			delete(current.Extra, ProxyModeExtraKey)
			switch issue {
			case "missing":
				parent = nil
			case "disabled":
				parent.Status = StatusDisabled
			case "non_oauth":
				parent.Type = AccountTypeAPIKey
			case "transport_cooldown":
				until := time.Now().Add(time.Hour)
				parent.TempUnschedulableUntil = &until
			}
			repo := &shadowProxyReuseRepo{parent: parent, balancedAccountProxyStub: balancedAccountProxyStub{
				pluginDirectoryProxyRepo: pluginDirectoryProxyRepo{account: current},
			}}
			require.ErrorIs(t, ValidateRandomProxyForReuse(context.Background(), current, repo), ErrRandomProxyChanged)
		})
	}
}

func TestValidateRandomProxyReuseUsesCurrentPoolAndPeriod(t *testing.T) {
	current := pluginDirectoryAccount(RandomProxyEmptyPoolPolicyReject)
	current.Extra[RandomProxyPoolScopeExtraKey] = RandomProxyPoolSelected
	current.Extra[RandomProxyPoolIDsExtraKey] = []int64{7, 8}
	current.Extra[RandomProxyMaxReuseMinutesExtraKey] = 30
	proxy := &Proxy{ID: 7, Status: StatusActive}
	bound := *current
	bound.ProxyID, bound.Proxy = &proxy.ID, proxy
	repo := &balancedAccountProxyStub{pluginDirectoryProxyRepo: pluginDirectoryProxyRepo{account: current, proxy: proxy}}
	require.NoError(t, ValidateRandomProxyForReuse(context.Background(), &bound, repo))
	require.Equal(t, []ProxyPoolSelection{{AccountID: 42, IDs: []int64{7, 8}, Restricted: true, MaxReuseDuration: 30 * time.Minute}}, repo.selections)
	newProxy := *proxy
	newProxy.ID = 8
	repo.proxy = &newProxy
	require.ErrorIs(t, ValidateRandomProxyForReuse(context.Background(), &bound, repo), ErrRandomProxyChanged)
	require.Equal(t, []int64{7, 8}, repo.selections[1].IDs)
}

func TestValidateRandomProxyReuseEmptyPoolPolicies(t *testing.T) {
	for _, policy := range []string{RandomProxyEmptyPoolPolicyDirect, RandomProxyEmptyPoolPolicyReject, RandomProxyEmptyPoolPolicyDisable} {
		t.Run(policy, func(t *testing.T) {
			current := pluginDirectoryAccount(policy)
			repo := &balancedAccountProxyStub{pluginDirectoryProxyRepo: pluginDirectoryProxyRepo{account: current}}
			bound := *current
			err := ValidateRandomProxyForReuse(context.Background(), &bound, repo)
			if policy == RandomProxyEmptyPoolPolicyDirect {
				require.NoError(t, err)
				oldProxy := &Proxy{ID: 7, Status: StatusActive}
				bound.ProxyID, bound.Proxy = &oldProxy.ID, oldProxy
				require.ErrorIs(t, ValidateRandomProxyForReuse(context.Background(), &bound, repo), ErrRandomProxyChanged)
				return
			}
			require.ErrorIs(t, err, ErrRandomProxyUnavailable)
			if policy == RandomProxyEmptyPoolPolicyDisable {
				require.Equal(t, []int64{42}, repo.disabledIDs)
			}
		})
	}
}
