package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func codexTicketBackoffFailure(kind string) func(*http.Request) (*http.Response, error) {
	return func(req *http.Request) (*http.Response, error) {
		switch kind {
		case "ineligible_ticket":
			return codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(312)), nil
		case "wrong_model":
			return codexTicketCompletedResponse("gpt-other", fakeCodexTicketState(292)), nil
		case "http_5xx":
			return &http.Response{StatusCode: http.StatusServiceUnavailable,
				Header: http.Header{}, Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
		case "network_error":
			return nil, errors.New("dial tcp: connection refused")
		case "business_wrong_model":
			if req.Header.Get(openAICodexTurnStateHeader) != "" {
				return codexTicketCompletedResponse("gpt-other", ""), nil
			}
		case "business_ineligible_ticket":
			if req.Header.Get(openAICodexTurnStateHeader) != "" {
				return codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(312)), nil
			}
		}
		return codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(292)), nil
	}
}

func codexTicketBackoffIntegrationFixture(t *testing.T, kind string) (*OpenAIGatewayService, *Account, *int) {
	t.Helper()
	calls := new(int)
	respond := codexTicketBackoffFailure(kind)
	upstream := &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		*calls++
		return respond(req)
	}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{
		Enabled: true, HarvestProxyURL: "http://harvest.example:8080",
	}, upstream)
	return svc, ticketTestAccount(41), calls
}

func TestCodexTicketBackoffIntegrationSuppressesRepeatedFailures(t *testing.T) {
	for _, tc := range []struct {
		kind         string
		initialCalls int
	}{
		{"ineligible_ticket", 1},
		{"wrong_model", 1},
		{"http_5xx", 1},
		{"network_error", 1},
		{"business_wrong_model", 2},
		{"business_ineligible_ticket", 2},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			svc, account, calls := codexTicketBackoffIntegrationFixture(t, tc.kind)
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			require.Equal(t, tc.initialCalls, *calls, "首轮必须实际到达失败阶段")
			require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			require.Equal(t, tc.initialCalls, *calls, "退避期间第二轮不得再次请求上游")
		})
	}
}

func TestCodexTicketBackoffIntegrationFailedRenewalPreservesOldTicket(t *testing.T) {
	svc, account, calls := codexTicketBackoffIntegrationFixture(t, "business_ineligible_ticket")
	previous := &openAICodexTicket{Model: "gpt-6-astra", State: fakeCodexTicketState(292), Length: 292,
		CapturedAt: time.Now().Add(-59 * time.Minute), ExpiresAt: time.Now().Add(time.Minute)}
	require.True(t, svc.storeOpenAICodexTicket(context.Background(), account, previous))
	previous = svc.lookupOpenAICodexTicket(account, previous.Model)
	svc.probeOnceOpenAICodexTicket(context.Background(), account, previous.Model)
	require.Equal(t, 2, *calls)
	checkOldTicket := func() {
		t.Helper()
		require.Same(t, previous, svc.lookupOpenAICodexTicket(account, previous.Model))
		headers := http.Header{}
		require.NoError(t, svc.applyOpenAICodexTicket(context.Background(), account, previous.Model, headers))
		require.Equal(t, previous.State, headers.Get(openAICodexTurnStateHeader))
	}
	checkOldTicket()
	svc.probeOnceOpenAICodexTicket(context.Background(), account, previous.Model)
	checkOldTicket()
	require.Equal(t, 2, *calls, "失败续票应保留旧票并退避")
}

func TestCodexTicketBackoffIntegrationIndependentIdentity(t *testing.T) {
	for _, change := range []string{"model", "account", "access_token", "refresh_token"} {
		t.Run(change, func(t *testing.T) {
			svc, account, calls := codexTicketBackoffIntegrationFixture(t, "http_5xx")
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			require.Equal(t, 1, *calls)
			model := "gpt-6-astra"
			switch change {
			case "model":
				model = "gpt-5.6-sol"
			case "account":
				account = ticketTestAccount(42)
			case "access_token", "refresh_token":
				account.Credentials[change] = "rotated-test-credential"
			}
			svc.probeOnceOpenAICodexTicket(context.Background(), account, model)
			require.Equal(t, 2, *calls, "其它账号、模型或凭证不能被普通失败连带阻断")
			svc.probeOnceOpenAICodexTicket(context.Background(), account, model)
			require.Equal(t, 2, *calls, "新的身份发生失败后也必须退避")
		})
	}
}

func TestCodexTicketBackoffIntegrationProxyRotationDoesNotBypass(t *testing.T) {
	svc, account, calls := codexTicketBackoffIntegrationFixture(t, "ineligible_ticket")
	account.Extra = map[string]any{ProxyModeExtraKey: ProxyModeRandom,
		RandomProxyPoolIDsExtraKey: []int64{7}}
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	require.Equal(t, 1, *calls)
	account.Extra[RandomProxyPoolIDsExtraKey] = []int64{8, 9}
	proxyID := int64(8)
	account.ProxyID, account.Proxy = &proxyID, &Proxy{ID: 8, Protocol: "http", Host: "next.example", Port: 8080}
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	require.Equal(t, 1, *calls, "随机代理池和运行时出口变化不能绕过账号模型退避")
}
