package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type pluginDirectoryProxyRepo struct {
	AccountRepository
	account      *Account
	proxy        *Proxy
	selectionErr error
	disableErr   error
	globalCalls  int
	selectedIDs  []int64
	disabledIDs  []int64
}

func (r *pluginDirectoryProxyRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	if r.account == nil || r.account.ID != id {
		return nil, ErrAccountNotFound
	}
	account := *r.account
	return &account, nil
}

func (r *pluginDirectoryProxyRepo) SelectRandomActiveProxy(context.Context) (*Proxy, error) {
	r.globalCalls++
	return r.proxy, r.selectionErr
}

func (r *pluginDirectoryProxyRepo) SelectRandomActiveProxyFromPool(_ context.Context, ids []int64) (*Proxy, error) {
	r.selectedIDs = append([]int64(nil), ids...)
	return r.proxy, r.selectionErr
}

func (r *pluginDirectoryProxyRepo) DisableRandomProxyAccountIfUnavailable(_ context.Context, id int64) error {
	r.disabledIDs = append(r.disabledIDs, id)
	if r.disableErr != nil {
		return r.disableErr
	}
	r.account.Status = StatusDisabled
	r.account.Schedulable = false
	return nil
}

func pluginDirectoryAccount(policy string) *Account {
	return &Account{
		ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{
			"access_token": "test-access-token", "chatgpt_account_id": "test-chatgpt-account",
		},
		Extra: map[string]any{
			ProxyModeExtraKey: ProxyModeRandom, RandomProxyEmptyPoolPolicyExtraKey: policy,
		},
	}
}

func TestResolvePluginOutboundIdentityPausesDuringDailyCooldown(t *testing.T) {
	a := pluginDirectoryAccount(RandomProxyEmptyPoolPolicyReject)
	now := time.Now().UTC()
	a.Extra[DailyCooldownExtraKey] = map[string]any{"enabled": true,
		"start": now.Add(-time.Hour).Format("15:04"), "end": now.Add(time.Hour).Format("15:04"), "timezone": "UTC"}
	repo := &pluginDirectoryProxyRepo{account: a}
	svc := &OpenAIGatewayService{accountRepo: repo}
	identity, err := svc.ResolvePluginOutboundIdentity(context.Background(), a.ID)
	require.NoError(t, err)
	require.Nil(t, identity)
	require.Zero(t, repo.globalCalls, "冷却时不能分配出口或暴露出站凭据")
	require.Empty(t, repo.disabledIDs, "每日冷却不能永久禁用账号")
}

func TestResolvePluginOutboundIdentityUsesSelectedRandomProxy(t *testing.T) {
	account := pluginDirectoryAccount(RandomProxyEmptyPoolPolicyReject)
	account.Extra[RandomProxyPoolScopeExtraKey] = RandomProxyPoolSelected
	account.Extra[RandomProxyPoolIDsExtraKey] = []int64{9, 7}
	repo := &pluginDirectoryProxyRepo{
		account: account,
		proxy:   &Proxy{ID: 7, Protocol: "socks5", Host: "proxy.example", Port: 1080, Status: StatusActive},
	}
	svc := &OpenAIGatewayService{accountRepo: repo}
	identity, err := svc.ResolvePluginOutboundIdentity(context.Background(), account.ID)
	require.NoError(t, err)
	require.NotNil(t, identity)
	require.Equal(t, "socks5://proxy.example:1080", identity.ProxyURL)
	require.Equal(t, []int64{7, 9}, repo.selectedIDs)
	require.Zero(t, repo.globalCalls)
	require.Nil(t, repo.account.ProxyID, "随机代理选择不得持久化成固定代理")
	require.Equal(t, "test-access-token", identity.Token)
	require.Equal(t, "test-chatgpt-account", identity.Headers.Get("chatgpt-account-id"))
	require.Equal(t, "responses=experimental", identity.Headers.Get("OpenAI-Beta"))
	require.NotEmpty(t, identity.Headers.Get("User-Agent"))
}

