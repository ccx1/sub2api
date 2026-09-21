package service

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketConfiguredRetryExhaustionRestartsAfterCooldown(t *testing.T) {
	cfg := config.OpenAICodexTicketConfig{RetryBackoffSeconds: []int{12, 25}, RetryMaxAttempts: 3, RetryExhaustedCooldownSeconds: 400}
	svc := ticketTestService(t, cfg, nil)
	input, now := codexTicketBackoffInput(), time.Now()
	key := codexTicketBackoffKey(input.Account, input.Token, input.Model)
	for i, seconds := range []int{12, 25, 400, 12} {
		svc.advanceCodexTicketBackoff(input, now)
		raw, _ := svc.openaiCodexTicketBackoff.Load(key)
		state := raw.(*codexTicketBackoffState)
		require.Equal(t, now.Add(time.Duration(seconds)*time.Second), state.RetryAt)
		require.Equal(t, i == 2, state.Exhausted)
		require.Equal(t, i%3+1, state.Failures)
		now = state.RetryAt.Add(time.Second)
	}
	input.Attempt.Success = true
	svc.advanceCodexTicketBackoff(input, now)
	_, exists := svc.openaiCodexTicketBackoff.Load(key)
	require.False(t, exists)
}

func TestCodexTicketUnlimitedRetryUsesLastConfiguredDelay(t *testing.T) {
	cfg := config.OpenAICodexTicketConfig{RetryBackoffSeconds: []int{10, 40}, RetryMaxAttempts: 0}
	svc := ticketTestService(t, cfg, nil)
	input, now := codexTicketBackoffInput(), time.Now()
	for i := 1; i <= 12; i++ {
		svc.advanceCodexTicketBackoff(input, now)
		raw, _ := svc.openaiCodexTicketBackoff.Load(codexTicketBackoffKey(input.Account, input.Token, input.Model))
		state := raw.(*codexTicketBackoffState)
		seconds := 40
		if i == 1 {
			seconds = 10
		}
		require.Equal(t, now.Add(time.Duration(seconds)*time.Second), state.RetryAt)
		require.Equal(t, i, state.Failures)
		require.False(t, state.Exhausted)
		now = state.RetryAt.Add(time.Second)
	}
}

func TestCodexTicketConfiguredAuthAndRateLimitCooldown(t *testing.T) {
	for _, status := range []int{401, 403, 429} {
		for _, respect := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d_%t", status, respect), func(t *testing.T) {
				cfg := config.OpenAICodexTicketConfig{RetryBackoffSeconds: []int{10}, AuthCooldownSeconds: 20, RateLimitCooldownSeconds: 45, RespectRetryAfter: respect}
				svc, account := ticketTestService(t, cfg, nil), ticketTestAccount(41)
				before := time.Now()
				svc.coolOpenAICodexTicket(account, "tok", &openAICodexTicketProbeRejected{Status: status, RetryAfter: "100"})
				seconds := 20
				if status == http.StatusTooManyRequests {
					seconds = 45
				}
				if respect {
					seconds = 100
				}
				raw, exists := svc.openaiCodexTicketCooldown.Load(openAICodexTicketCooldownKey(account, "tok"))
				require.True(t, exists)
				require.WithinDuration(t, before.Add(time.Duration(seconds)*time.Second), raw.(time.Time), time.Second)
			})
		}
	}
}

func TestCodexTicketConfiguredHarvestConcurrency(t *testing.T) {
	svc, upstream := codexTicketBoundedService(t, 8)
	svc.cfg.Gateway.OpenAICodexTicket.HarvestConcurrency = 2
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { svc.refreshOpenAICodexTickets(ctx); close(done) }()
	for batch := 0; batch < 4; batch++ {
		awaitCodexTicketProbeSignals(t, upstream.entered, 2)
		require.EqualValues(t, 2, upstream.active.Load())
		upstream.permit <- struct{}{}
		upstream.permit <- struct{}{}
	}
	awaitCodexTicketProbeSignals(t, done, 1)
	require.EqualValues(t, 2, upstream.peak.Load())
	require.EqualValues(t, 8, upstream.calls.Load())
}
