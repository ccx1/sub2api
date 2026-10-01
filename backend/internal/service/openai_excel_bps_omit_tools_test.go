package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

const omitToolsTestKey = "openai_excel_bps_omit_unsupported_tools"

func TestExcelBPSOmitToolsRequiresExplicitOptIn(t *testing.T) {
	var missing *Account
	require.False(t, missing.IsExcelBPSOmitUnsupportedToolsEnabled())
	account := excelAccount()
	require.False(t, account.IsExcelBPSOmitUnsupportedToolsEnabled())
	account.Extra[omitToolsTestKey] = true
	require.True(t, account.IsExcelBPSOmitUnsupportedToolsEnabled())
	for _, mutate := range []func(*Account){
		func(a *Account) { a.Extra["openai_excel_bps"] = false },
		func(a *Account) { a.Platform = PlatformAnthropic },
		func(a *Account) { a.Type = AccountTypeAPIKey },
		func(a *Account) { id := int64(100); a.ParentAccountID = &id },
	} {
		a := excelAccount()
		a.Extra[omitToolsTestKey] = true
		mutate(a)
		require.False(t, a.IsExcelBPSOmitUnsupportedToolsEnabled())
	}
}

func TestExcelBPSOmitToolsRoutingUsesSelectedAccount(t *testing.T) {
	body := []byte(`{"tools":[{"type":"web_search","external_web_access":true}]}`)
	ctx := WithOpenAIExcelBPSRouting(context.Background(), body)
	svc := &OpenAIGatewayService{}
	for _, raw := range []any{nil, false, "true", 1, true} {
		t.Run(fmt.Sprintf("flag=%v/%T", raw, raw), func(t *testing.T) {
			account := excelAccount()
			account.Extra[omitToolsTestKey] = raw
			account.Credentials[openAIEndpointCapabilitiesCredentialKey] = map[string]any{"chat_completions": false}
			svc.coolDownExcelBPS(ctx, account, "60")
			enabled, _ := raw.(bool)
			for _, candidateCtx := range []context.Context{ctx, context.WithoutCancel(ctx)} {
				require.Equal(t, enabled, svc.isExcelBPSCoolingDownContext(candidateCtx, account, "gpt-6-astra"))
				require.Equal(t, enabled, accountSupportsOpenAICapabilitiesForRequest(candidateCtx, account, "gpt-6-astra", OpenAIEndpointCapabilityResponses, ""))
				require.Equal(t, enabled, accountSupportsOpenAICapabilitiesForRequest(candidateCtx, account, "gpt-6-astra", OpenAIEndpointCapabilityResponsesCompact, ""))
				require.False(t, accountSupportsOpenAICapabilitiesForRequest(candidateCtx, account, "gpt-6-astra", OpenAIEndpointCapabilityChatCompletions, ""))
			}
			native := WithOpenAIExcelBPSRouting(ctx, nil)
			require.False(t, svc.isExcelBPSCoolingDownContext(native, account, "gpt-6-astra"))
			require.False(t, accountSupportsOpenAICapabilitiesForRequest(native, account, "gpt-6-astra", OpenAIEndpointCapabilityResponses, ""))
			account.Extra["openai_excel_bps_models"] = []string{"gpt-5.6-sol"}
			require.False(t, svc.isExcelBPSCoolingDownContext(ctx, account, "gpt-6-astra"))
			require.False(t, accountSupportsOpenAICapabilitiesForRequest(ctx, account, "gpt-6-astra", OpenAIEndpointCapabilityResponses, ""))
			account.Extra["openai_excel_bps_models"] = nil
			account.Extra["openai_excel_bps"] = false
			require.False(t, svc.isExcelBPSCoolingDownContext(ctx, account, "gpt-6-astra"))
		})
	}
}

func TestExcelBPSOmitToolsBulkValidation(t *testing.T) {
	for _, raw := range []any{nil, "true", 1, true, false} {
		t.Run(fmt.Sprintf("%v/%T", raw, raw), func(t *testing.T) {
			extra := map[string]any{omitToolsTestKey: raw}
			changed, err := normalizeBulkExcelBPSExtra(extra)
			require.True(t, changed)
			if _, ok := raw.(bool); ok {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	extra := map[string]any{"openai_excel_bps": false, omitToolsTestKey: true, "unrelated": "keep"}
	_, err := normalizeBulkExcelBPSExtra(extra)
	require.NoError(t, err)
	require.Equal(t, false, extra[omitToolsTestKey])
	require.Equal(t, "keep", extra["unrelated"])
}
