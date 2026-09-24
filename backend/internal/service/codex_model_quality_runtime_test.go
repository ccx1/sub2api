package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type qualityRuntimeRepo struct {
	AccountRepository
	mu           sync.Mutex
	account      *Account
	lease        string
	record       *CodexModelQualityRecord
	saveDeadline time.Time
	writes       int
	getHook      func(context.Context)
	casHook      func(context.Context) (bool, error)
}

func (r *qualityRuntimeRepo) GetByID(ctx context.Context, _ int64) (*Account, error) {
	if r.getHook != nil {
		r.getHook(ctx)
	}
	return cloneOpenAICodexTicketAccount(r.account), nil
}
func (r *qualityRuntimeRepo) CompareAndSwapCodexTicket(ctx context.Context, _ *Account, _ string, _ any) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writes++
	if r.casHook != nil {
		return r.casHook(ctx)
	}
	return true, nil
}
func (r *qualityRuntimeRepo) LoadCodexModelQuality(context.Context, int64, string) (*CodexModelQualityRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.record == nil {
		return nil, nil
	}
	copy := *r.record
	return &copy, nil
}
func (r *qualityRuntimeRepo) AcquireCodexModelQuality(_ context.Context, _ int64, _, lease string, _ time.Duration) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lease != "" {
		return false, nil
	}
	r.lease = lease
	return true, nil
}
func (r *qualityRuntimeRepo) SaveCodexModelQuality(ctx context.Context, _ int64, _, lease string, record *CodexModelQualityRecord, _ time.Duration) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lease != lease {
		return false, nil
	}
	if record.Status.Status == "running" {
		r.saveDeadline, _ = ctx.Deadline()
	}
	copy := *record
	r.record = &copy
	return true, nil
}
func (r *qualityRuntimeRepo) ReleaseCodexModelQuality(_ context.Context, _ int64, _, lease string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lease == lease {
		r.lease = ""
	}
	return nil
}

func qualityRuntimeFixture(t *testing.T) (*OpenAIGatewayService, *qualityRuntimeRepo, *codexModelQualityJob) {
	t.Helper()
	s := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, nil)
	settings, settingRepo := newTicketPolicySettings()
	settings.cfg = s.cfg
	s.settingService = settings
	p := DefaultCodexModelQualityPolicy()
	p.Enabled, p.FingerprintEnabled = true, false
	raw, _ := json.Marshal(p)
	settingRepo.values[SettingKeyCodexModelQuality] = string(raw)
	account := ticketTestAccount(41)
	account.Extra = make(map[string]any)
	r := &qualityRuntimeRepo{account: account}
	s.accountRepo = r
	ticket := &openAICodexTicket{AccountID: account.ID, Model: "gpt-6-astra", State: fakeCodexTicketState(292), Length: 292,
		CapturedAt: time.Now().Add(-10 * time.Minute), ExpiresAt: time.Now().Add(time.Hour), Egress: openAICodexTicketEgress(""), SessionID: "quality-session"}
	s.openaiCodexTickets.Store(openAICodexTicketKey(account.ID, ticket.Model), ticket)
	job, reason := s.prepareCodexModelQuality(context.Background(), account, ticket.Model, p)
	require.NotNil(t, job, reason)
	job.lease, job.source, r.lease = "quality-lease", "automatic", "quality-lease"
	job.leaseExpiresAt = time.Now().Add(time.Minute)
	return s, r, job
}

func TestCodexModelQualityBudgetProtectsHardExpiry(t *testing.T) {
	now := time.Now()
	p := DefaultCodexModelQualityPolicy()
	for _, tc := range []struct{ remaining, want time.Duration }{
		{time.Hour, 30 * time.Second}, {100 * time.Second, 10 * time.Second}, {32 * time.Second, 2 * time.Second}, {31 * time.Second, 0}, {0, 0}, {-time.Second, 0},
	} {
		require.Equal(t, tc.want, codexModelQualityBudget(now, now.Add(tc.remaining), p))
	}
}

