package service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type ticketSessionEpochRepo struct {
	*ticketPreviewReadOnlyRepo
	epoch string
	err   error
}

func (r *ticketSessionEpochRepo) GetCodexTicketSessionEpoch(_ context.Context, scope CodexTicketSessionScope) (string, error) {
	if scope.AccountID != r.account.ID {
		return "", errors.New("unexpected scope")
	}
	return r.epoch, r.err
}

func TestCodexTicketSessionEpochSnapshotAndReadOnlyPreview(t *testing.T) {
	svc, base := ticketPreviewService(t)
	repo := &ticketSessionEpochRepo{ticketPreviewReadOnlyRepo: base, epoch: "cycle-one"}
	svc.accountRepo = repo
	svc.cfg.Gateway.OpenAICodexTicket.SessionMode = "account_model"
	input := openAICodexTicketProbeInput{Account: base.account, Token: "test", Model: "gpt-6-astra"}
	first := ticketSessionRequest(t, svc, input).Header.Get("session_id")
	captured := repo.epoch
	input.SessionEpoch = &captured
	repo.epoch = "cycle-two"
	input.State = "candidate-ticket"
	require.Equal(t, first, ticketSessionRequest(t, svc, input).Header.Get("session_id"), "复验使用本轮快照")
	input.SessionEpoch, input.State = nil, ""
	next := ticketSessionRequest(t, svc, input).Header.Get("session_id")
	require.NotEqual(t, first, next, "阈值后的下一轮使用新 Session")
	preview, err := svc.PreviewOpenAICodexTicketRequest(context.Background(), base.account.ID, CodexTicketRequestPreviewInput{Model: input.Model})
	require.NoError(t, err)
	require.False(t, preview.Sent)
	require.Equal(t, next, http.Header(preview.BeforeHeaders).Get("session_id"))
	require.Equal(t, "cycle-two", repo.epoch)
	repo.err = errors.New("redis unavailable")
	_, err = svc.buildOpenAICodexTicketProbeRequest(context.Background(), input)
	require.Error(t, err, "读共享轮次失败不能悄悄退回旧 Session")
	svc.cfg.Gateway.OpenAICodexTicket.SessionMode = "random"
	require.NotEmpty(t, ticketSessionRequest(t, svc, input).Header.Get("session_id"))
}

type ticketFollowingBusinessRepo struct {
	*codexScheduleRepo
	reserves           []CodexTicketReserveRequest
	businessSelections int
}

func (r *ticketFollowingBusinessRepo) ReserveCodexTicket(ctx context.Context, req CodexTicketReserveRequest) (*CodexTicketReservation, error) {
	r.reserves = append(r.reserves, req)
	reservation, err := r.codexScheduleRepo.ReserveCodexTicket(ctx, req)
	if reservation != nil {
		reservation.SessionEpoch = "shared-cycle"
	}
	return reservation, err
}

func (r *ticketFollowingBusinessRepo) SelectBalancedProxy(ctx context.Context, _ ProxyPoolSelection) (*Proxy, error) {
	if CodexTicketBusinessReservationFrom(ctx) == nil {
		return nil, errors.New("ticket resolver bypassed shared reservation")
	}
	r.businessSelections++
	return r.proxy, nil
}

func TestCodexTicketFollowingRandomAccountUsesSharedPoolAndSession(t *testing.T) {
	svc, base := codexScheduledFixture(t, false)
	repo := &ticketFollowingBusinessRepo{codexScheduleRepo: base}
	svc.accountRepo = repo
	base.account.Extra[ProxyModeExtraKey] = ProxyModeRandom
	base.account.Extra[CodexTicketProxyModeExtraKey] = CodexTicketProxyModeAccount
	base.proxy = &Proxy{ID: 8, Status: StatusActive, Protocol: "http", Host: "business.example", Port: 8080}
	svc.cfg.Gateway.OpenAICodexTicket.SessionMode = "account"
	upstream := &codexTicketVerificationUpstream{respond: func(int) *http.Response {
		return codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(356))
	}}
	svc.httpUpstream = upstream
	svc.probeOnceOpenAICodexTicket(context.Background(), base.account, "gpt-6-astra")
	require.Len(t, repo.reserves, 1)
	require.True(t, repo.reserves[0].HarvestUsesBusiness)
	require.True(t, repo.reserves[0].PoolMode)
	require.Nil(t, repo.reserves[0].FixedProxy)
	require.Equal(t, 1, repo.businessSelections)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, upstream.requests[0].Header.Get("session_id"), upstream.requests[1].Header.Get("session_id"))
	require.Equal(t, upstream.proxies[0], upstream.proxies[1])
	require.NotNil(t, svc.lookupOpenAICodexTicket(base.account, "gpt-6-astra"))
}
