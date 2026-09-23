package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type proxyTransportSourceStub struct {
	AccountRepository
	failed   *Proxy
	selected *Proxy
	current  *Account
	err      error
}

func (s *proxyTransportSourceStub) ReportProxyTransportFailure(_ context.Context, _ int64, proxy *Proxy) error {
	s.failed = proxy
	return s.err
}

func (s *proxyTransportSourceStub) ResolveFixedProxyFailover(context.Context, *Account) (*Proxy, error) {
	return s.selected, s.err
}

func (s *proxyTransportSourceStub) GetByID(context.Context, int64) (*Account, error) {
	copy := *s.current
	return &copy, nil
}

func (s *proxyTransportSourceStub) SelectRandomActiveProxy(context.Context) (*Proxy, error) {
	return s.selected, s.err
}

func TestProxyTransportFailureObservesFixedTLSHandshake(t *testing.T) {
	proxy := &Proxy{ID: 70, Status: StatusActive, Protocol: "http", Host: "proxy.test", Port: 1333}
	account := &Account{ID: 663, ProxyID: &proxy.ID, Proxy: proxy}
	req, err := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	require.NoError(t, err)
	source := &proxyTransportSourceStub{}
	cause := fmt.Errorf("TLS handshake failed: %w", io.ErrUnexpectedEOF)
	require.True(t, observeRandomProxyHTTPResult(req, account, source, nil, cause))
	require.Same(t, proxy, source.failed)
}

func TestProxyTransportCooldownIgnoresBusinessAndCancellation(t *testing.T) {
	proxy := &Proxy{ID: 70}
	account := &Account{ID: 663, ProxyID: &proxy.ID, Proxy: proxy}
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, errors.New("upstream status 401"), errors.New("upstream status 429"), errors.New("invalid TLS profile"), io.ErrUnexpectedEOF} {
		source := &proxyTransportSourceStub{}
		require.False(t, ReportRandomProxyTransportFailure(context.Background(), account, source, cause))
		require.Nil(t, source.failed)
	}
}

func TestProxyTransportFixedFallbackAndReuse(t *testing.T) {
	old := &Proxy{ID: 70, Status: StatusActive, Protocol: "http", Host: "old.test", Port: 1333}
	next := &Proxy{ID: 58, Status: StatusActive, Protocol: "http", Host: "next.test", Port: 641}
	account := &Account{ID: 663, Status: StatusActive, Schedulable: true, ProxyID: &old.ID, Proxy: old}
	source := &proxyTransportSourceStub{selected: next, current: account}
	bound := *account
	require.NoError(t, ResolveRandomProxyFromSource(context.Background(), account, source))
	require.Equal(t, next.ID, *account.ProxyID)
	require.Nil(t, account.Extra, "临时切换不能将固定账号改为随机模式")
	require.ErrorIs(t, ValidateRandomProxyForReuse(context.Background(), &bound, source), ErrRandomProxyChanged)
	require.NoError(t, ValidateRandomProxyForReuse(context.Background(), account, source))
	source.selected = nil
	require.ErrorIs(t, ResolveRandomProxyFromSource(context.Background(), account, source), ErrRandomProxyUnavailable)
}

func TestProxyTransportAccountTestReportsWithoutReplaying(t *testing.T) {
	proxy := &Proxy{ID: 70, Status: StatusActive, Protocol: "http", Host: "proxy.test", Port: 1333}
	account := &Account{ID: 663, ProxyID: &proxy.ID, Proxy: proxy}
	cause := fmt.Errorf("TLS handshake failed: %w", io.ErrUnexpectedEOF)
	upstream := &queuedHTTPUpstreamStub{errors: []error{cause}}
	source := &proxyTransportSourceStub{}
	svc := &AccountTestService{accountRepo: source, httpUpstream: upstream}
	req, err := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	require.NoError(t, err)
	_, err = svc.doOpenAIAccountTestUpstream(req, proxy.URL(), account, false)
	require.ErrorIs(t, err, cause)
	require.Equal(t, 1, upstream.callCount)
	require.Same(t, proxy, source.failed)
}

func TestProxyTransportTicketReportsActualHarvestAndBusinessExit(t *testing.T) {
	harvest := &Proxy{ID: 70, Protocol: "http", Host: "harvest.test", Port: 1333}
	business := &Proxy{ID: 58, Protocol: "http", Host: "business.test", Port: 641}
	account := &Account{ID: 663, ProxyID: &business.ID, Proxy: business}
	source := &proxyTransportSourceStub{}
	svc := &OpenAIGatewayService{accountRepo: source}
	ctx := context.WithValue(context.Background(), codexTicketScheduleKey{}, &codexTicketSchedule{
		reservation: &CodexTicketReservation{Proxy: harvest},
	})
	cause := fmt.Errorf("TLS handshake failed: %w", io.ErrUnexpectedEOF)
	svc.reportCodexProbeConnectionFailure(ctx, openAICodexTicketProbeInput{Account: account, ProxyURL: harvest.URL()}, cause)
	require.Same(t, harvest, source.failed)
	svc.reportCodexProbeConnectionFailure(ctx, openAICodexTicketProbeInput{Account: account, ProxyURL: business.URL(), State: "ticket"}, cause)
	require.Same(t, business, source.failed)
	source.failed = nil
	svc.reportCodexProbeConnectionFailure(ctx, openAICodexTicketProbeInput{Account: account, ProxyURL: "http://changed.test:80"}, cause)
	require.Nil(t, source.failed, "地址不匹配时不能把故障归给另一条出口")
}
