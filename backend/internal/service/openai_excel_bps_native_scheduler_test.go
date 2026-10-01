package service

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

const bpsMixedTestGroupID int64 = 27131

type bpsMixedSchedulerFixture struct {
	service     *OpenAIGatewayService
	cache       *schedulerTestGatewayCache
	accounts    []Account
	concurrency schedulerTestConcurrencyCache
}

func newBPSMixedSchedulerFixture(t *testing.T, advanced, loadBatch bool) bpsMixedSchedulerFixture {
	t.Helper()
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	accounts := make([]Account, 2)
	for i := range accounts {
		accounts[i] = Account{ID: int64(271310 + i), Platform: PlatformOpenAI, Type: AccountTypeOAuth,
			Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{bpsMixedTestGroupID},
			Credentials: map[string]any{"access_token": "token", "chatgpt_account_id": "chatgpt"},
			Extra:       map[string]any{"openai_excel_bps": i == 0, "openai_excel_bps_models": []any{"gpt-6-astra"}}}
	}
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{}}
	concurrency := schedulerTestConcurrencyCache{acquireResults: map[int64]bool{}, loadMap: map[int64]*AccountLoadInfo{}}
	cfg := newSchedulerTestSubscriptionPriorityConfig()
	cfg.Gateway.Scheduling.LoadBatchEnabled = loadBatch
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, cache: cache,
		cfg: cfg, rateLimitService: newOpenAIAdvancedSchedulerRateLimitService(strconv.FormatBool(advanced)),
		concurrencyService: NewConcurrencyService(concurrency)}
	require.Equal(t, advanced, svc.isOpenAIAdvancedSchedulerEnabled(context.Background()))
	return bpsMixedSchedulerFixture{service: svc, cache: cache, accounts: accounts, concurrency: concurrency}
}

func selectBPSMixedAccount(t *testing.T, fixture bpsMixedSchedulerFixture, ctx context.Context, session string) *AccountSelectionResult {
	t.Helper()
	groupID := bpsMixedTestGroupID
	selection, _, err := fixture.service.SelectAccountWithScheduler(ctx, &groupID, "", session, "gpt-6-astra", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	if selection.ReleaseFunc != nil {
		t.Cleanup(selection.ReleaseFunc)
	}
	return selection
}

func bpsMixedRoutingCases() []struct {
	name string
	body []byte
} {
	return []struct {
		name string
		body []byte
	}{
		{"native_ingress", nil},
		{"hosted_tools", []byte(`{"tools":[{"type":"web_search","external_web_access":true}]}`)},
		{"plain_responses", []byte(`{"input":"plain response"}`)},
	}
}

func TestExcelBPSMixedPoolUsesIdleNativeAccount(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		for _, route := range bpsMixedRoutingCases() {
			t.Run(fmt.Sprintf("advanced=%t/%s", advanced, route.name), func(t *testing.T) {
				f := newBPSMixedSchedulerFixture(t, advanced, true)
				busy, idle := f.accounts[0].ID, f.accounts[1].ID
				f.concurrency.acquireResults[busy] = false
				f.concurrency.loadMap[busy] = &AccountLoadInfo{AccountID: busy, LoadRate: 100}
				f.concurrency.loadMap[idle] = &AccountLoadInfo{AccountID: idle, LoadRate: 0}
				selection := selectBPSMixedAccount(t, f, WithOpenAIExcelBPSRouting(context.Background(), route.body), "")
				require.Equal(t, idle, selection.Account.ID)
				require.True(t, selection.Acquired, "an idle native account must avoid the busy BPS wait queue")
				require.Nil(t, selection.WaitPlan)
			})
		}
	}
}

func TestExcelBPSMixedPoolPreservesNativePriority(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		for _, loadBatch := range []bool{false, true} {
			t.Run(fmt.Sprintf("advanced=%t/load_batch=%t", advanced, loadBatch), func(t *testing.T) {
				f := newBPSMixedSchedulerFixture(t, advanced, loadBatch)
				f.accounts[0].Priority = 100
				f.accounts[1].Priority = 0
				selection := selectBPSMixedAccount(t, f, WithOpenAIExcelBPSRouting(context.Background(), nil), "")
				require.Equal(t, f.accounts[1].ID, selection.Account.ID, "enabling BPS must not outrank administrator priority")
			})
		}
	}
}