func TestCodexModelQualityWorkerAppliesGlobalAndAccountLimits(t *testing.T) {
	s := &OpenAIGatewayService{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.codexModelQuality.ctx = ctx

	_, acquired, reason := s.takeCodexModelQualityWorker(1, 2, 1)
	require.True(t, acquired)
	require.Empty(t, reason)
	_, acquired, reason = s.takeCodexModelQualityWorker(1, 2, 1)
	require.False(t, acquired)
	require.Equal(t, "account_capacity", reason)
	_, acquired, reason = s.takeCodexModelQualityWorker(2, 2, 1)
	require.True(t, acquired)
	require.Empty(t, reason)
	_, acquired, reason = s.takeCodexModelQualityWorker(3, 2, 1)
	require.False(t, acquired)
	require.Equal(t, "capacity", reason)

	s.finishCodexModelQualityWorker(1)
	s.finishCodexModelQualityWorker(2)
	s.codexModelQuality.mu.Lock()
	require.Zero(t, s.codexModelQuality.active)
	require.Empty(t, s.codexModelQuality.activeByAccount)
	s.codexModelQuality.mu.Unlock()
}

func TestCodexModelQualityDiagnosticWorkerStartsWithoutBackgroundLoop(t *testing.T) {
	s := &OpenAIGatewayService{}

	runCtx, acquired, reason := s.takeCodexModelQualityDiagnosticWorker(41, 1, 1)
	require.True(t, acquired, reason)
	require.NotNil(t, runCtx)
	require.NoError(t, runCtx.Err())
	s.finishCodexModelQualityWorker(41)

	_, acquired, reason = s.takeCodexModelQualityWorker(41, 1, 1)
	require.False(t, acquired)
	require.Equal(t, "capacity", reason)
}

func TestCodexModelQualityRequestPinsCredentialsAndDoesNotRotateCookie(t *testing.T) {
	s, _, job := qualityRuntimeFixture(t)
	challenge := newCodexQualityCapabilityChallenge()
	answer, _ := json.Marshal(challenge.expected)
	expires := job.ticket.ExpiresAt
	s.httpUpstream = &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		require.Equal(t, job.ticket.State, req.Header.Get(openAICodexTurnStateHeader))
		require.Equal(t, job.ticket.SessionID, req.Header.Get("session_id"))
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), "Complete the task directly")
		require.NotContains(t, string(body), "Reply with exactly: pong")
		return &http.Response{StatusCode: 200, Header: http.Header{"Set-Cookie": {"__cflb=replace; Max-Age=9999"}}, Body: io.NopCloser(strings.NewReader(qualityResponseBody("", string(answer))))}, nil
	}}
	status := qualityStatusForJob(job)
	score, err := s.answerCodexQualityCapability(context.Background(), job, &status, challenge)
	require.NoError(t, err)
	require.Equal(t, float64(100), score)
	require.Equal(t, "unknown", status.ModelIdentity)
	require.Equal(t, expires, job.ticket.ExpiresAt)
	require.Empty(t, job.ticket.Cookies)
}

func TestCodexModelQualityNoBudgetDoesNotSendOrRevoke(t *testing.T) {
	s, r, job := qualityRuntimeFixture(t)
	job.ticket.ExpiresAt = time.Now().Add(20 * time.Second)
	s.openaiCodexTickets.Store(openAICodexTicketKey(job.account.ID, job.ticket.Model), codexTicketLeaf(job.ticket))
	s.executeCodexModelQuality(context.Background(), job, r)
	record, _ := r.LoadCodexModelQuality(context.Background(), job.account.ID, job.ticket.Model)
	require.Equal(t, "skipped", record.Status.Status)
	require.Equal(t, "insufficient_ttl", record.Status.Reason)
	require.Zero(t, record.Status.Requests)
	require.Zero(t, r.writes)
}

func TestCodexModelQualityTimeoutIsInconclusiveAndSharesAbsoluteDeadline(t *testing.T) {
	s, r, job := qualityRuntimeFixture(t)
	s.httpUpstream = &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		deadline, ok := req.Context().Deadline()
		require.True(t, ok)
		require.Equal(t, r.saveDeadline, deadline)
		<-req.Context().Done()
		return nil, req.Context().Err()
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	s.executeCodexModelQuality(ctx, job, r)
	record, _ := r.LoadCodexModelQuality(context.Background(), job.account.ID, job.ticket.Model)
	require.Equal(t, "inconclusive", record.Status.Status)
	require.Equal(t, "timeout", record.Status.Reason)
	require.Zero(t, r.writes)
}

func TestCodexModelQualityConfirmedFailureQuarantinesOnlyTestedTicket(t *testing.T) {
	s, r, job := qualityRuntimeFixture(t)
	calls := 0
	s.httpUpstream = &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(qualityResponseBody(job.ticket.Model,
			`{"selected":[],"transformed":[],"checksum":-999,"flags":[],"final":-999}`)))}, nil
	}}
	s.executeCodexModelQuality(context.Background(), job, r)
	record, _ := r.LoadCodexModelQuality(context.Background(), job.account.ID, job.ticket.Model)
	require.Equal(t, 2, calls)
	require.Equal(t, "quarantined", record.Status.Status)
	require.Equal(t, "capability_failed", record.Status.Reason)
	require.Equal(t, 1, r.writes)
	require.True(t, s.codexTicketRevoked(openAICodexTicketKey(job.account.ID, job.ticket.Model), job.ticket))
}

func TestCodexModelQualityLateResultCannotRevokeReplacement(t *testing.T) {
	s, r, job := qualityRuntimeFixture(t)
	replacement := codexTicketLeaf(job.ticket)
	replacement.CapturedAt = time.Now()
	replacement.State = "gAAAAA" + strings.Repeat("C", 286)
	s.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		s.openaiCodexTickets.Store(openAICodexTicketKey(job.account.ID, job.ticket.Model), replacement)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(qualityResponseBody("other", "{}")))}, nil
	}}
	s.executeCodexModelQuality(context.Background(), job, r)
	record, _ := r.LoadCodexModelQuality(context.Background(), job.account.ID, job.ticket.Model)
	require.Equal(t, "stale", record.Status.Status)
	require.Zero(t, r.writes)
	require.False(t, s.codexTicketRevoked(openAICodexTicketKey(job.account.ID, job.ticket.Model), replacement))
}
