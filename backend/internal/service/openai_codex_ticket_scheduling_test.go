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

type codexScheduleRepo struct {
	*codexTicketControlRepo
	started, stages, validates     int
	reports                        [][3]bool
	finishes                       []CodexTicketFinishRequest
	stageErr, startErr, reserveErr error
	proxy                          *Proxy
	history                        []CodexTicketAttempt
	businessFailures               int
}

func (r *codexScheduleRepo) ReportRandomProxyFailure(context.Context, int64, int64) error {
	r.businessFailures++
	return nil
}

func (r *codexScheduleRepo) SelectRandomActiveProxy(context.Context) (*Proxy, error) {
	return r.proxy, nil
}

func TestCodexTicketBusinessStreamQuotaDoesNotPunishEitherProxy(t *testing.T) {
	svc, repo := codexScheduledFixture(t, true)
	repo.account.Extra[ProxyModeExtraKey] = ProxyModeRandom
	repo.proxy = &Proxy{ID: 8, Status: StatusActive, Protocol: "http", Host: "business.example", Port: 8080}
	calls := 0
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		calls++
		resp := codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(356))
		if calls == 2 {
			resp.Body = io.NopCloser(strings.NewReader("data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"model\":\"gpt-6-astra\",\"error\":{\"type\":\"usage_limit_reached\",\"resets_in_seconds\":900}}}\n\n"))
		}
		return resp, nil
	}}
	svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.Equal(t, 2, calls)
	require.Zero(t, repo.businessFailures)
	require.Len(t, repo.finishes, 1)
	require.False(t, repo.finishes[0].Silence)
	require.Equal(t, "account", repo.finishes[0].RetryScope)
	require.Equal(t, "upstream_error", repo.finishes[0].Outcome)
	require.Nil(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
}

func (r *codexScheduleRepo) ReserveCodexTicket(_ context.Context, in CodexTicketReserveRequest) (*CodexTicketReservation, error) {
	if r.reserveErr != nil {
		return nil, r.reserveErr
	}
	return &CodexTicketReservation{AccountID: in.AccountID, Model: in.Model, Token: "lease", Generation: 1, Config: in.Config, Proxy: r.proxy}, nil
}
func (r *codexScheduleRepo) StartCodexTicket(context.Context, *CodexTicketReservation) error {
	if r.startErr != nil {
		return r.startErr
	}
	r.started++
	return nil
}
func (r *codexScheduleRepo) CheckCodexTicketStage(context.Context, *CodexTicketReservation, *Proxy) error {
	r.stages++
	return r.stageErr
}
func (r *codexScheduleRepo) ValidateCodexTicket(context.Context, *CodexTicketReservation) error {
	r.validates++
	return nil
}
func (r *codexScheduleRepo) ReportCodexTicketHarvest(_ context.Context, _ *CodexTicketReservation, accepted, silence, neutral bool) error {
	r.reports = append(r.reports, [3]bool{accepted, silence, neutral})
	return nil
}
func (r *codexScheduleRepo) FinishCodexTicket(_ context.Context, in CodexTicketFinishRequest) error {
	r.finishes = append(r.finishes, in)
	return nil
}
func (r *codexScheduleRepo) GetCodexTicketRuntimeStatus(context.Context, int64) (*CodexTicketRuntimeStatus, error) {
	return &CodexTicketRuntimeStatus{State: "idle", AttemptsUsed: r.started, MaxAttempts: 6, Generation: 1}, nil
}
func (r *codexScheduleRepo) RecordCodexTicketAttempt(_ context.Context, _ int64, attempt CodexTicketAttempt) error {
	r.history = append(r.history, attempt)
	return nil
}

func codexScheduledFixture(t *testing.T, enabled bool) (*OpenAIGatewayService, *codexScheduleRepo) {
	t.Helper()
	svc, base := codexTicketControlService(t)
	policy := config.DefaultCodexTicketProtection()
	policy.Enabled = enabled
	svc.cfg.Gateway.OpenAICodexTicket.Protection = &policy
	svc.cfg.Gateway.OpenAICodexTicket.LengthMode = config.CodexTicketLengthAuto
	repo := &codexScheduleRepo{codexTicketControlRepo: base}
	svc.accountRepo = repo
	return svc, repo
}

func TestCodexTicketScheduledHarvestOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name             string
		enabled          bool
		length           int
		model            string
		deferred         bool
		calls            int
		outcome          string
		silence, success bool
	}{
		{"off accepts 312", false, 312, "gpt-6-astra", false, 2, "success", false, true},
		{"enabled rejects 312", true, 312, "gpt-6-astra", false, 1, "ticket_rejected", true, false},
		{"312 mismatch still silences", true, 312, "gpt-5.6-luna", false, 1, "ticket_rejected", true, false},
		{"premium mismatch 356", true, 356, "gpt-5.6-luna", false, 1, "model_mismatch", false, false},
		{"business waits", true, 356, "gpt-6-astra", true, 1, "verification_deferred", false, false},
		{"accepted publishes", true, 356, "gpt-6-astra", false, 2, "success", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := codexScheduledFixture(t, tc.enabled)
			if tc.deferred {
				repo.stageErr = &CodexTicketWaitError{Status: &CodexTicketRuntimeStatus{State: "waiting", Reason: "proxy_silent", ProxyID: 8}}
			}
			calls := 0
			svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
				calls++
				return codexTicketCompletedResponse(tc.model, fakeCodexTicketState(tc.length)), nil
			}}
			svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
			require.Equal(t, tc.calls, calls)
			require.Equal(t, 1, repo.started)
			require.Len(t, repo.finishes, 1)
			require.Equal(t, tc.outcome, repo.finishes[0].Outcome)
			require.Equal(t, tc.silence, repo.finishes[0].Silence)
			require.Equal(t, tc.success, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra") != nil)
			require.Len(t, repo.history, 1)
			require.Equal(t, tc.outcome, repo.history[0].Outcome)
			if tc.deferred {
				require.EqualValues(t, 8, repo.finishes[0].DeferredProxyID)
			}
		})
	}
}

