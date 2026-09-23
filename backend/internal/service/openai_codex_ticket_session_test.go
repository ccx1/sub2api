package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func ticketSessionRequest(t *testing.T, svc *OpenAIGatewayService, in openAICodexTicketProbeInput) *http.Request {
	t.Helper()
	req, err := svc.buildOpenAICodexTicketProbeRequest(context.Background(), in)
	require.NoError(t, err)
	t.Cleanup(func() { _ = req.Body.Close() })
	_, err = uuid.Parse(req.Header.Get("session_id"))
	require.NoError(t, err)
	return req
}

func TestCodexTicketSessionModesIsolationAndRestart(t *testing.T) {
	for _, mode := range []string{"", "random", "account", "account_model"} {
		t.Run("mode="+mode, func(t *testing.T) {
			cfg := config.OpenAICodexTicketConfig{SessionMode: mode}
			svc := ticketTestService(t, cfg, nil)
			input := openAICodexTicketProbeInput{Account: ticketTestAccount(41), Token: "test", Model: "gpt-6-astra"}
			first := ticketSessionRequest(t, svc, input).Header.Get("session_id")
			second := ticketSessionRequest(t, ticketTestService(t, cfg, nil), input).Header.Get("session_id")
			if mode == "" || mode == "random" {
				require.NotEqual(t, first, second)
				return
			}
			require.Equal(t, first, second, "重启及另一实例应保持相同会话")
			input.Model = "gpt-5.6-sol"
			otherModel := ticketSessionRequest(t, svc, input).Header.Get("session_id")
			require.Equal(t, mode == "account", first == otherModel)
			input.Account = ticketTestAccount(42)
			require.NotEqual(t, otherModel, ticketSessionRequest(t, svc, input).Header.Get("session_id"))
			input.Account, input.Model = ticketTestAccount(41), " gpt-6-astra "
			input.Token, input.ProxyURL, input.State = "refreshed", "http://next.example", "candidate"
			require.Equal(t, first, ticketSessionRequest(t, svc, input).Header.Get("session_id"), "代理/令牌/阶段不改变锁定会话")
		})
	}
}

func TestCodexTicketSessionModesReachHarvestVerificationAndRetry(t *testing.T) {
	for _, mode := range []string{"random", "account", "account_model"} {
		t.Run(mode, func(t *testing.T) {
			upstream := &codexTicketVerificationUpstream{respond: func(int) *http.Response {
				return codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(292))
			}}
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, SessionMode: mode, HarvestProxyURL: "http://harvest.example:8080"}, upstream)
			account := ticketTestAccount(41)
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			require.Len(t, upstream.requests, 2)
			ticket := svc.lookupOpenAICodexTicket(account, "gpt-6-astra")
			require.NotNil(t, ticket)
			ticket.ExpiresAt = time.Now().Add(-time.Second)
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			require.Len(t, upstream.requests, 4)
			sessions := map[string]bool{}
			for _, req := range upstream.requests {
				sessions[req.Header.Get("session_id")] = true
			}
			if mode == "random" {
				require.Len(t, sessions, 2, "每次采票使用新 session，采票与业务复验复用同一 session")
			} else {
				require.Len(t, sessions, 1)
			}
		})
	}
}

func TestCodexTicketSessionModePreviewUsesSnapshotWithoutWrites(t *testing.T) {
	for _, mode := range []string{"random", "account", "account_model"} {
		t.Run(mode, func(t *testing.T) {
			svc, repo := ticketPreviewService(t)
			svc.cfg.Gateway.OpenAICodexTicket.SessionMode = mode
			before, err := json.Marshal(repo.account)
			require.NoError(t, err)
			preview, err := svc.PreviewOpenAICodexTicketRequest(context.Background(), 41, CodexTicketRequestPreviewInput{Model: "gpt-6-astra"})
			require.NoError(t, err)
			require.False(t, preview.Sent)
			req := ticketSessionRequest(t, svc, openAICodexTicketProbeInput{Account: repo.account, Token: "test", Model: "gpt-6-astra"})
			require.Equal(t, mode != "random", http.Header(preview.BeforeHeaders).Get("session_id") == req.Header.Get("session_id"))
			after, err := json.Marshal(repo.account)
			require.NoError(t, err)
			require.Equal(t, string(before), string(after))
		})
	}
}

