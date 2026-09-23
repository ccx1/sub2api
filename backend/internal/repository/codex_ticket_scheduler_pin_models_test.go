package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type codexModelPinTest struct {
	allocator *ProxyPoolAllocator
	request   service.CodexTicketReserveRequest
	role      string
}

func newCodexModelPinTest(t *testing.T, role string) codexModelPinTest {
	t.Helper()
	a, _, req := newCodexPinTest(t, false)
	req.Config.ProxyFailureThreshold = 20
	codexSeedPins(t, a, req)
	req.HarvestUsesBusiness = role == "follow_business"
	return codexModelPinTest{allocator: a, request: req, role: role}
}

func (fixture codexModelPinTest) start(t *testing.T, model string) (*service.CodexTicketReservation, int64) {
	t.Helper()
	req := fixture.request
	req.Model, req.Models = model, []string{model}
	r := codexStart(t, fixture.allocator, req)
	if fixture.role == "business" {
		require.EqualValues(t, 1, r.Proxy.ID, "business failures must preserve the harvest binding")
		return r, codexPinBusiness(t, fixture.allocator, r).ID
	}
	if fixture.role == "follow_business" && model == "sol" {
		require.Equal(t, r.Proxy.ID, codexPinBusiness(t, fixture.allocator, r).ID)
	}
	return r, r.Proxy.ID
}

func (fixture codexModelPinTest) finish(t *testing.T, r *service.CodexTicketReservation, success bool) {
	t.Helper()
	ctx := context.Background()
	finish := service.CodexTicketFinishRequest{Reservation: r, Outcome: "upstream_error"}
	if fixture.role == "harvest" {
		finish.HarvestProxyFailed = !success
		if success {
			require.NoError(t, fixture.allocator.ReportCodexTicketHarvest(ctx, r, true, false, false))
		}
	} else if fixture.role == "follow_business" && r.Model == "astra" {
		finish.HarvestProxyFailed = !success
	} else {
		finish.BusinessProxyFailed, finish.BusinessProxySucceeded = !success, success
		finish.Outcome = "verification_failed"
	}
	if success {
		finish.Outcome = "success"
	}
	require.NoError(t, fixture.allocator.FinishCodexTicket(ctx, finish))
}

func (fixture codexModelPinTest) failures(t *testing.T, model string, count int, proxyID int64) {
	t.Helper()
	for i := 0; i < count; i++ {
		r, selected := fixture.start(t, model)
		require.Equal(t, proxyID, selected, "%s failure %d must retain this binding", model, i+1)
		fixture.finish(t, r, false)
	}
}

func TestCodexSchedulerPinFailuresAreIndependentAcrossModels(t *testing.T) {
	for _, role := range []string{"harvest", "business", "follow_business"} {
		t.Run(role, func(t *testing.T) {
			fixture := newCodexModelPinTest(t, role)
			initial := int64(1)
			if role != "harvest" {
				initial = 2
			}
			for range 19 {
				fixture.failures(t, "astra", 1, initial)
				fixture.failures(t, "sol", 1, initial)
			}
			r, selected := fixture.start(t, "sol")
			require.Equal(t, initial, selected, "19 failures per model must not accumulate into 38")
			fixture.finish(t, r, true)
			fixture.failures(t, "sol", 7, initial)
			fixture.failures(t, "astra", 1, initial)
			r, replacement := fixture.start(t, "sol")
			require.NotEqual(t, initial, replacement, "sol success must not erase astra's 19 failures")
			fixture.finish(t, r, false)
			fixture.failures(t, "sol", 18, replacement)
			fixture.failures(t, "astra", 19, replacement)
			r, selected = fixture.start(t, "sol")
			require.Equal(t, replacement, selected, "new binding must start with fresh counters for every model")
			fixture.finish(t, r, false)
			r, selected = fixture.start(t, "sol")
			require.NotEqual(t, replacement, selected, "the 20th sol failure must rotate the new binding")
			if role == "harvest" {
				require.EqualValues(t, 2, codexPinBusiness(t, fixture.allocator, r).ID,
					"harvest failures must preserve the independent business binding")
			}
			codexFinish(t, fixture.allocator, r, "canceled")
		})
	}
}

func TestCodexSchedulerPinLegacyScalarFailuresDoNotRotateNewModel(t *testing.T) {
	for _, role := range []string{"harvest", "business", "follow_business"} {
		t.Run(role, func(t *testing.T) {
			fixture := newCodexModelPinTest(t, role)
			ctx := context.Background()
			key := codexSchedulerAccountKey(fixture.request.AccountID)
			raw, err := fixture.allocator.rdb.Get(ctx, key).Bytes()
			require.NoError(t, err)
			var state map[string]any
			require.NoError(t, json.Unmarshal(raw, &state))
			pinRole, initial := "harvest", int64(1)
			if role != "harvest" {
				pinRole, initial = "business", 2
			}
			pin, ok := state[pinRole+"pin"].(map[string]any)
			require.True(t, ok)
			pin["failures"] = 20
			delete(pin, "failure_version")
			delete(pin, "model_failures")
			legacy, err := json.Marshal(state)
			require.NoError(t, err)
			require.NoError(t, fixture.allocator.rdb.Set(ctx, key, legacy, 0).Err())
			fixture.failures(t, "sol", 19, initial)
			fixture.failures(t, "sol", 1, initial)
			r, selected := fixture.start(t, "sol")
			require.NotEqual(t, initial, selected, "legacy scalar must reset once, then count actual failures")
			codexFinish(t, fixture.allocator, r, "canceled")
		})
	}
}

func TestCodexSchedulerPinFailuresAreIndependentAcrossAccounts(t *testing.T) {
	for _, role := range []string{"harvest", "business", "follow_business"} {
		t.Run(role, func(t *testing.T) {
			first := newCodexModelPinTest(t, role)
			second := first
			second.request.AccountID, second.request.Selection.AccountID = 8, 8
			seed := second.request
			seed.HarvestUsesBusiness = false
			codexSeedPins(t, second.allocator, seed)
			initial := int64(1)
			if role != "harvest" {
				initial = 2
			}
			for range 19 {
				first.failures(t, "astra", 1, initial)
				second.failures(t, "astra", 1, initial)
			}
			first.failures(t, "astra", 1, initial)
			r, replacement := first.start(t, "astra")
			require.NotEqual(t, initial, replacement)
			codexFinish(t, first.allocator, r, "canceled")
			r, selected := second.start(t, "astra")
			require.Equal(t, initial, selected, "another account reaching its threshold must not rotate this binding")
			second.finish(t, r, false)
			r, selected = second.start(t, "astra")
			require.NotEqual(t, initial, selected, "another account rebinding must not erase this account's 19 failures")
			codexFinish(t, second.allocator, r, "canceled")
		})
	}
}
