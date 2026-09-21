//go:build unit

package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

type sharedUsageProbeRepo struct {
	AccountRepository
	proxy      *Proxy
	proxyErr   error
	proxyCalls int
	disabledID int64
	disableErr error
	write      func(context.Context, int64, map[string]any) error
}

func (r *sharedUsageProbeRepo) SelectRandomActiveProxy(context.Context) (*Proxy, error) {
	r.proxyCalls++
	return r.proxy, r.proxyErr
}

func (r *sharedUsageProbeRepo) DisableRandomProxyAccountIfUnavailable(_ context.Context, id int64) error {
	r.disabledID = id
	return r.disableErr
}

func (r *sharedUsageProbeRepo) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	if r.write != nil {
		return r.write(ctx, id, updates)
	}
	return nil
}

func sharedUsageProbeAccount(policy string) *Account {
	return &Account{ID: 111, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "test-access-token"},
		Extra: map[string]any{SharedPoolOwnerKey: int64(7), ProxyModeExtraKey: ProxyModeRandom,
			RandomProxyEmptyPoolPolicyExtraKey: policy}}
}

func TestSharedUsageProbeProxyEmptyPoolPolicies(t *testing.T) {
	for _, policy := range []string{RandomProxyEmptyPoolPolicyReject, RandomProxyEmptyPoolPolicyDisable, RandomProxyEmptyPoolPolicyDirect} {
		t.Run(policy, func(t *testing.T) {
			repo := &sharedUsageProbeRepo{}
			svc := &AccountUsageService{accountRepo: repo}
			original := sharedUsageProbeAccount(policy)
			staleID := int64(9)
			original.ProxyID, original.Proxy = &staleID, &Proxy{ID: staleID}
			resolved, err := svc.openAIUsageProbeAccount(context.Background(), original)
			if policy == RandomProxyEmptyPoolPolicyDirect {
				require.NoError(t, err)
				require.Nil(t, resolved.ProxyID)
				require.Nil(t, resolved.Proxy)
			} else {
				require.ErrorIs(t, err, ErrRandomProxyUnavailable)
				require.Nil(t, resolved)
			}
			require.Equal(t, 1, repo.proxyCalls)
			if policy == RandomProxyEmptyPoolPolicyDisable {
				require.Equal(t, original.ID, repo.disabledID)
			} else {
				require.Zero(t, repo.disabledID)
			}
			require.Equal(t, staleID, *original.ProxyID)
			require.Equal(t, staleID, original.Proxy.ID)
		})
	}
}

func TestSharedUsageProbeProxySelectionAndDisableErrorsStopProbe(t *testing.T) {
	for _, failSelection := range []bool{false, true} {
		repo := &sharedUsageProbeRepo{}
		want := errors.New("proxy operation failed")
		if failSelection {
			repo.proxyErr = want
		} else {
			repo.disableErr = want
		}
		svc := &AccountUsageService{accountRepo: repo}
		account := sharedUsageProbeAccount(RandomProxyEmptyPoolPolicyDisable)
		updates, err := svc.probeOpenAICodexSnapshot(context.Background(), account)
		require.Error(t, err)
		require.Contains(t, err.Error(), want.Error())
		require.Nil(t, updates)
		require.Nil(t, account.ProxyID)
	}
}

func TestSharedUsageProbeKeepsFixedProxyAndScopesRandomProxyToCopy(t *testing.T) {
	for _, random := range []bool{false, true} {
		proxy := &Proxy{ID: 4, Protocol: "http", Host: "127.0.0.1", Port: 8111, Status: StatusActive}
		repo := &sharedUsageProbeRepo{proxy: proxy}
		svc := &AccountUsageService{accountRepo: repo}
		account := sharedUsageProbeAccount(RandomProxyEmptyPoolPolicyReject)
		if !random {
			delete(account.Extra, ProxyModeExtraKey)
			account.ProxyID, account.Proxy = &proxy.ID, proxy
		}
		resolved, err := svc.openAIUsageProbeAccount(context.Background(), account)
		require.NoError(t, err)
		require.NotSame(t, account, resolved)
		require.Equal(t, proxy.ID, *resolved.ProxyID)
		require.Same(t, proxy, resolved.Proxy)
		if random {
			require.Equal(t, 1, repo.proxyCalls)
			require.Nil(t, account.ProxyID)
			require.Nil(t, account.Proxy)
		} else {
			require.Zero(t, repo.proxyCalls)
			require.Same(t, proxy, account.Proxy)
		}
	}
}

func sharedUsageLocalProxy(t *testing.T, handler http.HandlerFunc) *Proxy {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	u, err := url.Parse(server.URL)
	require.NoError(t, err)
	host, port, err := net.SplitHostPort(u.Host)
	require.NoError(t, err)
	portNumber, err := strconv.Atoi(port)
	require.NoError(t, err)
	return &Proxy{ID: 4, Protocol: "http", Host: host, Port: portNumber, Status: StatusActive}
}

func TestSharedUsageProbeActuallyConnectsThroughSelectedProxy(t *testing.T) {
	requests := make(chan string, 1)
	proxy := sharedUsageLocalProxy(t, func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Method + " " + r.Host
		w.WriteHeader(http.StatusBadGateway)
	})
	repo := &sharedUsageProbeRepo{proxy: proxy}
	svc := &AccountUsageService{accountRepo: repo}
	account := sharedUsageProbeAccount(RandomProxyEmptyPoolPolicyReject)
	_, err := svc.probeOpenAICodexSnapshot(context.Background(), account)
	require.Error(t, err)
	select {
	case request := <-requests:
		require.Equal(t, "CONNECT chatgpt.com:443", request)
	default:
		t.Fatal("probe did not reach the selected local proxy")
	}
	require.Nil(t, account.ProxyID)
	require.Nil(t, account.Proxy)
}
