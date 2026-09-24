package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func accountImportDefaultTestService(t *testing.T, settings AccountImportSettings) (*adminServiceImpl, *importOptionsAccountRepo) {
	t.Helper()
	settingService, _ := accountImportSettingsService(t, settings)
	repo := &importOptionsAccountRepo{}
	return &adminServiceImpl{accountRepo: repo, settingService: settingService}, repo
}

func accountImportDefaultTestInput() *CreateAccountInput {
	input := importOptionsInput()
	input.SkipImportDefaults = false
	return input
}

func TestAccountImportDefaultsCreateAppliesIndependentDefaults(t *testing.T) {
	settings := DefaultAccountImportSettings()
	settings.Enabled, settings.ProxyMode = true, "random"
	settings.CodexTicketEnabled = false
	settings.Extra = map[string]any{
		RandomProxyPoolScopeExtraKey: "all", RandomProxyEmptyPoolPolicyExtraKey: "reject",
		ProxyRegionModeExtraKey: "manual", ProxyRegionCountryExtraKey: "JP",
		CodexTicketProxyModeExtraKey: "account", CodexTicketProxyStrategyExtraKey: "round_robin",
	}
	svc, repo := accountImportDefaultTestService(t, settings)
	input := accountImportDefaultTestInput()
	account, err := svc.CreateAccount(context.Background(), input)
	require.NoError(t, err)
	require.Same(t, account, repo.created)
	require.True(t, account.AntiDegradationEnabled())
	require.False(t, OpenAICodexTicketAccountEnabled(account))
	require.True(t, account.IsRandomProxy())
	require.Nil(t, account.ProxyID)
	require.Equal(t, "JP", account.Extra[ProxyRegionCountryExtraKey])
	require.Equal(t, "account", account.CodexTicketProxyMode())
	require.Nil(t, input.Extra)
	require.Nil(t, input.ProtectionEnabled)
	require.Nil(t, input.CodexTicketEnabled)
}

func TestAccountImportDefaultsPreserveExplicitFalseAndZero(t *testing.T) {
	settings := DefaultAccountImportSettings()
	settings.Enabled, settings.ProxyMode = true, "fixed"
	id := int64(7)
	settings.ProxyID = &id
	for _, viaExtra := range []bool{false, true} {
		svc, _ := accountImportDefaultTestService(t, settings)
		input := accountImportDefaultTestInput()
		zero, disabled := int64(0), false
		input.ProxyID = &zero
		if viaExtra {
			input.Extra = map[string]any{AntiDegradationExtraKey: false, OpenAICodexTicketEnabledExtraKey: false}
		} else {
			input.ProtectionEnabled, input.CodexTicketEnabled = &disabled, &disabled
		}
		account, err := svc.CreateAccount(context.Background(), input)
		require.NoError(t, err, "显式直连不应查询未使用的默认固定代理")
		require.Nil(t, account.ProxyID)
		require.False(t, account.AntiDegradationEnabled())
		require.False(t, OpenAICodexTicketAccountEnabled(account))
		require.NotNil(t, input.ProxyID)
		require.Zero(t, *input.ProxyID)
	}
}

func TestAccountImportDefaultsRoutingAndRegionFamiliesAreIndependent(t *testing.T) {
	settings := DefaultAccountImportSettings()
	settings.Enabled, settings.ProxyMode = true, "random"
	settings.Extra = map[string]any{
		RandomProxyPoolScopeExtraKey: "group", RandomProxyGroupIDExtraKey: int64(9),
		ProxyRegionModeExtraKey: "manual", ProxyRegionCountryExtraKey: "JP",
		CodexTicketProxyModeExtraKey: "fixed", CodexTicketProxyIDExtraKey: int64(19),
	}
	svc, _ := accountImportDefaultTestService(t, settings)
	input := accountImportDefaultTestInput()
	input.Extra = map[string]any{
		ProxyModeExtraKey: "random", RandomProxyPoolScopeExtraKey: "all",
		ProxyRegionModeExtraKey: "off", CodexTicketProxyModeExtraKey: "account",
	}
	account, err := svc.CreateAccount(context.Background(), input)
	require.NoError(t, err)
	require.True(t, account.IsRandomProxy())
	require.NotContains(t, account.Extra, RandomProxyGroupIDExtraKey)
	require.NotContains(t, account.Extra, ProxyRegionCountryExtraKey)
	require.Equal(t, "account", account.CodexTicketProxyMode())
	require.Zero(t, account.CodexTicketProxyID())

	input = accountImportDefaultTestInput()
	input.Extra = map[string]any{RandomProxyMaxReuseMinutesExtraKey: int64(5)}
	prepared, err := svc.applyAccountImportDefaults(context.Background(), input)
	require.NoError(t, err)
	require.NotContains(t, prepared.Extra, ProxyModeExtraKey, "任意路由族字段都代表显式配置")
	require.Equal(t, "JP", prepared.Extra[ProxyRegionCountryExtraKey])
}

