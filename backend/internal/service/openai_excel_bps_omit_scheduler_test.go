package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func forEachOmitToolsSchedulerMode(t *testing.T, check func(*testing.T, bpsMixedSchedulerFixture)) {
	t.Helper()
	for _, advanced := range []bool{false, true} {
		for _, loadBatch := range []bool{false, true} {
			t.Run(fmt.Sprintf("advanced=%t/load_batch=%t", advanced, loadBatch), func(t *testing.T) {
				check(t, newBPSMixedSchedulerFixture(t, advanced, loadBatch))
			})
		}
	}
}

func coolOmitToolsMixedPool(f bpsMixedSchedulerFixture) context.Context {
	ctx := WithOpenAIExcelBPSRouting(context.Background(), []byte(`{"tools":[{"type":"web_search","external_web_access":true}]}`))
	for i := range f.accounts {
		f.accounts[i].Extra["openai_excel_bps"] = true
		f.accounts[i].Extra[ExcelBPSOmitUnsupportedToolsKey] = i == 0
		f.accounts[i].Priority = i * 100
		f.service.coolDownExcelBPS(ctx, &f.accounts[i], "60")
	}
	return ctx
}

func TestExcelBPSOmitToolsMixedPoolCooldown(t *testing.T) {
	forEachOmitToolsSchedulerMode(t, func(t *testing.T, f bpsMixedSchedulerFixture) {
		ctx := coolOmitToolsMixedPool(f)
		selection := selectBPSMixedAccount(t, f, ctx, "")
		require.Equal(t, f.accounts[1].ID, selection.Account.ID, "only the opted-in BPS route is cooling down")
		require.True(t, selection.Acquired)
		require.Nil(t, selection.WaitPlan)
	})
}

func TestExcelBPSOmitToolsMixedPoolSticky(t *testing.T) {
	for _, stickyIndex := range []int{0, 1} {
		t.Run(fmt.Sprintf("sticky_account=%d", stickyIndex), func(t *testing.T) {
			forEachOmitToolsSchedulerMode(t, func(t *testing.T, f bpsMixedSchedulerFixture) {
				ctx := coolOmitToolsMixedPool(f)
				const session = "bps_omit_tools_sticky"
				const key = "openai:" + session
				f.cache.sessionBindings[key] = f.accounts[stickyIndex].ID
				selection := selectBPSMixedAccount(t, f, ctx, session)
				require.Equal(t, f.accounts[1].ID, selection.Account.ID)
				require.Equal(t, f.accounts[1].ID, f.cache.sessionBindings[key])
				if stickyIndex == 1 {
					require.Zero(t, f.cache.deletedSessions[key], "native routing must preserve its valid binding")
				}
			})
		})
	}
}

func TestExcelBPSOmitToolsBPSOnlyCapabilitySelection(t *testing.T) {
	for _, omit := range []bool{false, true} {
		t.Run(fmt.Sprintf("omit=%t", omit), func(t *testing.T) {
			forEachOmitToolsSchedulerMode(t, func(t *testing.T, f bpsMixedSchedulerFixture) {
				f.accounts[0].Extra[ExcelBPSOmitUnsupportedToolsKey] = omit
				f.accounts[0].Credentials[openAIEndpointCapabilitiesCredentialKey] = map[string]any{"chat_completions": false}
				f.accounts[1].Priority = 100
				want := f.accounts[1].ID
				if omit {
					want = f.accounts[0].ID
				}
				ctx := WithOpenAIExcelBPSRouting(context.Background(), []byte(`{"tools":[{"type":"web_search","external_web_access":true}]}`))
				groupID := bpsMixedTestGroupID
				checks := []struct {
					capability     OpenAIEndpointCapability
					requireCompact bool
				}{
					{OpenAIEndpointCapabilityResponses, false},
					{OpenAIEndpointCapabilityResponsesCompact, false},
					{OpenAIEndpointCapabilityResponsesCompact, true},
				}
				for _, check := range checks {
					selection, _, err := f.service.SelectAccountWithSchedulerForCapability(ctx, &groupID, "", "", "gpt-6-astra", nil,
						OpenAIUpstreamTransportAny, check.capability, check.requireCompact, false, true, PlatformOpenAI)
					require.NoError(t, err, "capability=%s requireCompact=%t", check.capability, check.requireCompact)
					require.NotNil(t, selection)
					if selection.ReleaseFunc != nil {
						selection.ReleaseFunc()
					}
					require.NotNil(t, selection.Account)
					require.Equal(t, want, selection.Account.ID, "capability=%s requireCompact=%t", check.capability, check.requireCompact)
				}
			})
		})
	}
}

func TestExcelBPSOmitToolsPreservesCompactRestrictions(t *testing.T) {
	forEachOmitToolsSchedulerMode(t, func(t *testing.T, f bpsMixedSchedulerFixture) {
		f.accounts[0].Extra[ExcelBPSOmitUnsupportedToolsKey] = true
		f.accounts[0].Credentials[openAIEndpointCapabilitiesCredentialKey] = map[string]any{"chat_completions": false}
		f.accounts[1].Priority = 100
		ctx := WithOpenAIExcelBPSRouting(context.Background(), []byte(`{"tools":[{"type":"web_search","external_web_access":true}]}`))
		groupID := bpsMixedTestGroupID
		for _, restriction := range []struct {
			mode      string
			supported bool
		}{
			{OpenAICompactModeForceOff, true},
			{"", false},
		} {
			f.accounts[0].Extra["openai_compact_mode"] = restriction.mode
			f.accounts[0].Extra["openai_compact_supported"] = restriction.supported
			selection, _, err := f.service.SelectAccountWithSchedulerForCapability(ctx, &groupID, "", "", "gpt-6-astra", nil,
				OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponsesCompact, true, false, true, PlatformOpenAI)
			require.NoError(t, err)
			require.NotNil(t, selection)
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			require.NotNil(t, selection.Account)
			require.Equal(t, f.accounts[1].ID, selection.Account.ID,
				"omit must respect compact mode=%q supported=%t", restriction.mode, restriction.supported)
		}
	})
}
