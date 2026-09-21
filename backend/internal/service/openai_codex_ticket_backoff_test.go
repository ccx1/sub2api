package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func codexTicketBackoffInput() *openAICodexTicketProbeInput {
	return &openAICodexTicketProbeInput{Account: ticketTestAccount(41), Token: "tok", Model: "gpt-6-astra",
		Attempt: &CodexTicketAttempt{ID: "attempt", StartedAt: time.Now(), Model: "gpt-6-astra", Reason: "ticket_not_matched"}}
}

// 模拟冷却到期，不等待真实的分钟级时间；保留连续失败次数。
func expireCodexTicketBackoff(t *testing.T, svc *OpenAIGatewayService, account *Account, model string) {
	t.Helper()
	key := codexTicketBackoffKey(account, account.GetCredential("access_token"), model)
	raw, ok := svc.openaiCodexTicketBackoff.Load(key)
	require.True(t, ok)
	state := *raw.(*codexTicketBackoffState)
	state.RetryAt = time.Now().Add(-time.Second)
	svc.openaiCodexTicketBackoff.Store(key, &state)
}

func TestCodexTicketBackoffEscalatesUntilSuccess(t *testing.T) {
	svc, input := &OpenAIGatewayService{}, codexTicketBackoffInput()
	svc.cfg = &config.Config{Gateway: config.GatewayConfig{OpenAICodexTicket: config.OpenAICodexTicketConfig{
		RetryMaxAttempts: 0, RetryBackoffSeconds: []int{30, 60, 300, 600, 1200, 1800},
	}}}
	now := time.Now()
	key := codexTicketBackoffKey(input.Account, input.Token, input.Model)
	for i, delay := range []time.Duration{30 * time.Second, time.Minute, 5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 30 * time.Minute, 30 * time.Minute} {
		svc.advanceCodexTicketBackoff(input, now)
		raw, ok := svc.openaiCodexTicketBackoff.Load(key)
		require.True(t, ok)
		state := raw.(*codexTicketBackoffState)
		require.Equal(t, i+1, state.Failures)
		require.Equal(t, now.Add(delay), state.RetryAt)
		now = state.RetryAt.Add(time.Second)
	}
	input.Attempt.Success = true
	svc.advanceCodexTicketBackoff(input, now)
	_, exists := svc.openaiCodexTicketBackoff.Load(key)
	require.False(t, exists)
	input.Attempt.Success = false
	svc.advanceCodexTicketBackoff(input, now)
	raw, _ := svc.openaiCodexTicketBackoff.Load(key)
	require.Equal(t, now.Add(30*time.Second), raw.(*codexTicketBackoffState).RetryAt)
}

func TestCodexTicketBackoffKeepsCooldownWhenProxyChanges(t *testing.T) {
	svc, input := &OpenAIGatewayService{}, codexTicketBackoffInput()
	input.Account.Extra = map[string]any{ProxyModeExtraKey: ProxyModeRandom}
	svc.advanceCodexTicketBackoff(input, time.Now())
	proxyID := int64(19)
	input.Account.ProxyID = &proxyID
	input.Account.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "new.example", Port: 80}
	require.True(t, svc.openAICodexTicketBackoffActive(input.Account, input.Token, input.Model))
	require.False(t, svc.openAICodexTicketBackoffActive(input.Account, input.Token, "gpt-5.6-sol"))
	require.False(t, svc.openAICodexTicketBackoffActive(ticketTestAccount(42), input.Token, input.Model))
	require.False(t, svc.openAICodexTicketBackoffActive(input.Account, "new-token", input.Model))
	require.NotContains(t, codexTicketBackoffKey(input.Account, input.Token, input.Model), "tok")
}

func TestCodexTicketBackoffExpiryKeepsStreakAndCleansOldIdentities(t *testing.T) {
	svc, input := &OpenAIGatewayService{}, codexTicketBackoffInput()
	now := time.Now()
	key := codexTicketBackoffKey(input.Account, input.Token, input.Model)
	svc.advanceCodexTicketBackoff(input, now.Add(-time.Minute))
	require.False(t, svc.openAICodexTicketBackoffActive(input.Account, input.Token, input.Model))
	svc.advanceCodexTicketBackoff(input, now)
	raw, _ := svc.openaiCodexTicketBackoff.Load(key)
	require.Equal(t, 2, raw.(*codexTicketBackoffState).Failures)
	svc.sweepCodexTicketBackoff(now.Add(25 * time.Hour))
	_, exists := svc.openaiCodexTicketBackoff.Load(key)
	require.False(t, exists)
}

func TestCodexTicketBackoffIgnoresControlsCancellationAndUnsentAttempts(t *testing.T) {
	for _, mode := range []string{"unsent", "canceled_response", "canceled_context", "controls", "global_off", "auth_cooldown"} {
		t.Run(mode, func(t *testing.T) {
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
			input := codexTicketBackoffInput()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "unsent":
				input.Attempt.StartedAt = time.Time{}
			case "canceled_response":
				input.Attempt.canceled = true
			case "canceled_context":
				cancel()
			case "controls":
				input.Attempt.Reason = "controls_changed"
			case "global_off":
				svc.cfg.Gateway.OpenAICodexTicket.Enabled = false
			case "auth_cooldown":
				svc.coolOpenAICodexTicket(input.Account, input.Token, &openAICodexTicketProbeRejected{Status: http.StatusTooManyRequests})
			}
			svc.finishOpenAICodexTicketHarvest(ctx, input)
			require.False(t, svc.openAICodexTicketBackoffActive(input.Account, input.Token, input.Model))
		})
	}
}

func TestCodexTicketBackoffSuccessDoesNotClearCredentialCooldown(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	input := codexTicketBackoffInput()
	svc.advanceCodexTicketBackoff(input, time.Now())
	svc.coolOpenAICodexTicket(input.Account, input.Token, &openAICodexTicketProbeRejected{Status: http.StatusTooManyRequests, RetryAfter: "1800"})
	input.Attempt.Success = true
	svc.finishOpenAICodexTicketHarvest(context.Background(), input)
	require.False(t, svc.openAICodexTicketBackoffActive(input.Account, input.Token, input.Model))
	require.True(t, svc.openAICodexTicketCooling(input.Account, input.Token))
}