func TestAccountImportDefaultsBillingRegionUsesFallbackOnlyWhenBillingIsUnknown(t *testing.T) {
	settings := DefaultAccountImportSettings()
	settings.Enabled, settings.ProxyMode = true, "random"
	settings.Extra = map[string]any{
		ProxyRegionModeExtraKey:            "billing",
		ProxyRegionFallbackCountryExtraKey: "JP",
	}
	svc, _ := accountImportDefaultTestService(t, settings)

	input := accountImportDefaultTestInput()
	input.Credentials["billing_currency"] = "USD"
	account, err := svc.CreateAccount(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, "JP", account.Extra[ProxyRegionFallbackCountryExtraKey])
	country, err := account.ProxyRegionCountry()
	require.NoError(t, err)
	require.Equal(t, "JP", country)

	input = accountImportDefaultTestInput()
	input.Credentials["billing_currency"] = "JPY"
	account, err = svc.CreateAccount(context.Background(), input)
	require.NoError(t, err)
	country, err = account.ProxyRegionCountry()
	require.NoError(t, err)
	require.Equal(t, "JP", country, "可识别账单地区优先于导入默认兜底")

	input = accountImportDefaultTestInput()
	input.Credentials["price_country"] = "PH"
	input.Credentials["billing_currency"] = "USD"
	account, err = svc.CreateAccount(context.Background(), input)
	require.NoError(t, err)
	country, err = account.ProxyRegionCountry()
	require.NoError(t, err)
	require.Equal(t, "PH", country, "price_country 优先于币种和导入默认兜底")
}

func TestAccountImportDefaultsDisabledAndSkippedPreserveBehavior(t *testing.T) {
	for _, skip := range []bool{false, true} {
		settings := DefaultAccountImportSettings()
		settings.Enabled = skip
		svc, _ := accountImportDefaultTestService(t, settings)
		input := accountImportDefaultTestInput()
		input.SkipImportDefaults = skip
		zero := int64(0)
		input.ProxyID = &zero
		if skip {
			svc.settingService.settingRepo = &accountImportSettingsRepoStub{readErr: errors.New("must not read")}
		}
		account, err := svc.CreateAccount(context.Background(), input)
		require.NoError(t, err)
		require.Nil(t, account.ProxyID)
		require.NotContains(t, account.Extra, AntiDegradationExtraKey)
		require.NotContains(t, account.Extra, OpenAICodexTicketEnabledExtraKey)
	}
}

func TestAccountImportDefaultsUnsupportedAccountsDoNotReceiveTicketSettings(t *testing.T) {
	settings := DefaultAccountImportSettings()
	settings.Enabled = true
	settings.Extra = map[string]any{CodexTicketProxyModeExtraKey: "fixed", CodexTicketProxyIDExtraKey: int64(99)}
	for _, platform := range []string{PlatformAnthropic, PlatformGemini, PlatformOpenAI} {
		svc, _ := accountImportDefaultTestService(t, settings)
		input := accountImportDefaultTestInput()
		input.Platform, input.Type = platform, AccountTypeAPIKey
		account, err := svc.CreateAccount(context.Background(), input)
		require.NoError(t, err)
		require.NotContains(t, account.Extra, OpenAICodexTicketEnabledExtraKey)
		require.NotContains(t, account.Extra, CodexTicketProxyModeExtraKey)
		require.NotContains(t, account.Extra, CodexTicketProxyIDExtraKey)
	}
}

func TestAccountImportDefaultsBadSettingsDoNotPersist(t *testing.T) {
	for _, failure := range []bool{false, true} {
		svc, repo := accountImportDefaultTestService(t, DefaultAccountImportSettings())
		settingsRepo := &accountImportSettingsRepoStub{raw: "not JSON"}
		if failure {
			settingsRepo.readErr = errors.New("database unavailable")
		}
		svc.settingService.settingRepo = settingsRepo
		_, err := svc.CreateAccount(context.Background(), accountImportDefaultTestInput())
		require.Error(t, err)
		require.Nil(t, repo.created)
	}
}

