package service

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

type codexTicketPoolDialer struct {
	mu      sync.Mutex
	headers []http.Header
}

func (d *codexTicketPoolDialer) Dial(_ context.Context, _ string, h http.Header, _ string) (openAIWSClientConn, int, http.Header, error) {
	d.mu.Lock()
	d.headers = append(d.headers, h.Clone())
	d.mu.Unlock()
	return &openAIWSFakeConn{}, http.StatusSwitchingProtocols, nil, nil
}

func codexTicketPoolFixture(t *testing.T) (*OpenAIGatewayService, *Account, *openAIWSConnPool, *codexTicketPoolDialer) {
	t.Helper()
	s, account, _ := codexTicketWSFixture(t)
	s.cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	s.cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	s.cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	pool := newOpenAIWSConnPool(s.cfg)
	t.Cleanup(pool.Close)
	dialer := &codexTicketPoolDialer{}
	pool.setClientDialerForTest(dialer)
	return s, account, pool, dialer
}

func codexTicketPoolRequest(t *testing.T, s *OpenAIGatewayService, account *Account) openAIWSAcquireRequest {
	t.Helper()
	h := http.Header{}
	snapshot, err := s.applyOpenAICodexTicketSnapshot(context.Background(), account, "gpt-6-astra", h)
	require.NoError(t, err)
	return openAIWSAcquireRequest{Account: account, WSURL: "wss://example.invalid/responses", Headers: h,
		CodexTicketReceipt: codexTicketWSReceiptFromSnapshot(snapshot)}
}

func TestCodexTicketWSPoolRenewalReplacesOldHandshake(t *testing.T) {
	s, account, pool, dialer := codexTicketPoolFixture(t)
	firstReq := codexTicketPoolRequest(t, s, account)
	first, err := pool.Acquire(context.Background(), firstReq)
	require.NoError(t, err)
	firstID := first.ConnID()
	first.Release()
	old := s.lookupOpenAICodexTicket(account, "gpt-6-astra")
	s.invalidateOpenAICodexTicket(context.Background(), account, old)
	next := *old
	next.State, next.CapturedAt = "gAAAAA"+strings.Repeat("N", 286), time.Now()
	require.True(t, s.storeOpenAICodexTicket(context.Background(), account, &next))
	req := codexTicketPoolRequest(t, s, account)
	req.PreferredConnID = firstID
	second, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	defer second.Release()
	require.False(t, second.Reused(), "换票后不得沿用旧票握手")
	require.NotEqual(t, firstID, second.ConnID())
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	require.Len(t, dialer.headers, 2)
	require.Equal(t, next.State, dialer.headers[1].Get(openAICodexTurnStateHeader))
}

func TestCodexTicketWSPoolSameTicketRetainsReceipt(t *testing.T) {
	s, account, pool, _ := codexTicketPoolFixture(t)
	req := codexTicketPoolRequest(t, s, account)
	first, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	first.Release()
	second, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	defer second.Release()
	require.True(t, second.Reused())
	pending := req.CodexTicketReceipt
	confirmed := confirmedOpenAICodexTicketWSReceipt(pending, second)
	require.NotNil(t, confirmed, "同票复用应继续观察实际握手票据")
	watch := confirmed.watch(context.Background(), s, "gpt-6-astra")
	require.NotNil(t, watch)
	watch.invalidate = func() { s.invalidateOpenAICodexTicket(context.Background(), confirmed.account, &confirmed.ticket) }
	watch.observe([]byte(`{"type":"response.completed","response":{"status":"completed","model":"gpt-other"}}`))
	require.Nil(t, s.lookupOpenAICodexTicket(account, "gpt-6-astra"))
}

func TestCodexTicketWSPoolStrictPreferredRejectsChangedTicket(t *testing.T) {
	s, account, pool, _ := codexTicketPoolFixture(t)
	req := codexTicketPoolRequest(t, s, account)
	first, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	first.Release()
	next := req.CodexTicketReceipt.ticket
	next.State, next.CapturedAt = "gAAAAA"+strings.Repeat("N", 286), time.Now()
	require.True(t, s.storeOpenAICodexTicket(context.Background(), account, &next))
	req = codexTicketPoolRequest(t, s, account)
	req.PreferredConnID, req.ForcePreferredConn = first.ConnID(), true
	lease, err := pool.Acquire(context.Background(), req)
	require.ErrorIs(t, err, errOpenAIWSPreferredConnUnavailable)
	require.Nil(t, lease)
}

