package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type ticketEmptyPoolScheduleRepo struct {
	*ticketFollowingBusinessRepo
	disabledIDs []int64
	disableErr  error
}

func (r *ticketEmptyPoolScheduleRepo) DisableRandomProxyAccountIfUnavailable(_ context.Context, id int64) error {
	r.disabledIDs = append(r.disabledIDs, id)
	return r.disableErr
}

func ticketScheduledEmptyPoolFixture(t *testing.T, policy string) (*OpenAIGatewayService, *ticketEmptyPoolScheduleRepo) {
	t.Helper()
	svc, base := codexScheduledFixture(t, false)
	repo := &ticketEmptyPoolScheduleRepo{ticketFollowingBusinessRepo: &ticketFollowingBusinessRepo{codexScheduleRepo: base}}
	svc.accountRepo = repo
	base.account.Extra[ProxyModeExtraKey] = ProxyModeRandom
	base.account.Extra[CodexTicketProxyModeExtraKey] = CodexTicketProxyModeAccount
	base.account.Extra[RandomProxyEmptyPoolPolicyExtraKey] = policy
	return svc, repo
}

func TestCodexTicketScheduledEmptyPoolHonorsAccountPolicy(t *testing.T) {
	for _, policy := range []string{RandomProxyEmptyPoolPolicyReject, RandomProxyEmptyPoolPolicyDisable, RandomProxyEmptyPoolPolicyDirect} {
		t.Run(policy, func(t *testing.T) {
			svc, repo := ticketScheduledEmptyPoolFixture(t, policy)
			if policy != RandomProxyEmptyPoolPolicyDirect {
				repo.reserveErr = &CodexTicketWaitError{Status: &CodexTicketRuntimeStatus{Reason: "pool_empty"}}
			}
			_, proxy, err := svc.reserveCodexTicketHarvest(context.Background(), repo.account, "gpt-6-astra", svc.openAICodexTicketConfig())
			if policy == RandomProxyEmptyPoolPolicyDirect {
				require.NoError(t, err)
				require.Empty(t, proxy.url)
			} else {
				require.ErrorIs(t, err, repo.reserveErr)
			}
			require.Len(t, repo.reserves, 1)
			require.True(t, repo.reserves[0].HarvestUsesBusiness)
			require.Equal(t, policy == RandomProxyEmptyPoolPolicyDirect, repo.reserves[0].AllowDirectOnEmpty)
			require.Equal(t, policy == RandomProxyEmptyPoolPolicyDisable, len(repo.disabledIDs) > 0)
			require.Zero(t, repo.started)
			require.Empty(t, repo.finishes)
		})
	}
}

func TestCodexTicketScheduledTemporaryPoolWaitNeverDisables(t *testing.T) {
	for _, reason := range []string{"proxy_silent", "capacity", "proxy_unhealthy", "proxy_switch_waiting"} {
		t.Run(reason, func(t *testing.T) {
			svc, repo := ticketScheduledEmptyPoolFixture(t, RandomProxyEmptyPoolPolicyDisable)
			repo.reserveErr = &CodexTicketWaitError{Status: &CodexTicketRuntimeStatus{Reason: reason}}
			_, _, err := svc.reserveCodexTicketHarvest(context.Background(), repo.account, "gpt-6-astra", svc.openAICodexTicketConfig())
			require.ErrorIs(t, err, repo.reserveErr)
			require.Empty(t, repo.disabledIDs)
		})
	}
}

func TestCodexTicketScheduledEmptyPoolDisableFailureIsReported(t *testing.T) {
	svc, repo := ticketScheduledEmptyPoolFixture(t, RandomProxyEmptyPoolPolicyDisable)
	repo.reserveErr = &CodexTicketWaitError{Status: &CodexTicketRuntimeStatus{Reason: "pool_empty"}}
	repo.disableErr = errors.New("conditional disable unavailable")
	_, _, err := svc.reserveCodexTicketHarvest(context.Background(), repo.account, "gpt-6-astra", svc.openAICodexTicketConfig())
	require.ErrorContains(t, err, repo.disableErr.Error())
	require.Equal(t, []int64{repo.account.ID}, repo.disabledIDs)
}