func TestCodexTicketSessionModeSwitchStopsInFlightWithoutRevokingIssued(t *testing.T) {
	for _, modes := range [][2]string{{"random", "account"}, {"account", "account_model"}, {"account_model", "random"}} {
		t.Run(modes[0]+"_to_"+modes[1], func(t *testing.T) {
			svc, account, previous, _ := ticketWatchdogFixture(t)
			svc.cfg.Gateway.OpenAICodexTicket.SessionMode = modes[0]
			snapshot := svc.openAICodexTicketConfig()
			input := openAICodexTicketProbeInput{Account: account, Token: "test", Model: previous.Model, Config: &snapshot, SubscriptionTier: openAICodexTicketSubscriptionTier(account), CheckControls: true}
			candidate := *previous
			candidate.Verified, candidate.Binding = true, svc.codexTicketBindingForConfig(account, snapshot)
			require.True(t, svc.storeOpenAICodexTicket(context.Background(), account, &candidate))
			svc.cfg.Gateway.OpenAICodexTicket.SessionMode = modes[1]
			require.False(t, svc.openAICodexTicketProbeConfigCurrent(context.Background(), input))
			req := ticketSessionRequest(t, svc, input)
			_, err := svc.doOpenAICodexTicketProbe(req, input)
			require.ErrorIs(t, err, errOpenAICodexTicketControlsChanged)
			require.False(t, svc.storeOpenAICodexTicket(context.Background(), account, &candidate))
			headers := http.Header{"Session_id": []string{"business-session"}}
			require.NoError(t, svc.applyOpenAICodexTicket(context.Background(), account, previous.Model, headers))
			require.Equal(t, previous.State, headers.Get(openAICodexTurnStateHeader))
			require.Equal(t, "business-session", headers.Get("session_id"))
		})
	}
}

func TestCodexTicketSessionModeSettingRoundTripAndValidation(t *testing.T) {
	svc, repo := newTicketPolicySettings()
	ctx := context.Background()
	cfg, err := svc.GetCodexTicketSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "random", cfg.SessionMode)
	for _, mode := range []string{"account", "account_model", "random"} {
		cfg.SessionMode = mode
		_, err = svc.UpdateCodexTicketSettings(ctx, cfg)
		require.NoError(t, err)
		require.Equal(t, mode, svc.GetOpenAICodexTicketRuntimeConfig(ctx, cfg).SessionMode)
		reloaded, err := NewSettingService(repo, svc.cfg).GetCodexTicketSettings(ctx)
		require.NoError(t, err)
		require.Equal(t, mode, reloaded.SessionMode)
	}
	before := repo.values[SettingKeyCodexTicketPolicy]
	cfg.SessionMode = "unknown"
	_, err = svc.UpdateCodexTicketSettings(ctx, cfg)
	require.Error(t, err)
	require.Equal(t, before, repo.values[SettingKeyCodexTicketPolicy])
}

func TestCodexTicketSessionSnapshotAndLegacyBinding(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{SessionMode: "account"}, nil)
	input := openAICodexTicketProbeInput{Account: ticketTestAccount(41), Token: "test", Model: "gpt-6-astra"}
	accountSession := ticketSessionRequest(t, svc, input).Header.Get("session_id")
	snapshot := config.OpenAICodexTicketConfig{SessionMode: "account_model"}
	input.Config = &snapshot
	modelSession := ticketSessionRequest(t, svc, input).Header.Get("session_id")
	require.NotEqual(t, accountSession, modelSession)
	require.Equal(t, modelSession, ticketSessionRequest(t, svc, input).Header.Get("session_id"))
	snapshot.SessionMode = "random"
	require.NotEqual(t, ticketSessionRequest(t, svc, input).Header.Get("session_id"), ticketSessionRequest(t, svc, input).Header.Get("session_id"))
	cfg := svc.openAICodexTicketConfig()
	cfg.SessionMode, cfg.ProxyFailureThreshold = "", 0
	legacyJSON, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NotContains(t, string(legacyJSON), "session_mode")
	require.NotContains(t, string(legacyJSON), "proxy_failure_threshold")
	legacyBinding := svc.codexTicketBindingForConfig(input.Account, cfg)
	cfg.SessionMode, cfg.ProxyFailureThreshold = "random", 9
	require.Equal(t, legacyBinding, svc.codexTicketBindingForConfig(input.Account, cfg))
	cfg.SessionMode = "account"
	require.NotEqual(t, legacyBinding, svc.codexTicketBindingForConfig(input.Account, cfg))
	var stored map[string]any
	require.NoError(t, json.Unmarshal(legacyJSON, &stored))
	settings, repo := newTicketPolicySettings()
	repo.values[SettingKeyCodexTicketPolicy] = string(legacyJSON)
	reloaded, err := settings.GetCodexTicketSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, "random", reloaded.SessionMode)
	require.Equal(t, 3, reloaded.ProxyFailureThreshold)
}
