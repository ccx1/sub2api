package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketBusinessHoldWSIngressDrainLifetime(t *testing.T) {
	for _, outcome := range []string{"completed", "expired", "upstream failure", "lease loss"} {
		t.Run(outcome, func(t *testing.T) { checkCodexTicketIngressDrainHold(t, outcome) })
	}
}

func newCodexTicketIngressBusinessHoldService(t *testing.T) (*OpenAIGatewayService, *Account, *stagedPassthroughConn) {
	t.Helper()
	cfg := passthroughLifecycleConfig()
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 60
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 60
	cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 60
	cfg.Gateway.OpenAICodexTicket = config.OpenAICodexTicketConfig{Enabled: true}
	upstream := newStagedPassthroughConn()
	pool := newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(&stagedPassthroughDialer{conn: &ingressDrainTestConn{upstream}})
	t.Cleanup(pool.Close)
	svc := newPassthroughLifecycleService(cfg, upstream)
	svc.openaiWSPool = pool
	account := ticketTestAccount(901)
	account.Concurrency = 1
	account.Extra = map[string]any{"openai_oauth_responses_websockets_v2_mode": OpenAIWSIngressModeCtxPool}
	return svc, account, upstream
}

func checkCodexTicketIngressDrainHold(t *testing.T, outcome string) {
	t.Helper()
	svc, account, upstream := newCodexTicketIngressBusinessHoldService(t)
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	results := make(chan *OpenAIForwardResult, 2)
	turnErrors := make(chan error, 2)
	hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, result *OpenAIForwardResult, err error) {
		results <- result
		turnErrors <- err
	}}
	server, done := startPassthroughLifecycleServer(t, controlCtx, svc, account, hooks)
	defer func() { cancelControl(context.Canceled); server.Close() }()
	client := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = client.CloseNow() }()
	requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
	require.Error(t, svc.checkCodexTicketBusinessIdle(context.Background(), account))
	require.NoError(t, client.CloseNow())
	require.Never(t, func() bool {
		return svc.checkCodexTicketBusinessIdle(context.Background(), account) == nil
	}, 60*time.Millisecond, time.Millisecond, "客户端断连不能提前释放仍在排空的业务租约")
	upstreamFailure := errors.New("upstream drain failed")
	switch outcome {
	case "completed":
		upstream.Send(`{"type":"response.completed","response":{"id":"resp_business_drain","model":"gpt-5.1","usage":{"input_tokens":9,"output_tokens":2}}}`)
	case "upstream failure":
		upstream.Fail(upstreamFailure)
	case "lease loss":
		cancelControl(ErrOpenAIWSIngressLeaseLost)
	}
	select {
	case err := <-done:
		checkCodexTicketIngressDrainResult(t, outcome, err, upstreamFailure)
	case <-time.After(4 * time.Second):
		t.Fatal("ingress 未在排空完成或失败后结束")
	}
	require.NoError(t, svc.checkCodexTicketBusinessIdle(context.Background(), account))
	require.Len(t, results, 1, "每轮只结算一次")
	result, turnErr := <-results, <-turnErrors
	checkCodexTicketIngressDrainResult(t, outcome, turnErr, upstreamFailure)
	if outcome == "completed" {
		require.NotNil(t, result)
		require.Equal(t, OpenAIUsage{InputTokens: 9, OutputTokens: 2}, result.Usage)
	} else {
		require.Nil(t, result, "不能凭空生成用量")
	}
}

func checkCodexTicketIngressDrainResult(t *testing.T, outcome string, err, upstreamFailure error) {
	t.Helper()
	switch outcome {
	case "upstream failure":
		require.ErrorIs(t, err, upstreamFailure)
	case "lease loss":
		require.ErrorIs(t, err, ErrOpenAIWSIngressLeaseLost)
	default:
		require.NoError(t, err)
	}
}

type ingressBusinessHoldRenewalLossRepo struct {
	*turnAdmissionRepo
	codexTicketLocalBusinessHolds
	renewing chan struct{}
	fail     chan struct{}
}

func (r *ingressBusinessHoldRenewalLossRepo) RenewCodexTicketBusinessHold(ctx context.Context, _ CodexTicketBusinessLease) error {
	close(r.renewing)
	select {
	case <-r.fail:
		return errors.New("business hold store unavailable")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestCodexTicketBusinessHoldWSIngressRenewalLoss(t *testing.T) {
	for _, phase := range []string{"read_drain", "write_client"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			checkCodexTicketIngressRenewalLoss(t, phase)
		})
	}
}

func checkCodexTicketIngressRenewalLoss(t *testing.T, phase string) {
	t.Helper()
	svc, account, upstream := newCodexTicketIngressBusinessHoldService(t)
	repo := &ingressBusinessHoldRenewalLossRepo{
		turnAdmissionRepo: &turnAdmissionRepo{account: account},
		renewing:          make(chan struct{}),
		fail:              make(chan struct{}),
	}
	svc.accountRepo = repo
	results := make(chan *OpenAIForwardResult, 2)
	turnErrors := make(chan error, 2)
	hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, result *OpenAIForwardResult, err error) {
		results <- result
		turnErrors <- err
	}}
	controlCtx, cancelControl := context.WithCancel(context.Background())
	server, done := startPassthroughLifecycleServer(t, controlCtx, svc, account, hooks)
	defer func() { cancelControl(); server.Close() }()
	client := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = client.CloseNow() }()
	requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
	require.Error(t, svc.checkCodexTicketBusinessIdle(context.Background(), account))
	if phase == "write_client" {
		payload, err := json.Marshal(map[string]string{"type": "response.output_text.delta", "delta": strings.Repeat("x", 16<<20)})
		require.NoError(t, err)
		upstream.Send(string(payload))
	}
	select {
	case <-repo.renewing:
	case <-time.After(codexTicketBusinessHoldTTL/3 + 5*time.Second):
		t.Fatal("业务租约未发起真实续期")
	}
	if phase == "read_drain" {
		require.NoError(t, client.CloseNow())
		require.Never(t, func() bool {
			return svc.checkCodexTicketBusinessIdle(context.Background(), account) == nil
		}, 60*time.Millisecond, time.Millisecond)
	}
	close(repo.fail)
	select {
	case err := <-done:
		require.ErrorIs(t, err, ErrCodexTicketBusinessHoldLost)
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("真实续期失败后 ingress 必须停止上游读取")
	}
	require.NoError(t, svc.checkCodexTicketBusinessIdle(context.Background(), account))
	require.Len(t, results, 1, "失租只能结算一次")
	require.Nil(t, <-results, "失租不能伪造用量")
	require.ErrorIs(t, <-turnErrors, ErrCodexTicketBusinessHoldLost)
	require.Empty(t, upstream.writes, "失租后不能重发上游请求")
}