func TestExcelBPSMixedPoolPreservesNativeSticky(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		for _, loadBatch := range []bool{false, true} {
			for _, route := range bpsMixedRoutingCases() {
				t.Run(fmt.Sprintf("advanced=%t/load_batch=%t/%s", advanced, loadBatch, route.name), func(t *testing.T) {
					f := newBPSMixedSchedulerFixture(t, advanced, loadBatch)
					const session = "bps_mixed_native_sticky"
					const key = "openai:" + session
					f.cache.sessionBindings[key] = f.accounts[1].ID
					selection := selectBPSMixedAccount(t, f, WithOpenAIExcelBPSRouting(context.Background(), route.body), session)
					require.Equal(t, f.accounts[1].ID, selection.Account.ID)
					require.Equal(t, f.accounts[1].ID, f.cache.sessionBindings[key])
					require.Zero(t, f.cache.deletedSessions[key], "a BPS peer must not invalidate a valid native binding")
				})
			}
		}
	}
}

func TestExcelBPSCapabilityOverrideRequiresConfirmedBridge(t *testing.T) {
	account := excelAccount()
	account.Credentials[openAIEndpointCapabilitiesCredentialKey] = map[string]any{"chat_completions": false}
	for _, route := range bpsMixedRoutingCases() {
		t.Run(route.name, func(t *testing.T) {
			ctx := WithOpenAIExcelBPSRouting(context.Background(), route.body)
			for _, capability := range []OpenAIEndpointCapability{OpenAIEndpointCapabilityResponses, OpenAIEndpointCapabilityResponsesCompact} {
				require.Equal(t, route.name == "plain_responses", accountSupportsOpenAICapabilitiesForRequest(ctx, account, "gpt-6-astra", capability, ""))
			}
			require.False(t, accountSupportsOpenAICapabilitiesForRequest(ctx, account, "gpt-6-astra", OpenAIEndpointCapabilityChatCompletions, ""))
		})
	}
	for _, ctx := range []context.Context{nil, context.Background()} {
		require.False(t, accountSupportsOpenAICapabilitiesForRequest(ctx, account, "gpt-6-astra", OpenAIEndpointCapabilityResponses, ""))
	}
}

func TestExcelBPSNativeCapabilityRestrictionSurvivesScheduling(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		for _, loadBatch := range []bool{false, true} {
			t.Run(fmt.Sprintf("advanced=%t/load_batch=%t", advanced, loadBatch), func(t *testing.T) {
				f := newBPSMixedSchedulerFixture(t, advanced, loadBatch)
				f.accounts[0].Credentials[openAIEndpointCapabilitiesCredentialKey] = map[string]any{"chat_completions": false}
				f.accounts[1].Priority = 100
				groupID := bpsMixedTestGroupID
				ctx := WithOpenAIExcelBPSRouting(context.Background(), nil)
				selection, _, err := f.service.SelectAccountWithSchedulerForCapability(ctx, &groupID, "", "", "gpt-6-astra", nil,
					OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponses, false, false, true, PlatformOpenAI)
				require.NoError(t, err)
				require.NotNil(t, selection)
				require.NotNil(t, selection.Account)
				if selection.ReleaseFunc != nil {
					t.Cleanup(selection.ReleaseFunc)
				}
				require.Equal(t, f.accounts[1].ID, selection.Account.ID, "BPS configuration must not bypass a denied native capability")
			})
		}
	}
}

func TestExcelBPSRoutingMarkerDistinguishesUnspecifiedAndNative(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := excelAccount()
	ctx := context.Background()
	svc.coolDownExcelBPS(ctx, account, "60")
	require.True(t, svc.isExcelBPSCoolingDownContext(ctx, account, "gpt-6-astra"), "legacy unspecified context keeps the model-scoped cooldown")
	nativeCtx := WithOpenAIExcelBPSRouting(ctx, nil)
	require.False(t, svc.isExcelBPSCoolingDownContext(nativeCtx, account, "gpt-6-astra"))
	hostedCtx := WithOpenAIExcelBPSRouting(ctx, []byte(`{"tools":[{"type":"web_search","external_web_access":true}]}`))
	require.False(t, svc.isExcelBPSCoolingDownContext(hostedCtx, account, "gpt-6-astra"))
	bpsCtx := WithOpenAIExcelBPSRouting(nativeCtx, []byte(`{"input":"plain response"}`))
	require.True(t, svc.isExcelBPSCoolingDownContext(bpsCtx, account, "gpt-6-astra"), "confirmed BPS intent replaces a previous native marker")
	account.Credentials[openAIEndpointCapabilitiesCredentialKey] = map[string]any{"chat_completions": false}
	for _, unconfirmed := range []context.Context{ctx, nativeCtx, hostedCtx} {
		require.False(t, accountSupportsOpenAICapabilitiesForRequest(unconfirmed, account, "gpt-6-astra", OpenAIEndpointCapabilityResponses, ""))
	}
	require.True(t, accountSupportsOpenAICapabilitiesForRequest(bpsCtx, account, "gpt-6-astra", OpenAIEndpointCapabilityResponses, ""))
}
