package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexSchedulerAccountSessionUsesPerModelThreshold(t *testing.T) {
	for _, resetSol := range []bool{false, true} {
		name := "switch_resets_models"
		if resetSol {
			name = "success_preserves_other_model"
		}
		t.Run(name, func(t *testing.T) {
			a, _, req := newCodexPinTest(t, false)
			req.Config.SessionMode, req.Config.ProxyFailureThreshold = "account", 20
			req.Models = []string{"astra", "sol"}
			req.PoolMode, req.FixedProxy = false, codexPoolCandidate(1).proxy
			for range 19 {
				for _, model := range req.Models {
					req.Model = model
					codexSessionFail(t, a, codexStart(t, a, req))
					require.Empty(t, codexSessionEpoch(t, a, req), "cross-model failures must not be aggregated")
				}
			}
			if resetSol {
				req.Model = "sol"
				codexFinish(t, a, codexStart(t, a, req), "success")
			}
			req.Model = "astra"
			codexSessionFail(t, a, codexStart(t, a, req))
			epoch := codexSessionEpoch(t, a, req)
			require.NotEmpty(t, epoch, "astra's own twentieth failure rotates the shared session")
			req.Model = "sol"
			r := codexStart(t, a, req)
			require.Equal(t, epoch, r.SessionEpoch, "account identity scope stays shared")
			codexSessionFail(t, a, r)
			require.Equal(t, epoch, codexSessionEpoch(t, a, req), "a new epoch does not inherit old model counters")
		})
	}
}

func TestCodexSchedulerSessionLegacyAggregateDoesNotTriggerModelRotation(t *testing.T) {
	for _, mode := range []string{"account", "account_model"} {
		t.Run(mode, func(t *testing.T) {
			a, _, req := newCodexPinTest(t, false)
			ctx := context.Background()
			req.Config.SessionMode, req.Config.ProxyFailureThreshold = mode, 20
			req.Model, req.Models = "astra", []string{"astra", "sol"}
			r, err := a.ReserveCodexTicket(ctx, req)
			require.NoError(t, err)
			codexFinish(t, a, r, "canceled")
			var stored map[string]any
			require.NoError(t, json.Unmarshal([]byte(a.rdb.Get(ctx, codexSchedulerAccountKey(7)).Val()), &stored))
			key := "account"
			if mode == "account_model" {
				key = "model:astra"
			}
			stored["sessions"] = map[string]any{key: map[string]any{"epoch": "legacy", "failures": 20}}
			encoded, err := json.Marshal(stored)
			require.NoError(t, err)
			require.NoError(t, a.rdb.Set(ctx, codexSchedulerAccountKey(7), encoded, 0).Err())
			for range 19 {
				codexSessionFail(t, a, codexStart(t, a, req))
				require.Equal(t, "legacy", codexSessionEpoch(t, a, req))
			}
			codexSessionFail(t, a, codexStart(t, a, req))
			require.NotEqual(t, "legacy", codexSessionEpoch(t, a, req))
		})
	}
}

func TestCodexSchedulerSessionFailureCountersIsolateAccounts(t *testing.T) {
	a, _, first := newCodexPinTest(t, false)
	first.Config.SessionMode, first.Config.ProxyFailureThreshold = "account", 20
	first.PoolMode, first.FixedProxy = false, codexPoolCandidate(1).proxy
	second := first
	second.AccountID, second.Selection.AccountID = 8, 8
	for range 19 {
		codexSessionFail(t, a, codexStart(t, a, first))
		codexSessionFail(t, a, codexStart(t, a, second))
		require.Empty(t, codexSessionEpoch(t, a, first))
		require.Empty(t, codexSessionEpoch(t, a, second))
	}
	codexSessionFail(t, a, codexStart(t, a, first))
	firstEpoch := codexSessionEpoch(t, a, first)
	require.NotEmpty(t, firstEpoch)
	require.Empty(t, codexSessionEpoch(t, a, second), "one account's threshold does not rotate another account")
	codexSessionFail(t, a, codexStart(t, a, second))
	secondEpoch := codexSessionEpoch(t, a, second)
	require.NotEmpty(t, secondEpoch, "the other account keeps its own nineteen failures")
	require.NotEqual(t, firstEpoch, secondEpoch)
	require.Equal(t, firstEpoch, codexSessionEpoch(t, a, first))
}