func TestCodexTicketScheduledStreamQuotaOverridesLengthRule(t *testing.T) {
	svc, repo := codexScheduledFixture(t, true)
	calls := 0
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		calls++
		resp := codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(312))
		resp.Body = io.NopCloser(strings.NewReader("data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"model\":\"gpt-6-astra\",\"error\":{\"type\":\"usage_limit_reached\",\"resets_in_seconds\":900}}}\n\n"))
		return resp, nil
	}}
	svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.Equal(t, 1, calls)
	require.Len(t, repo.finishes, 1)
	require.False(t, repo.finishes[0].Silence)
	require.Equal(t, "account", repo.finishes[0].RetryScope)
	require.True(t, repo.finishes[0].RetryAt.After(time.Now().Add(850*time.Second)))
	require.Equal(t, [3]bool{false, false, true}, repo.reports[0])
	require.Equal(t, 200, *repo.history[0].HarvestHTTPStatus)
}

func TestCodexTicketScheduledWaitDoesNotSendOrRecordFailure(t *testing.T) {
	for _, phase := range []string{"reserve", "start"} {
		t.Run(phase, func(t *testing.T) {
			svc, repo := codexScheduledFixture(t, true)
			wait := &CodexTicketWaitError{Status: &CodexTicketRuntimeStatus{State: "waiting", Reason: "account_cooldown"}}
			if phase == "reserve" {
				repo.reserveErr = wait
			} else {
				repo.startErr = wait
			}
			svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
				t.Fatal("waiting must not send")
				return nil, errors.New("unexpected")
			}}
			svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
			require.Zero(t, repo.started)
			require.Empty(t, repo.history)
			if phase == "start" {
				require.Len(t, repo.finishes, 1)
				require.False(t, repo.finishes[0].Started)
			}
		})
	}
}

func TestCodexTicketPendingModelsExcludeFullAffinityInventory(t *testing.T) {
	svc, repo := codexScheduledFixture(t, true)
	svc.cfg.Gateway.OpenAICodexTicket.Models = []string{"gpt-6-astra", "gpt-5.6-sol"}
	repo.account.Extra[CodexTicketProxyModeExtraKey] = CodexTicketProxyModeAccount
	cfg := svc.openAICodexTicketConfig()
	ticket := &openAICodexTicket{AccountID: repo.account.ID, Model: "gpt-5.6-sol", State: fakeCodexTicketState(356), Length: 356,
		Verified: true, CapturedAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(10 * time.Second),
		Binding: svc.codexTicketBinding(repo.account), AccountBinding: openAICodexTicketAccountBinding(repo.account)}
	svc.openaiCodexTickets.Store(openAICodexTicketKey(repo.account.ID, ticket.Model), ticket)
	require.True(t, ticket.usable(time.Now(), repo.account, cfg))
	require.Equal(t, []string{"gpt-6-astra", "gpt-5.6-sol"}, svc.codexTicketPendingModels(context.Background(), repo.account, cfg))
	standby := codexTicketLeaf(ticket)
	standby.State = "gAAAAA" + strings.Repeat("D", 350)
	standby.CapturedAt, standby.ExpiresAt = time.Now(), time.Now().Add(time.Hour)
	require.True(t, svc.storeOpenAICodexTicket(context.Background(), repo.account, standby))
	require.Equal(t, []string{"gpt-6-astra", "gpt-5.6-sol"}, svc.codexTicketPendingModels(context.Background(), repo.account, cfg), "备用充足时，软到期的主票仍需复验")
	require.Len(t, svc.codexTicketPendingModels(withCodexTicketManualRetry(context.Background()), repo.account, cfg), 2)
}

func TestCodexTicketManualAndAutomaticShareWorkerLimit(t *testing.T) {
	svc, upstream := codexTicketBoundedService(t, 2)
	svc.cfg.Gateway.OpenAICodexTicket.HarvestConcurrency = 1
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { svc.probeOnceOpenAICodexTicket(ctx, ticketTestAccount(1), "gpt-6-astra"); close(done) }()
	awaitCodexTicketProbeSignals(t, upstream.entered, 1)
	manualDone := make(chan struct{})
	go func() {
		svc.probeOnceOpenAICodexTicket(withCodexTicketManualRetry(context.WithoutCancel(ctx)), ticketTestAccount(2), "gpt-6-astra")
		close(manualDone)
	}()
	require.Never(t, func() bool { return upstream.calls.Load() > 1 }, 50*time.Millisecond, 5*time.Millisecond)
	cancel()
	awaitCodexTicketProbeSignals(t, done, 1)
	awaitCodexTicketProbeSignals(t, upstream.entered, 1)
	upstream.permit <- struct{}{}
	awaitCodexTicketProbeSignals(t, manualDone, 1)
	require.EqualValues(t, 2, upstream.calls.Load())
	require.Zero(t, svc.openaiCodexTicketActive.Load())
}