func TestResolvePluginOutboundIdentityHonorsEmptyPoolPolicy(t *testing.T) {
	for _, policy := range []string{RandomProxyEmptyPoolPolicyReject, RandomProxyEmptyPoolPolicyDisable, RandomProxyEmptyPoolPolicyDirect} {
		t.Run(policy, func(t *testing.T) {
			repo := &pluginDirectoryProxyRepo{account: pluginDirectoryAccount(policy)}
			svc := &OpenAIGatewayService{accountRepo: repo}
			identity, err := svc.ResolvePluginOutboundIdentity(context.Background(), repo.account.ID)
			if policy == RandomProxyEmptyPoolPolicyDirect {
				require.NoError(t, err)
				require.NotNil(t, identity)
				require.Empty(t, identity.ProxyURL)
			} else {
				require.ErrorIs(t, err, ErrRandomProxyUnavailable)
				require.Nil(t, identity, "代理不可用时不得交付可供插件直连的身份")
			}
			if policy == RandomProxyEmptyPoolPolicyDisable {
				require.Equal(t, []int64{repo.account.ID}, repo.disabledIDs)
				require.Equal(t, StatusDisabled, repo.account.Status)
				require.False(t, repo.account.Schedulable)
			} else {
				require.Empty(t, repo.disabledIDs)
				require.Equal(t, StatusActive, repo.account.Status)
			}
		})
	}
}

func TestResolvePluginOutboundIdentityFailsClosedOnProxyErrors(t *testing.T) {
	t.Run("selection", func(t *testing.T) {
		failure := errors.New("proxy lookup failed")
		repo := &pluginDirectoryProxyRepo{account: pluginDirectoryAccount(RandomProxyEmptyPoolPolicyDirect), selectionErr: failure}
		svc := &OpenAIGatewayService{accountRepo: repo}
		identity, err := svc.ResolvePluginOutboundIdentity(context.Background(), repo.account.ID)
		require.ErrorIs(t, err, failure)
		require.Nil(t, identity)
		require.Empty(t, repo.disabledIDs)
	})
	t.Run("disable", func(t *testing.T) {
		repo := &pluginDirectoryProxyRepo{
			account: pluginDirectoryAccount(RandomProxyEmptyPoolPolicyDisable), disableErr: errors.New("disable update failed"),
		}
		svc := &OpenAIGatewayService{accountRepo: repo}
		identity, err := svc.ResolvePluginOutboundIdentity(context.Background(), repo.account.ID)
		require.ErrorIs(t, err, ErrRandomProxyUnavailable)
		require.ErrorContains(t, err, "disable update failed")
		require.Nil(t, identity)
	})
}

func TestResolvePluginOutboundIdentityKeepsFixedProxy(t *testing.T) {
	account := pluginDirectoryAccount("")
	account.Extra = nil
	proxyID := int64(8)
	account.ProxyID = &proxyID
	account.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "fixed.example", Port: 8080, Status: StatusActive}
	repo := &pluginDirectoryProxyRepo{account: account, selectionErr: errors.New("must not select")}
	svc := &OpenAIGatewayService{accountRepo: repo}
	identity, err := svc.ResolvePluginOutboundIdentity(context.Background(), account.ID)
	require.NoError(t, err)
	require.NotNil(t, identity)
	require.Equal(t, "http://fixed.example:8080", identity.ProxyURL)
	require.Zero(t, repo.globalCalls)
	require.Empty(t, repo.selectedIDs)
}

func TestResolvePluginOutboundIdentityKeepsAccountScope(t *testing.T) {
	for _, kind := range []string{"other-platform", "api-key", "setup-token", "shadow"} {
		t.Run(kind, func(t *testing.T) {
			account := pluginDirectoryAccount(RandomProxyEmptyPoolPolicyDisable)
			switch kind {
			case "other-platform":
				account.Platform = PlatformAnthropic
			case "api-key":
				account.Type = AccountTypeAPIKey
			case "setup-token":
				account.Type = AccountTypeSetupToken
			case "shadow":
				parentID := int64(1)
				account.ParentAccountID = &parentID
			}
			repo := &pluginDirectoryProxyRepo{account: account}
			svc := &OpenAIGatewayService{accountRepo: repo}
			identity, err := svc.ResolvePluginOutboundIdentity(context.Background(), account.ID)
			require.NoError(t, err)
			require.Nil(t, identity)
			require.Zero(t, repo.globalCalls)
			require.Empty(t, repo.disabledIDs)
		})
	}
}
