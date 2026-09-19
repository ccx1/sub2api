package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestProtectionSurvivesStaleAdminForm(t *testing.T) {
	ctx := context.Background()
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
		1: {ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Concurrency: 4, Extra: map[string]any{}},
	}}
	admin := &adminServiceImpl{accountRepo: repo}
	protection := NewAntiDegradeService(admin)
	enabled, err := protection.SetProtection(ctx, 1, true, false)
	require.NoError(t, err)
	staleEnabled := copyProtectionTestExtra(enabled.Extra)
	updated, err := admin.UpdateAccount(ctx, 1, &UpdateAccountInput{Name: "renamed", Extra: map[string]any{"custom": true}})
	require.NoError(t, err)
	require.True(t, updated.AntiDegradationEnabled())
	require.Equal(t, enabled.Extra[AntiDegradeMarkerExtraKey], updated.Extra[AntiDegradeMarkerExtraKey])
	require.Equal(t, "session", updated.Extra[codexFingerprintModeExtraKey])
	_, err = protection.SetProtection(ctx, 1, false, true)
	require.NoError(t, err)
	updated, err = admin.UpdateAccount(ctx, 1, &UpdateAccountInput{Extra: staleEnabled})
	require.NoError(t, err)
	require.False(t, updated.AntiDegradationEnabled(), "旧的开启表单不能重新启用已经关闭的策略")
	require.Equal(t, codexFingerprintOff, updated.GetCodexFingerprintMode())
	require.False(t, updated.IsTLSFingerprintEnabled())
}

func copyProtectionTestExtra(extra map[string]any) map[string]any {
	copy := make(map[string]any, len(extra))
	for key, value := range extra {
		copy[key] = value
	}
	return copy
}

type protectionTransportRecorder struct {
	standard, fingerprint int
	profile               *tlsfingerprint.Profile
}

func (u *protectionTransportRecorder) Do(*http.Request, string, int64, int) (*http.Response, error) {
	u.standard++
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
}

func (u *protectionTransportRecorder) DoWithTLS(_ *http.Request, _ string, _ int64, _ int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	u.fingerprint++
	u.profile = profile
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
}

func TestProtectionOpenAIHTTPUsesConfiguredTLS(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "enabled", false: "globally_disabled"}[enabled], func(t *testing.T) {
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 4, Extra: map[string]any{}}
			configureAccountProtection(account)
			cfg := &config.Config{}
			cfg.Gateway.TLSFingerprint.Enabled = enabled
			transport := &protectionTransportRecorder{}
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: transport}
			request := httptest.NewRequest(http.MethodPost, "https://example.test/responses", nil)
			response, err := svc.doOpenAIUpstream(request, "", account)
			require.NoError(t, err)
			defer response.Body.Close()
			if enabled {
				require.Equal(t, 1, transport.fingerprint)
				require.Equal(t, tlsfingerprint.BuiltinProfile("nodejs24").CacheKey(), transport.profile.CacheKey())
			} else {
				require.Equal(t, 1, transport.standard)
				require.Zero(t, transport.fingerprint)
			}
		})
	}
}

func TestProtectionRejectsLossyHTTPBeforeBuildingRequest(t *testing.T) {
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{requestIntegrityModeKey: "enforce"}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	stageMode1Request(c, account, []byte(`{"model":"gpt-5","input":"keep my context","tools":[{"type":"function","name":"lookup"}]}`))
	svc := &OpenAIGatewayService{}
	_, err := svc.buildUpstreamRequest(context.Background(), c, account, []byte(`{"model":"gpt-5","input":"keep my context"}`), "token", true, "", false)
	require.ErrorContains(t, err, "tools")
	_, err = svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, []byte(`{"model":"gpt-5","input":"keep my context"}`), "token")
	require.ErrorContains(t, err, "tools")
}

type protectionRandomProxyTestRepo struct {
	*upstreamBillingProbeAccountRepo
}

func (r *protectionRandomProxyTestRepo) SelectRandomActiveProxy(context.Context) (*Proxy, error) {
	return nil, nil
}

func TestProtectionAccountConnectionTestRejectsEmptyProxyPool(t *testing.T) {
	for _, policy := range []string{RandomProxyEmptyPoolPolicyReject, RandomProxyEmptyPoolPolicyDisable} {
		t.Run(policy, func(t *testing.T) {
			repo := &protectionRandomProxyTestRepo{&upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
				1: {ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Extra: map[string]any{ProxyModeExtraKey: ProxyModeRandom, RandomProxyEmptyPoolPolicyExtraKey: policy}},
			}}}
			transport := &protectionTransportRecorder{}
			svc := &AccountTestService{accountRepo: repo, httpUpstream: transport}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/accounts/1/test", nil)
			err := svc.TestAccountConnection(c, 1, "gpt-5", "hello", "")
			require.ErrorContains(t, err, "random proxy")
			require.Zero(t, transport.standard+transport.fingerprint)
			if policy == RandomProxyEmptyPoolPolicyDisable {
				account, err := repo.GetByID(context.Background(), 1)
				require.NoError(t, err)
				require.Equal(t, StatusDisabled, account.Status)
			}
		})
	}
}
