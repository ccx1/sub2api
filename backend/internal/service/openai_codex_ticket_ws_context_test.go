package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

type codexTicketCancelCache struct {
	SchedulerCache
	cancel context.CancelFunc
	calls  int
}

func (c *codexTicketCancelCache) GetAccount(ctx context.Context, _ int64) (*Account, error) {
	c.calls++
	c.cancel()
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestCodexTicketSchedulingCancellationStopsHydration(t *testing.T) {
	s, account, _ := codexTicketWSFixture(t)
	account.Credentials = nil
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cache := &codexTicketCancelCache{cancel: cancel}
	s.schedulerSnapshot = NewSchedulerSnapshotService(cache, nil, nil, nil, nil)
	scheduler := &defaultOpenAIAccountScheduler{service: s}
	started := time.Now()
	for range 3 {
		compatible, reason := scheduler.isAccountRequestCompatibleReason(ctx, account,
			OpenAIAccountScheduleRequest{RequestedModel: "gpt-6-astra"})
		require.False(t, compatible)
		require.Equal(t, "runtime_blocked", reason)
	}
	require.Equal(t, 1, cache.calls, "取消后不得为后续候选再补读快照")
	require.Less(t, time.Since(started), time.Second, "调度取消不能被每个候选独立的 2 秒超时放大")
}

func TestCodexTicketWSPrewarmObservesCompleteResponse(t *testing.T) {
	for _, scenario := range []struct {
		name, terminal string
		prefix         string
		reject         bool
	}{
		{"mismatch", `{"type":"response.completed","response":{"status":"completed","model":"gpt-other"}}`, "", true},
		{"healthy", `{"type":"response.completed","response":{"status":"completed","model":"gpt-6-astra"}}`, "", false},
		{"unknown model", `{"type":"response.completed","response":{"status":"completed"}}`, "", false},
		{"failed stream", `{"type":"response.completed","response":{"status":"completed","model":"gpt-other"}}`, `{"type":"response.in_progress","response":{"error":{"message":"failed"}}}`, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			s, account, receipt := codexTicketWSFixture(t)
			s.cfg.Gateway.OpenAIWS.PrewarmGenerateEnabled = true
			writes := captureTicketInvalidations(s)
			inner := &openAIWSCaptureConn{}
			if scenario.prefix != "" {
				inner.events = append(inner.events, []byte(scenario.prefix))
			}
			inner.events = append(inner.events, []byte(scenario.terminal))
			pool := newOpenAIWSConnPool(s.cfg)
			t.Cleanup(pool.Close)
			conn := newOpenAIWSConn("prewarm_ticket", account.ID, inner, nil)
			conn.codexTicketReceipt = receipt
			pool.getOrCreateAccountPool(account.ID).conns[conn.id] = conn
			lease := &openAIWSConnLease{conn: conn, pool: pool, accountID: account.ID}
			payload := map[string]any{"type": "response.create", "model": "gpt-6-astra"}
			err := s.performOpenAIWSGeneratePrewarm(context.Background(), lease,
				OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2},
				payload, "", payload, account, nil, 0)
			require.Len(t, inner.writes, 1)
			require.Equal(t, false, inner.writes[0]["generate"])
			if scenario.reject {
				require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
				event := awaitTicketInvalidation(t, writes).Invalidation
				require.Equal(t, "websocket_prewarm", event.Source)
				require.Equal(t, "response_model_mismatch", event.Reason)
				require.Equal(t, []string{"gpt-other"}, event.ReportedModels)
				require.False(t, lease.IsPrewarmed())
				require.Nil(t, s.lookupOpenAICodexTicket(account, "gpt-6-astra"))
				require.Error(t, lease.WriteJSONWithContextTimeout(context.Background(), payload, time.Second), "撤票后不能向旧握手写入本次业务")
				return
			}
			require.NoError(t, err)
			require.Empty(t, writes, "successful or incomplete prewarm must not record revocation")
			require.True(t, lease.IsPrewarmed())
			require.NotNil(t, s.lookupOpenAICodexTicket(account, "gpt-6-astra"))
		})
	}
}

type codexTicketWSLatestRepo struct {
	AccountRepository
	latest *Account
}

func (r *codexTicketWSLatestRepo) GetCodexTicketAccountSnapshot(context.Context, int64) (*Account, error) {
	return cloneOpenAICodexTicketAccount(r.latest), nil
}

