package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexTicketProxyStickinessCountsOnlyFailedStage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		phase  int
		status int
		body   string
		cause  error
		failed bool
	}{
		{name: "success"},
		{name: "harvest_mismatch", phase: 1, body: `{"type":"response.completed","response":{"status":"completed","model":"wrong"}}`, failed: true},
		{name: "business_mismatch", phase: 2, body: `{"type":"response.completed","response":{"status":"completed","model":"wrong"}}`, failed: true},
		{name: "harvest_transport", phase: 1, cause: errors.New("connection reset"), failed: true},
		{name: "business_transport", phase: 2, cause: errors.New("connection reset"), failed: true},
		{name: "business_500", phase: 2, status: 500, failed: true},
		{name: "harvest_429", phase: 1, status: 429},
		{name: "business_401", phase: 2, status: 401},
		{name: "business_quota_200", phase: 2, body: `{"type":"response.failed","response":{"status":"failed","error":{"type":"usage_limit_reached"}}}`},
		{name: "cancelled", phase: 2, cause: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := codexScheduledFixture(t, false)
			repo.account.Extra[ProxyModeExtraKey] = ProxyModeRandom
			repo.proxy = &Proxy{ID: 8, Status: StatusActive, Protocol: "http", Host: "business.example", Port: 8080}
			calls := 0
			svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
				calls++
				resp := codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(356))
				if calls != tc.phase {
					return resp, nil
				}
				if tc.status != 0 {
					resp.StatusCode = tc.status
				}
				if tc.body != "" {
					resp.Body = io.NopCloser(strings.NewReader("data: " + tc.body + "\n\n"))
				}
				return resp, tc.cause
			}}
			svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
			require.Len(t, repo.finishes, 1)
			finish := repo.finishes[0]
			require.Equal(t, tc.failed && tc.phase == 1, finish.HarvestProxyFailed)
			require.Equal(t, tc.failed && tc.phase == 2, finish.BusinessProxyFailed)
			require.Equal(t, tc.phase == 0, finish.BusinessProxySucceeded)
			require.Zero(t, repo.businessFailures, "共享调度必须使用配置阈值，不能另外调用旧失败换绑逻辑")
		})
	}
}

func TestCodexTicketProxyStickinessWaitAndOuterCancelAreNeutral(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.False(t, codexTicketProxyAttemptFailed(ctx, 500, errors.New("transport"), false))
	for _, cause := range []error{errOpenAICodexTicketControlsChanged, &CodexTicketWaitError{Status: &CodexTicketRuntimeStatus{Reason: "capacity"}}} {
		require.False(t, codexTicketProxyAttemptFailed(context.Background(), 0, cause, true))
	}
}

func TestCodexTicketProxyFailureThresholdSettings(t *testing.T) {
	svc, repo := newTicketPolicySettings()
	cfg, err := svc.GetCodexTicketSettings(context.Background())
	require.NoError(t, err)
	for _, threshold := range []int{0, 1, 7, 1000} {
		cfg.ProxyFailureThreshold = threshold
		stored, err := svc.UpdateCodexTicketSettings(context.Background(), cfg)
		require.NoError(t, err)
		if threshold == 0 {
			threshold = 3
		}
		require.Equal(t, threshold, stored.ProxyFailureThreshold)
	}
	before := repo.values[SettingKeyCodexTicketPolicy]
	for _, threshold := range []int{-1, 1001} {
		cfg.ProxyFailureThreshold = threshold
		_, err := svc.UpdateCodexTicketSettings(context.Background(), cfg)
		require.Error(t, err)
		require.Equal(t, before, repo.values[SettingKeyCodexTicketPolicy])
	}
}
