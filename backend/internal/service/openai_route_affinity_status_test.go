package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type routeAffinityStatusRepo struct {
	AccountRepository
	accountID int64
	models    []string
	reasons   []string
}

func (r *routeAffinityStatusRepo) SetModelRateLimit(_ context.Context, accountID int64, model string, _ time.Time, reasons ...string) error {
	r.accountID = accountID
	r.models = append(r.models, model)
	r.reasons = append(r.reasons, reasons...)
	return nil
}

func routeAffinityStatusFixture(t *testing.T) (*OpenAIGatewayService, openAIWSAcquireRequest, *routeAffinityDialer) {
	t.Helper()
	pool, req, dialer := routeAffinityFixture(t)
	s := &OpenAIGatewayService{cfg: pool.cfg, openaiWSPool: pool,
		settingService: &SettingService{settingRepo: &modelQualitySettingsRepo{}},
		accountRepo:    &routeAffinityStatusRepo{}}
	return s, req, dialer
}

func TestEnrichCodexRouteAffinityStatusPreservesTicketReadiness(t *testing.T) {
	cases := []struct {
		name, mode, want                            string
		enabled, connected, expired, cooling, ready bool
		count                                       int
	}{
		{"disabled", CodexRouteAffinityStrict, "off", false, true, false, false, true, 0},
		{"off", CodexRouteAffinityOff, "off", true, true, false, false, true, 0},
		{"unknown", CodexRouteAffinityStrict, "unknown", true, false, false, false, true, 0},
		{"available", CodexRouteAffinityStrict, "available", true, true, false, false, false, 1},
		{"unavailable", CodexRouteAffinityStrict, "unavailable", true, true, false, true, true, 0},
		{"expired", CodexRouteAffinityStrict, "unknown", true, true, true, false, true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, req, dialer := routeAffinityStatusFixture(t)
			policy := DefaultCodexRequestStrategyPolicy()
			policy.Enabled, policy.RouteAffinityMode = tc.enabled, tc.mode
			_, err := s.settingService.UpdateCodexRequestStrategyPolicy(context.Background(), policy)
			require.NoError(t, err)
			if tc.connected {
				lease, err := s.getOpenAIWSConnPool().Acquire(context.Background(), req)
				require.NoError(t, err)
				lease.Release()
			}
			if tc.expired {
				req.CodexTicketReceipt.ticket.ExpiresAt = time.Now().Add(-time.Second)
			}
			if tc.cooling {
				s.pauseOpenAIModelForRouteAffinity(context.Background(), req.Account, req.CodexTicketReceipt.ticket.Model, 90)
			}
			assertRouteAffinityEnrichment(t, s, req, tc.ready, tc.want, tc.count)
			wantDials := 0
			if tc.connected {
				wantDials = 1
			}
			require.Len(t, dialer.sentHeaders(), wantDials, "读取管理状态不能新增握手")
		})
	}
}

func assertRouteAffinityEnrichment(t *testing.T, s *OpenAIGatewayService, req openAIWSAcquireRequest, ready bool, wantStatus string, wantCount int) {
	t.Helper()
	want := OpenAICodexTicketStatus{Model: req.CodexTicketReceipt.ticket.Model, Ready: ready,
		AvailableCount: 3, PrimaryReady: ready, CredentialState: "ready", RemainingSeconds: 60,
		RouteAffinityStatus: wantStatus, RouteAffinityConnections: wantCount}
	statuses := []OpenAICodexTicketStatus{want}
	statuses[0].RouteAffinityStatus, statuses[0].RouteAffinityConnections = "stale", 99
	s.EnrichCodexRouteAffinityStatus(context.Background(), req.Account, statuses)
	require.Equal(t, want, statuses[0], "连接状态只能补充路由字段，不能修改票据可用性")
}

func TestRouteAffinityCooldownOnlyPausesRequestedModel(t *testing.T) {
	s, req, _ := routeAffinityStatusFixture(t)
	policy := DefaultCodexRequestStrategyPolicy()
	policy.Enabled, policy.RouteAffinityMode = true, CodexRouteAffinityStrict
	_, err := s.settingService.UpdateCodexRequestStrategyPolicy(context.Background(), policy)
	require.NoError(t, err)
	model := req.CodexTicketReceipt.ticket.Model
	s.pauseOpenAIModelForRouteAffinity(context.Background(), req.Account, model, 90)
	repo := s.accountRepo.(*routeAffinityStatusRepo)
	require.Equal(t, req.Account.ID, repo.accountID)
	require.Equal(t, []string{model}, repo.models)
	require.Equal(t, []string{openAIWSRouteAffinityUnavailableReason}, repo.reasons)
	require.Positive(t, req.Account.GetModelRateLimitRemainingTime(model))
	require.Zero(t, req.Account.GetModelRateLimitRemainingTime("gpt-5.6-luna"))
	require.Equal(t, StatusActive, req.Account.Status)
	require.True(t, req.Account.Schedulable)
	statuses := []OpenAICodexTicketStatus{{Model: model, Ready: true}, {Model: "gpt-5.6-luna", Ready: true}}
	s.EnrichCodexRouteAffinityStatus(context.Background(), req.Account, statuses)
	require.Equal(t, "unavailable", statuses[0].RouteAffinityStatus)
	require.Equal(t, "unknown", statuses[1].RouteAffinityStatus)
	require.True(t, statuses[0].Ready)
	require.True(t, statuses[1].Ready)
}