func TestCodexTicketWSPoolPrewarmPreservesActualReceipt(t *testing.T) {
	s, account, pool, _ := codexTicketPoolFixture(t)
	req := codexTicketPoolRequest(t, s, account)
	ap := pool.getOrCreateAccountPool(account.ID)
	ap.mu.Lock()
	ap.lastAcquire, ap.creating = &req, 1
	ap.mu.Unlock()
	pool.prewarmConns(account.ID, req, 1)
	lease, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	defer lease.Release()
	require.True(t, lease.Reused())
	receipt := confirmedOpenAICodexTicketWSReceipt(nil, lease)
	require.NotNil(t, receipt)
	require.Equal(t, req.CodexTicketReceipt.identity(), receipt.identity())
	next := req.CodexTicketReceipt.ticket
	next.CapturedAt = time.Now()
	require.True(t, s.storeOpenAICodexTicket(context.Background(), account, &next))
	require.False(t, sameOpenAIWSPrewarmTarget(req, codexTicketPoolRequest(t, s, account)))
}

func TestCodexTicketWSPoolUnmanagedStateCannotClaimNativeReceipt(t *testing.T) {
	s, account, pool, _ := codexTicketPoolFixture(t)
	req := codexTicketPoolRequest(t, s, account)
	req.CodexTicketReceipt = nil
	first, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	require.Nil(t, confirmedOpenAICodexTicketWSReceipt(nil, first))
	first.Release()
	nativeReq := codexTicketPoolRequest(t, s, account)
	second, err := pool.Acquire(context.Background(), nativeReq)
	require.NoError(t, err)
	defer second.Release()
	require.False(t, second.Reused(), "相同原始 STATE 不能替代原生握手证明")
}

func TestCodexTicketWSPoolChangedDialHeaderRejected(t *testing.T) {
	s, account, pool, dialer := codexTicketPoolFixture(t)
	req := codexTicketPoolRequest(t, s, account)
	req.HeadersFactory = func(_ context.Context, h http.Header) (http.Header, error) {
		h.Set(openAICodexTurnStateHeader, "client-owned-state")
		return h, nil
	}
	lease, err := pool.Acquire(context.Background(), req)
	require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
	require.Nil(t, lease)
	require.Empty(t, dialer.headers, "实际拨号头被改写时不得记录错误receipt")
}

func TestCodexTicketWSFramesRenewalRequiresReconnectBeforeSending(t *testing.T) {
	s, account, receipt := codexTicketWSFixture(t)
	inner := &codexTicketWSFrames{frames: make(chan []byte)}
	conn := s.observeOpenAICodexTicketWSFrames(context.Background(), inner, receipt)
	next := receipt.ticket
	next.State, next.CapturedAt = "gAAAAA"+strings.Repeat("N", 286), time.Now()
	require.True(t, s.storeOpenAICodexTicket(context.Background(), account, &next))
	for _, kind := range []coderws.MessageType{coderws.MessageText, coderws.MessageBinary} {
		err := conn.WriteFrame(context.Background(), kind, []byte(`{"type":"response.create","model":"gpt-6-astra"}`))
		var closeErr *OpenAIWSClientCloseError
		require.ErrorAs(t, err, &closeErr)
	}
	require.Empty(t, inner.written, "新票发布后不得把新请求写入旧握手连接")
	require.Equal(t, next.State, s.lookupOpenAICodexTicket(account, next.Model).State)
}

func TestCodexTicketWSRefreshRemovesRevokedNativeHeaderOnFailOpen(t *testing.T) {
	s, account, _, _ := codexTicketPoolFixture(t)
	req := codexTicketPoolRequest(t, s, account)
	s.cfg.Gateway.OpenAICodexTicket.FailClosed = false
	s.invalidateOpenAICodexTicket(context.Background(), account, &req.CodexTicketReceipt.ticket)
	require.NoError(t, s.refreshOpenAICodexTicketWSHeaders(context.Background(), account, "gpt-6-astra", &req))
	require.Nil(t, req.CodexTicketReceipt)
	require.Empty(t, req.Headers.Get(openAICodexTurnStateHeader))
}
