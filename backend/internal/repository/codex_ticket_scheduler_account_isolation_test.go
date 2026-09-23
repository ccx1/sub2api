package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexSchedulerFailureBudgetsAreIndependentAcrossAccounts(t *testing.T) {
	for _, outcome := range []string{"upstream_error", "ticket_rejected"} {
		t.Run(outcome, func(t *testing.T) {
			a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
			cfg := codexSchedulerConfig()
			cfg.Protection.MaxAccountAttempts = 2
			cfg.Protection.RejectionRetryMaxAttempts = 2
			first, second := codexRequest(7, cfg), codexRequest(8, cfg)
			first.Manual, second.Manual = true, true
			codexFinish(t, a, codexStart(t, a, first), outcome)
			codexFinish(t, a, codexStart(t, a, second), outcome)
			firstFailure := codexStart(t, a, first)
			codexFinish(t, a, firstFailure, outcome)
			cooldown := "account_cooldown"
			if outcome == "ticket_rejected" {
				cooldown = "rejection_cooldown"
			}
			require.Equal(t, cooldown, firstFailure.Status.Reason)
			secondStatus, err := a.GetCodexTicketRuntimeStatus(context.Background(), 8)
			require.NoError(t, err)
			require.Equal(t, 1, secondStatus.AttemptsUsed)
			require.NotEqual(t, cooldown, secondStatus.Reason)
			codexFinish(t, a, codexStart(t, a, first), "success")
			secondFailure := codexStart(t, a, second)
			codexFinish(t, a, secondFailure, outcome)
			require.Equal(t, 2, secondFailure.Status.AttemptsUsed)
			require.Equal(t, cooldown, secondFailure.Status.Reason, "another account's success must not reset this account")
			firstStatus, err := a.GetCodexTicketRuntimeStatus(context.Background(), 7)
			require.NoError(t, err)
			require.Zero(t, firstStatus.AttemptsUsed)
			require.Nil(t, firstStatus.CooldownUntil)
			require.Nil(t, firstStatus.RetryAt)
		})
	}
}
