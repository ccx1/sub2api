package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodexTicketExperimentHeadersFollowAccountIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, model, canonical, custom, originator, version string
		force, disableEnforcement                           bool
	}{
		{name: "astra minimum", model: "gpt-6-astra", originator: "codex-tui", version: "0.153.4"},
		{name: "account identity", model: "gpt-6-astra", custom: "codex_vscode/0.125.0 (Windows 11; x86_64) vscode", originator: "codex_vscode", version: "0.153.4"},
		{name: "account identity without enforcement", model: "gpt-6-astra", custom: "codex_vscode/0.125.0 (Windows 11; x86_64) vscode", disableEnforcement: true, originator: "codex_vscode", version: "0.153.4"},
		{name: "current version", model: "gpt-6-astra", canonical: "codex-tui/0.200.1 (Linux; x86_64) xterm", custom: "codex_vscode/0.125.0 (Windows 11; x86_64) vscode", originator: "codex_vscode", version: "0.200.1"},
		{name: "force canonical", model: "gpt-6-astra", custom: "codex_vscode/0.125.0 (Windows 11; x86_64) vscode", force: true, originator: "codex-tui", version: "0.153.4"},
		{name: "other model", model: "gpt-5.6-sol", custom: "codex_vscode/0.125.0 (Windows 11; x86_64) vscode", originator: "codex_vscode", version: codexCLIVersion},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previousEnforcement := codexIdentityEnforcement.Load()
			SetCodexIdentityEnforcementEnabled(!tc.disableEnforcement)
			t.Cleanup(func() { SetCodexIdentityEnforcementEnabled(previousEnforcement) })
			SetCodexCanonicalUserAgentResolver(func() string { return tc.canonical })
			t.Cleanup(func() { SetCodexCanonicalUserAgentResolver(nil) })
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{}, nil)
			svc.cfg.Gateway.ForceCodexCLI = tc.force
			account := ticketTestAccount(41)
			account.Credentials["user_agent"] = tc.custom
			req, err := svc.buildOpenAICodexTicketProbeRequest(context.Background(), openAICodexTicketProbeInput{Account: account, Token: "test-token", Model: tc.model})
			require.NoError(t, err)
			requireCodexTicketExperimentHeaders(t, req, tc.model)
			require.Equal(t, tc.originator, req.Header.Get("originator"))
			require.Equal(t, tc.version, req.Header.Get("version"))
			require.Equal(t, tc.version, openai.CodexUserAgentVersion(req.Header.Get("user-agent")))
			if tc.custom != "" && !tc.force {
				require.Contains(t, req.Header.Get("user-agent"), "(Windows 11; x86_64) vscode")
			}
		})
	}
}

func TestCodexTicketExperimentHeadersReachBothProbeStages(t *testing.T) {
	state := fakeCodexTicketState(292)
	upstream := &codexTicketVerificationUpstream{respond: func(int) *http.Response {
		return codexTicketCompletedResponse("gpt-6-astra", state)
	}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://harvest.example:8080"}, upstream)
	account := ticketTestAccount(41)
	account.Credentials["user_agent"] = "codex_vscode/0.125.0 (Windows 11; x86_64) vscode"
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	require.Len(t, upstream.requests, 2)
	for i, req := range upstream.requests {
		requireCodexTicketExperimentHeaders(t, req, "gpt-6-astra")
		require.Equal(t, "codex_vscode", req.Header.Get("originator"))
		if i == 0 {
			require.Empty(t, req.Header.Get(openAICodexTurnStateHeader))
		} else {
			require.Equal(t, state, req.Header.Get(openAICodexTurnStateHeader))
		}
	}
	require.NotNil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
}

func requireCodexTicketExperimentHeaders(t *testing.T, req *http.Request, model string) {
	t.Helper()
	require.Equal(t, "model="+model, req.Header.Get(openAICodexRoutingHintHeader))
	require.Empty(t, req.Header.Get("OpenAI-Beta"))
	require.Equal(t, openAIRemoteCompactionV2Feature, req.Header.Get("x-codex-beta-features"))
	require.Equal(t, "text/event-stream", req.Header.Get("accept"))
	require.Equal(t, "identity", req.Header.Get("accept-encoding"))
	require.Equal(t, "acc-1", req.Header.Get("chatgpt-account-id"))
	require.True(t, strings.HasPrefix(req.Header.Get("authorization"), "Bearer "))
	body, err := req.GetBody()
	require.NoError(t, err)
	defer body.Close()
	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, model, gjson.GetBytes(raw, "model").String())
	require.Equal(t, "ping", gjson.GetBytes(raw, "input.0.content.0.text").String())
}