func TestCodexTicketWSLatestAccountRejectsChangedConfiguration(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*Account)
	}{
		{"fixed proxy", func(a *Account) {
			id := int64(15)
			a.ProxyID = &id
			a.Proxy = &Proxy{ID: id, Protocol: "http", Host: "127.0.0.1", Port: 8080}
		}},
		{"tls", func(a *Account) { a.Extra["enable_tls_fingerprint"] = true }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			s, account, receipt := codexTicketWSFixture(t)
			latest := cloneOpenAICodexTicketAccount(account)
			latest.Status = StatusActive
			scenario.change(latest)
			s.accountRepo = &codexTicketWSLatestRepo{latest: latest}
			req := openAIWSAcquireRequest{Account: account, Headers: make(http.Header), CodexTicketReceipt: receipt}
			req.Headers.Set(openAICodexTurnStateHeader, receipt.ticket.State)
			require.ErrorIs(t, s.refreshOpenAICodexTicketWSHeaders(context.Background(), account, "gpt-6-astra", &req), ErrOpenAICodexTicketUnavailable)
			require.Empty(t, req.Headers.Get(openAICodexTurnStateHeader), "配置已变化时不能继续使用旧握手票据")
			inner := &codexTicketWSFrames{}
			wrapped := s.observeOpenAICodexTicketWSFrames(context.Background(), inner, receipt, account)
			err := wrapped.WriteFrame(context.Background(), coderws.MessageText,
				[]byte(`{"type":"response.create","model":"gpt-6-astra"}`))
			require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
			require.Empty(t, inner.written)
			require.NotEqual(t, openAICodexTicketAccountBinding(latest), openAICodexTicketAccountBinding(account))
		})
	}
}

func TestCodexTicketWSLatestAccountKeepsHealthySessionAfterTokenRefresh(t *testing.T) {
	s, account, receipt := codexTicketWSFixture(t)
	latest := cloneOpenAICodexTicketAccount(account)
	latest.Status = StatusActive
	latest.Credentials["access_token"] = "refreshed-token"
	s.accountRepo = &codexTicketWSLatestRepo{latest: latest}
	req := openAIWSAcquireRequest{Account: account, Headers: make(http.Header), CodexTicketReceipt: receipt}
	req.Headers.Set(openAICodexTurnStateHeader, receipt.ticket.State)
	require.NoError(t, s.refreshOpenAICodexTicketWSHeaders(context.Background(), account, "gpt-6-astra", &req))
	require.Equal(t, receipt.identity(), req.CodexTicketReceipt.identity())
	require.Equal(t, "refreshed-token", req.CodexTicketReceipt.account.GetCredential("access_token"))
	require.Equal(t, "tok", account.GetCredential("access_token"), "旧 WS 握手账号快照不可被覆盖")
	inner := &codexTicketWSFrames{}
	wrapped := s.observeOpenAICodexTicketWSFrames(context.Background(), inner, receipt, account)
	require.NoError(t, wrapped.WriteFrame(context.Background(), coderws.MessageText,
		[]byte(`{"type":"response.create","model":"gpt-6-astra"}`)))
	require.Len(t, inner.written, 1)
}

func TestCodexTicketWSInitialTurnKeepsInjectedSnapshotAfterPolicyChange(t *testing.T) {
	s, account, receipt := codexTicketWSFixture(t)
	req := openAIWSAcquireRequest{
		Account:            account,
		Headers:            make(http.Header),
		CodexTicketReceipt: receipt,
	}
	req.Headers.Set(openAICodexTurnStateHeader, receipt.ticket.State)

	// Simulate a settings update while the first handshake is waiting for a
	// connection slot. The first attempt must retain the already injected pair.
	s.cfg.Gateway.OpenAICodexTicket.Enabled = false
	require.NoError(t, s.refreshOpenAICodexTicketWSHeadersForTurn(
		context.Background(), account, receipt.ticket.Model, 1, 0, &req,
	))
	require.Equal(t, receipt.ticket.State, req.Headers.Get(openAICodexTurnStateHeader))
	require.Equal(t, receipt.identity(), req.CodexTicketReceipt.identity())

	// An explicit retry is a new attempt and may refresh both values together.
	require.NoError(t, s.refreshOpenAICodexTicketWSHeadersForTurn(
		context.Background(), account, receipt.ticket.Model, 1, 1, &req,
	))
	require.Nil(t, req.CodexTicketReceipt)
	require.Empty(t, req.Headers.Get(openAICodexTurnStateHeader))
}
