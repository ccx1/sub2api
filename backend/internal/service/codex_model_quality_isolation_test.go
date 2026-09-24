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
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type qualityIsolationRepo struct {
	*qualityRuntimeRepo
	proxy   *Proxy
	reports int
}

func (r *qualityIsolationRepo) GetCodexTicketProxy(context.Context, int64) (*Proxy, error) {
	return r.proxy, nil
}

func (r *qualityIsolationRepo) ReportProxyTransportFailure(context.Context, int64, *Proxy) error {
	r.reports++
	return nil
}

func TestCodexModelQualityTransportFailureCannotChangeProxyHealth(t *testing.T) {
	s, repo, previous := qualityRuntimeFixture(t)
	proxy := &Proxy{ID: 9, Protocol: "http", Host: "quality.test", Port: 8080, Status: StatusActive}
	r := &qualityIsolationRepo{qualityRuntimeRepo: repo, proxy: proxy}
	r.account.Proxy, r.account.ProxyID = proxy, &proxy.ID
	s.accountRepo = r
	previous.ticket.Egress = openAICodexTicketEgress(proxy.URL())
	s.openaiCodexTickets.Store(openAICodexTicketKey(r.account.ID, previous.ticket.Model), previous.ticket)
	job, reason := s.prepareCodexModelQuality(context.Background(), r.account, previous.ticket.Model, previous.policy)
	require.NotNil(t, job, reason)
	s.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		return nil, errors.New("proxyconnect tcp: connection reset by peer")
	}}
	_, err := s.requestCodexModelQuality(context.Background(), job, "test")
	require.ErrorContains(t, err, "network_error")
	require.Zero(t, r.reports)
	require.Zero(t, r.writes)
	require.False(t, s.openAICodexTicketCooling(job.account, job.account.GetCredential("access_token")))
}

func TestCodexModelQualityFailedQuarantineIsNotReportedAsIsolated(t *testing.T) {
	for _, fail := range []error{nil, errors.New("database unavailable")} {
		s, r, job := qualityRuntimeFixture(t)
		r.casHook = func(context.Context) (bool, error) { return false, fail }
		status := qualityStatusForJob(job)
		status.Status, status.Reason = "suspect", "model_mismatch"
		s.finishCodexModelQuality(job, r, status, time.Now())
		if fail == nil {
			require.Equal(t, "stale", r.record.Status.Status)
			require.Equal(t, "stale", r.record.Status.Reason)
		} else {
			require.Equal(t, "suspect", r.record.Status.Status)
			require.Equal(t, "quarantine_persist_failed", r.record.Status.Reason)
		}
		require.False(t, s.codexTicketRevoked(openAICodexTicketKey(job.account.ID, job.ticket.Model), job.ticket))
		require.Equal(t, 1, r.writes)
	}
}

func TestCodexModelQualityFinalFenceRejectsChangedAccount(t *testing.T) {
	s, r, job := qualityRuntimeFixture(t)
	reads := 0
	r.getHook = func(context.Context) {
		reads++
		if reads == 3 {
			r.account.Status = StatusDisabled
		}
	}
	status := qualityStatusForJob(job)
	status.Status, status.Reason = "suspect", "model_mismatch"
	s.finishCodexModelQuality(job, r, status, time.Now())
	require.Equal(t, "stale", r.record.Status.Status)
	require.Zero(t, r.writes)
	require.False(t, s.codexTicketRevoked(openAICodexTicketKey(job.account.ID, job.ticket.Model), job.ticket))
}

func TestCodexModelQualityExpiredTicketCannotBeQuarantined(t *testing.T) {
	s, r, job := qualityRuntimeFixture(t)
	job.ticket.ExpiresAt = time.Now().Add(-time.Second)
	status := qualityStatusForJob(job)
	status.Status, status.Reason = "suspect", "model_mismatch"
	s.finishCodexModelQuality(job, r, status, time.Now())
	require.Zero(t, r.writes)
	require.Nil(t, r.record)
	require.False(t, s.codexTicketRevoked(openAICodexTicketKey(job.account.ID, job.ticket.Model), job.ticket))
}

type qualityTLSUpstream struct {
	HTTPUpstream
	t     *testing.T
	calls int
}

func (u *qualityTLSUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	u.calls++
	require.NotNil(u.t, profile)
	require.Equal(u.t, "session=original", req.Header.Get("Cookie"))
	require.Equal(u.t, "quality-session", req.Header.Get("session_id"))
	return &http.Response{StatusCode: 200, Header: http.Header{"Set-Cookie": {"session=replacement; Max-Age=9999"}},
		Body: io.NopCloser(strings.NewReader(qualityResponseBody("gpt-6-astra", "{}")))}, nil
}

func TestCodexModelQualityCookieAndTLSStayPinnedAcrossRequests(t *testing.T) {
	s, r, initial := qualityRuntimeFixture(t)
	s.cfg.Gateway.TLSFingerprint.Enabled = true
	s.cfg.Gateway.OpenAICodexTicket.CredentialMode = config.CodexTicketCredentialCookie
	s.cfg.Gateway.OpenAICodexTicket.CookieTTLSeconds = 300
	s.settingService.codexTicketSettingsCache.Store(nil)
	r.account.Extra[AntiDegradationExtraKey] = true
	r.account.Extra["tls_fingerprint_builtin"] = "nodejs24"
	ticket := codexTicketLeaf(initial.ticket)
	ticket.CredentialMode = config.CodexTicketCredentialCookie
	ticket.Cookies = []*http.Cookie{{Name: "session", Value: "original", Path: "/backend-api", Secure: true, Expires: time.Now().Add(5 * time.Minute)}}
	ticket.Verified = true
	s.openaiCodexTickets.Store(openAICodexTicketKey(r.account.ID, ticket.Model), ticket)
	job, reason := s.prepareCodexModelQuality(context.Background(), r.account, ticket.Model, initial.policy)
	require.NotNil(t, job, reason)
	upstream := &qualityTLSUpstream{t: t}
	s.httpUpstream = upstream
	before := job.ticket.ExpiresAt
	for range 2 {
		_, err := s.requestCodexModelQuality(context.Background(), job, "test")
		require.NoError(t, err)
	}
	require.Equal(t, 2, upstream.calls)
	require.Equal(t, "original", job.ticket.Cookies[0].Value)
	require.Equal(t, before, job.ticket.ExpiresAt)
	require.Zero(t, r.writes)
}