func TestAccountImportDefaultsRevalidatesFixedAndGroupReferencesAtCreation(t *testing.T) {
	settings := DefaultAccountImportSettings()
	settings.Enabled, settings.ProxyMode = true, "fixed"
	id := int64(7)
	settings.ProxyID = &id
	svc, repo := accountImportDefaultTestService(t, settings)
	past := time.Now().Add(-time.Minute)
	svc.proxyRepo = &accountImportProxyRepoStub{proxy: &Proxy{ID: id, Status: StatusActive, ExpiresAt: &past}}
	_, err := svc.CreateAccount(context.Background(), accountImportDefaultTestInput())
	require.Error(t, err)
	require.Nil(t, repo.created)

	settings.ProxyMode, settings.ProxyID = "random", nil
	settings.Extra = map[string]any{RandomProxyPoolScopeExtraKey: "group", RandomProxyGroupIDExtraKey: id}
	svc, repo = accountImportDefaultTestService(t, settings)
	svc.proxyRepo = &accountImportProxyRepoStub{}
	_, err = svc.CreateAccount(context.Background(), accountImportDefaultTestInput())
	require.Error(t, err)
	require.Nil(t, repo.created)
}

func TestAccountImportDefaultsDoesNotMutateCallerMaps(t *testing.T) {
	settings := DefaultAccountImportSettings()
	svc, _ := accountImportDefaultTestService(t, settings)
	input := accountImportDefaultTestInput()
	input.Credentials["header_overrides"] = map[string]any{"X-Client": "test"}
	input.Extra = map[string]any{"custom": map[string]any{"items": []any{"original"}}}
	account, err := svc.CreateAccount(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"X-Client": "test"}, input.Credentials["header_overrides"])
	account.Extra["custom"].(map[string]any)["items"].([]any)[0] = "changed"
	require.Equal(t, "original", input.Extra["custom"].(map[string]any)["items"].([]any)[0])
}

func TestAccountImportDefaultsTicketCredentialAndProxyAreIndependent(t *testing.T) {
	settings := DefaultAccountImportSettings()
	settings.Enabled = true
	settings.Extra = map[string]any{
		CodexTicketProxyModeExtraKey: "random", CodexTicketProxyStrategyExtraKey: "round_robin",
		CodexTicketCredentialPolicyExtraKey: map[string]any{"mode": "cookie_state"},
	}
	svc, _ := accountImportDefaultTestService(t, settings)
	input := accountImportDefaultTestInput()
	input.Extra = map[string]any{CodexTicketCredentialPolicyExtraKey: map[string]any{"mode": "inherit"}}
	account, err := svc.CreateAccount(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, "random", account.CodexTicketProxyMode())
	require.Equal(t, "round_robin", account.CodexTicketProxyStrategy())
	require.Equal(t, map[string]any{"mode": "inherit"}, account.Extra[CodexTicketCredentialPolicyExtraKey])
	input.Extra = map[string]any{CodexTicketProxyModeExtraKey: "account"}
	account, err = svc.CreateAccount(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, "account", account.CodexTicketProxyMode())
	require.Equal(t, map[string]any{"mode": "cookie_state"}, account.Extra[CodexTicketCredentialPolicyExtraKey])
}

func TestAccountImportDefaultsFixedProxyAndDirectModes(t *testing.T) {
	for _, mode := range []string{"fixed", "direct", "preserve"} {
		t.Run(mode, func(t *testing.T) {
			settings := DefaultAccountImportSettings()
			settings.Enabled, settings.ProxyMode = true, mode
			id := int64(7)
			if mode == "fixed" {
				settings.ProxyID = &id
			}
			svc, _ := accountImportDefaultTestService(t, settings)
			svc.proxyRepo = &accountImportProxyRepoStub{proxy: &Proxy{ID: id, Status: StatusActive}}
			account, err := svc.CreateAccount(context.Background(), accountImportDefaultTestInput())
			require.NoError(t, err)
			if mode == "fixed" {
				require.Equal(t, &id, account.ProxyID)
			} else {
				require.Nil(t, account.ProxyID)
			}
			input := accountImportDefaultTestInput()
			explicitID := int64(21)
			input.ProxyID = &explicitID
			account, err = svc.CreateAccount(context.Background(), input)
			require.NoError(t, err)
			require.Equal(t, &explicitID, account.ProxyID)
		})
	}
}
