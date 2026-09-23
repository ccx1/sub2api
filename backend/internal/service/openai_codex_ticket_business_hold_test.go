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

func TestCodexTicketBusinessHoldHTTPBodyLifetime(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	account := ticketTestAccount(7)
	_, release1, err := svc.beginCodexTicketBusinessHold(context.Background(), account)
	require.NoError(t, err)
	_, release2, err := svc.beginCodexTicketBusinessHold(context.Background(), account)
	require.NoError(t, err)
	defer release1()
	defer release2()
	response := &http.Response{Body: io.NopCloser(strings.NewReader("stream"))}
	attachCodexTicketBusinessHold(response, nil, release1)
	require.Error(t, svc.checkCodexTicketBusinessIdle(context.Background(), account))
	_, err = io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	release1()
	require.Error(t, svc.checkCodexTicketBusinessIdle(context.Background(), account), "second request still holds")
	release2()
	require.NoError(t, svc.checkCodexTicketBusinessIdle(context.Background(), account))
}

type codexBusinessBrokenBody struct{}

func (codexBusinessBrokenBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (codexBusinessBrokenBody) Close() error             { return errors.New("close failed") }

func TestCodexTicketBusinessHoldErrorsAndProbeIsolation(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	account := ticketTestAccount(7)
	for _, failure := range []string{"read", "close", "nil_body", "transport"} {
		t.Run(failure, func(t *testing.T) {
			_, release, err := svc.beginCodexTicketBusinessHold(context.Background(), account)
			require.NoError(t, err)
			response := &http.Response{Body: codexBusinessBrokenBody{}}
			var transportErr error
			if failure == "nil_body" {
				response.Body = nil
			}
			if failure == "transport" {
				transportErr = io.ErrUnexpectedEOF
			}
			attachCodexTicketBusinessHold(response, transportErr, release)
			if failure == "read" {
				_, _ = response.Body.Read(make([]byte, 1))
			}
			if failure == "close" {
				_ = response.Body.Close()
			}
			require.NoError(t, svc.checkCodexTicketBusinessIdle(context.Background(), account))
		})
	}
	_, release, err := svc.beginCodexTicketBusinessHold(withCodexTicketHarvestProbe(context.Background()), account)
	require.NoError(t, err)
	defer release()
	require.NoError(t, svc.checkCodexTicketBusinessIdle(context.Background(), account))
}

func TestCodexTicketBusinessHoldRenewalAndCancellation(t *testing.T) {
	store := &codexTicketLocalBusinessHolds{}
	lease := CodexTicketBusinessLease{AccountID: 7, Token: "test", TTL: 200 * time.Millisecond}
	ctx, release, err := startCodexTicketBusinessLease(context.Background(), store, lease)
	require.NoError(t, err)
	defer release()
	initialUntil, err := store.CodexTicketBusinessHoldUntil(context.Background(), 7)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		until, _ := store.CodexTicketBusinessHoldUntil(context.Background(), 7)
		return until.After(initialUntil.Add(50 * time.Millisecond))
	}, time.Second, 20*time.Millisecond)
	require.NoError(t, store.ReleaseCodexTicketBusinessHold(context.Background(), lease))
	require.Eventually(t, func() bool { return ctx.Err() != nil }, time.Second, 10*time.Millisecond)
	require.ErrorIs(t, context.Cause(ctx), ErrCodexTicketBusinessHoldLost)
}

func TestCodexTicketBusinessHoldActualHTTPPath(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("stream"))}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, upstream)
	account := ticketTestAccount(7)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.invalid/v1/responses", strings.NewReader(`{"model":"gpt-6-astra"}`))
	require.NoError(t, err)
	response, err := svc.doOpenAIUpstream(request, "", account)
	require.NoError(t, err)
	require.Error(t, svc.checkCodexTicketBusinessIdle(context.Background(), account), "headers alone cannot release")
	_, err = io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, svc.checkCodexTicketBusinessIdle(context.Background(), account))
	require.NoError(t, response.Body.Close())
}

func TestCodexTicketBusinessHoldSkipsDisabledAccounts(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	for _, account := range []*Account{
		{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{OpenAICodexTicketEnabledExtraKey: false}},
	} {
		_, release, err := svc.beginCodexTicketBusinessHold(context.Background(), account)
		require.NoError(t, err)
		require.NoError(t, svc.checkCodexTicketBusinessIdle(context.Background(), account))
		release()
	}
}

func TestCodexTicketBusinessHoldWSTurnsReleaseBetweenTurns(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	hold := &codexTicketBusinessTurnHold{ctx: ctx, cancel: cancel, service: svc, account: ticketTestAccount(7)}
	defer hold.close()
	for range 2 {
		require.NoError(t, hold.begin())
		require.Error(t, svc.checkCodexTicketBusinessIdle(ctx, hold.account))
		hold.finish()
		hold.finish()
		require.NoError(t, svc.checkCodexTicketBusinessIdle(ctx, hold.account))
		require.NoError(t, ctx.Err(), "idle socket stays usable")
	}
	require.NoError(t, hold.begin())
	cancel()
	require.Eventually(t, func() bool {
		return svc.checkCodexTicketBusinessIdle(context.Background(), hold.account) == nil
	}, time.Second, 10*time.Millisecond)
}

func TestCodexTicketBusinessHoldDrainLeaseLossCancelsRequest(t *testing.T) {
	for _, disconnected := range []bool{false, true} {
		clientCtx, cancelClient := context.WithCancel(context.Background())
		store := &codexTicketLocalBusinessHolds{}
		lease := CodexTicketBusinessLease{AccountID: 7, Token: "drain", TTL: 90 * time.Millisecond}
		holdCtx, release, err := startCodexTicketBusinessLease(context.WithoutCancel(clientCtx), store, lease)
		require.NoError(t, err)
		requestCtx, cleanup := codexTicketBusinessRequestContext(clientCtx, holdCtx)
		if disconnected {
			cancelClient()
			require.NoError(t, holdCtx.Err(), "client cancellation cannot end upstream drain")
		}
		require.NoError(t, store.ReleaseCodexTicketBusinessHold(context.Background(), lease))
		require.Eventually(t, func() bool { return holdCtx.Err() != nil && requestCtx.Err() != nil }, time.Second, time.Millisecond)
		require.ErrorIs(t, context.Cause(holdCtx), ErrCodexTicketBusinessHoldLost)
		if !disconnected {
			require.ErrorIs(t, context.Cause(requestCtx), ErrCodexTicketBusinessHoldLost)
		}
		cleanup()
		release()
		cancelClient()
	}
}
