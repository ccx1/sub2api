//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

type sharedQuotaProxyRepository struct {
	*stubQuotaAccountRepo
	proxy        *Proxy
	selectionErr error
	selections   int
	disabled     []int64
}

func (r *sharedQuotaProxyRepository) SelectRandomActiveProxy(context.Context) (*Proxy, error) {
	r.selections++
	return r.proxy, r.selectionErr
}

func (r *sharedQuotaProxyRepository) DisableRandomProxyAccountIfUnavailable(_ context.Context, id int64) error {
	r.disabled = append(r.disabled, id)
	return nil
}

type sharedQuotaProbeTokens struct {
	*stubQuotaTokenCache
	reads int
}

func (c *sharedQuotaProbeTokens) GetAccessToken(ctx context.Context, key string) (string, error) {
	c.reads++
	return c.stubQuotaTokenCache.GetAccessToken(ctx, key)
}

func sharedQuotaProxyFixture() (*OpenAIQuotaService, *sharedQuotaProxyRepository, *sharedQuotaProbeTokens, *Account) {
	account := &Account{ID: 11, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
		Credentials: map[string]any{"chatgpt_account_id": "test-account"}, Extra: map[string]any{
			SharedPoolOwnerKey: int64(7), ProxyModeExtraKey: "random", RandomProxyEmptyPoolPolicyExtraKey: RandomProxyEmptyPoolPolicyReject,
		}}
	repo := &sharedQuotaProxyRepository{stubQuotaAccountRepo: &stubQuotaAccountRepo{accounts: map[int64]*Account{11: account}}}
	tokens := &sharedQuotaProbeTokens{stubQuotaTokenCache: &stubQuotaTokenCache{tokens: map[string]string{OpenAITokenCacheKey(account): "test-token"}}}
	svc := NewOpenAIQuotaService(repo, nil, NewOpenAITokenProvider(repo, tokens, nil), func(string) (*req.Client, error) { return nil, errors.New("unexpected network") })
	return svc, repo, tokens, account
}

func TestSharedQuotaRandomProxyResolvedOnRequestCopyBeforeToken(t *testing.T) {
	svc, repo, tokens, account := sharedQuotaProxyFixture()
	repo.proxy = &Proxy{ID: 33, Protocol: "http", Host: "proxy.example.invalid", Port: 8080, Status: StatusActive}
	token, accountID, proxyURL, _, err := svc.prepareUpstreamCall(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, "test-token", token)
	require.Equal(t, "test-account", accountID)
	require.Equal(t, repo.proxy.URL(), proxyURL)
	require.Equal(t, 1, tokens.reads)
	require.Equal(t, 1, repo.selections)
	require.Nil(t, account.ProxyID)
	require.Nil(t, account.Proxy)
	require.Empty(t, repo.disabled)
}

func TestSharedQuotaRandomProxyEmptyPoolPolicies(t *testing.T) {
	for _, policy := range []string{RandomProxyEmptyPoolPolicyReject, RandomProxyEmptyPoolPolicyDisable, RandomProxyEmptyPoolPolicyDirect} {
		t.Run(policy, func(t *testing.T) {
			svc, repo, tokens, account := sharedQuotaProxyFixture()
			account.Extra[RandomProxyEmptyPoolPolicyExtraKey] = policy
			stale := int64(100)
			account.ProxyID, account.Proxy = &stale, &Proxy{ID: stale, Protocol: "http", Host: "stale.example.invalid", Port: 8080}
			_, _, proxyURL, _, err := svc.prepareUpstreamCall(context.Background(), account.ID)
			if policy == RandomProxyEmptyPoolPolicyDirect {
				require.NoError(t, err)
				require.Equal(t, 1, tokens.reads)
			} else {
				require.ErrorIs(t, err, ErrRandomProxyUnavailable)
				require.Zero(t, tokens.reads)
			}
			require.Empty(t, proxyURL)
			if policy == RandomProxyEmptyPoolPolicyDisable {
				require.Equal(t, []int64{account.ID}, repo.disabled)
			} else {
				require.Empty(t, repo.disabled)
			}
			require.Equal(t, &stale, account.ProxyID)
			require.Equal(t, stale, account.Proxy.ID)
		})
	}
}

func TestSharedQuotaProxyFailureAndFixedProxy(t *testing.T) {
	svc, repo, tokens, account := sharedQuotaProxyFixture()
	repo.selectionErr = errors.New("selector failed")
	_, _, _, _, err := svc.prepareUpstreamCall(context.Background(), account.ID)
	require.ErrorIs(t, err, repo.selectionErr)
	require.Zero(t, tokens.reads)
	delete(account.Extra, ProxyModeExtraKey)
	fixed := int64(42)
	account.ProxyID, account.Proxy = &fixed, &Proxy{ID: fixed, Protocol: "http", Host: "fixed.example.invalid", Port: 8888}
	_, _, proxyURL, _, err := svc.prepareUpstreamCall(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, account.Proxy.URL(), proxyURL)
	require.Equal(t, 1, repo.selections, "固定代理不能进入随机选择")
	require.Equal(t, 1, tokens.reads)
}

func TestSharedQuotaShadowQueryUsesParentRandomProxy(t *testing.T) {
	svc, repo, _, parent := sharedQuotaProxyFixture()
	repo.proxy = &Proxy{ID: 33, Protocol: "http", Host: "proxy.example.invalid", Port: 8080, Status: StatusActive}
	parentID := parent.ID
	shadow := &Account{ID: 12, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parentID}
	repo.accounts[shadow.ID] = shadow
	_, accountID, proxyURL, _, err := svc.prepareUpstreamCall(context.Background(), shadow.ID)
	require.NoError(t, err)
	require.Equal(t, "test-account", accountID)
	require.Equal(t, repo.proxy.URL(), proxyURL)
	require.Nil(t, parent.ProxyID)
	require.Nil(t, shadow.ProxyID)
	_, err = svc.ResetCredit(context.Background(), shadow.ID)
	require.ErrorIs(t, err, ErrSparkShadowResetNotSupported)
}
