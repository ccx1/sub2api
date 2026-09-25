//go:build unit

package service

import (
	"context"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func excelBPSImportSettings() AccountImportSettings {
	settings := DefaultAccountImportSettings()
	settings.Enabled, settings.ExcelBPSEnabled = true, true
	settings.Extra = map[string]any{CodexTicketProxyModeExtraKey: "account"}
	return settings
}

func TestAccountImportDefaultsExcelBPSOnlyForEligibleOAuthAndSkipsTicketDefaults(t *testing.T) {
	svc, _ := accountImportDefaultTestService(t, excelBPSImportSettings())
	input := accountImportDefaultTestInput()
	input.Type = AccountTypeOAuth
	account, err := svc.CreateAccount(context.Background(), input)
	require.NoError(t, err)
	require.True(t, account.IsExcelBPSEnabled())
	require.NotContains(t, account.Extra, OpenAICodexTicketEnabledExtraKey)
	require.NotContains(t, account.Extra, CodexTicketProxyModeExtraKey)
	require.False(t, isOpenAICodexTicketAccount(account))
	require.Nil(t, input.Extra, "默认值不能写回调用方输入")

	for name, mutate := range map[string]func(*CreateAccountInput){
		"setup-token": func(in *CreateAccountInput) {},
		"apikey":      func(in *CreateAccountInput) { in.Type = AccountTypeAPIKey },
		"anthropic":   func(in *CreateAccountInput) { in.Platform, in.Type = PlatformAnthropic, AccountTypeOAuth },
		"pat": func(in *CreateAccountInput) {
			in.Type = AccountTypeOAuth
			in.Credentials = map[string]any{"access_token": "test-token", "auth_mode": "personal_access_token"}
		},
		"agent-identity": func(in *CreateAccountInput) {
			in.Type = AccountTypeOAuth
			in.Credentials = map[string]any{"access_token": "test-token", "auth_mode": OpenAIAuthModeAgentIdentity}
		},
	} {
		t.Run(name, func(t *testing.T) {
			svc, _ := accountImportDefaultTestService(t, excelBPSImportSettings())
			input := accountImportDefaultTestInput()
			mutate(input)
			account, err := svc.CreateAccount(context.Background(), input)
			require.NoError(t, err)
			require.NotContains(t, account.Extra, excelBPSExtraKey)
		})
	}
}

func TestAccountImportDefaultsExcelBPSKeepsExplicitValueAndDisabledSetting(t *testing.T) {
	svc, _ := accountImportDefaultTestService(t, excelBPSImportSettings())
	input := accountImportDefaultTestInput()
	input.Type, input.Extra = AccountTypeOAuth, map[string]any{excelBPSExtraKey: false}
	account, err := svc.CreateAccount(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, false, account.Extra[excelBPSExtraKey])
	require.True(t, isOpenAICodexTicketAccount(account))
	require.Equal(t, "account", account.Extra[CodexTicketProxyModeExtraKey])

	settings := excelBPSImportSettings()
	settings.ExcelBPSEnabled = false
	svc, _ = accountImportDefaultTestService(t, settings)
	input = accountImportDefaultTestInput()
	input.Type = AccountTypeOAuth
	account, err = svc.CreateAccount(context.Background(), input)
	require.NoError(t, err)
	require.NotContains(t, account.Extra, excelBPSExtraKey)
	require.True(t, OpenAICodexTicketAccountEnabled(account))
}

func TestAccountImportSettingsExcelBPSRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo := &accountImportSettingsRepoStub{}
	svc := &SettingService{settingRepo: repo}
	settings, err := svc.GetAccountImportSettings(ctx)
	require.NoError(t, err)
	require.False(t, settings.ExcelBPSEnabled)
	settings.ExcelBPSEnabled = true
	_, err = svc.UpdateAccountImportSettings(ctx, *settings)
	require.NoError(t, err)
	reread, err := svc.GetAccountImportSettings(ctx)
	require.NoError(t, err)
	require.True(t, reread.ExcelBPSEnabled)

	legacy := &accountImportSettingsRepoStub{raw: `{"enabled":true,"protection_enabled":true,"codex_ticket_enabled":true,"proxy_mode":"preserve","proxy_id":null,"extra":{}}`}
	old, err := (&SettingService{settingRepo: legacy}).GetAccountImportSettings(ctx)
	require.NoError(t, err, "旧配置缺少 excel_bps_enabled 时应按关闭读取")
	require.False(t, old.ExcelBPSEnabled)
}

func TestSharedPoolCreateExcelBPSSetsFlagAndRejectsIneligible(t *testing.T) {
	enabled, disabled := true, false
	for _, ticket := range []*bool{nil, &enabled} {
		in := SharedPoolAccountInput{Name: "mine", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 3,
			Credentials: map[string]any{"access_token": "test-token", "plan_type": "pro"}, ExcelBPSEnabled: &enabled, CodexTicketEnabled: ticket}
		accounts := &sharedTicketAccounts{}
		s := &SharedPoolService{repo: &sharedTicketCreateRepo{accounts: accounts}, accounts: accounts, earnings: sharedPoolTotalsStub{}}
		view, err := s.Create(context.Background(), 7, in)
		require.NoError(t, err)
		require.True(t, accounts.account.IsExcelBPSEnabled())
		require.NotContains(t, accounts.account.Extra, OpenAICodexTicketEnabledExtraKey, "BPS 账号即使是 Pro 也不应强制打票")
		require.Nil(t, view.CodexTicketEnabled)
	}

	in := SharedPoolAccountInput{Name: "mine", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 3,
		Credentials: map[string]any{"access_token": "test-token"}, ExcelBPSEnabled: &disabled}
	accounts := &sharedTicketAccounts{}
	s := &SharedPoolService{repo: &sharedTicketCreateRepo{accounts: accounts}, accounts: accounts, earnings: sharedPoolTotalsStub{}}
	_, err := s.Create(context.Background(), 7, in)
	require.NoError(t, err)
	require.NotContains(t, accounts.account.Extra, excelBPSExtraKey)

	for _, account := range []SharedPoolAccountInput{
		{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-test"}},
		{Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "test-token"}},
	} {
		account.Name, account.Concurrency, account.ExcelBPSEnabled = "mine", 3, &enabled
		s := &SharedPoolService{repo: &sharedPoolRepoStub{}}
		_, err := s.Create(context.Background(), 7, account)
		require.Equal(t, "EXCEL_BPS_UNSUPPORTED_ACCOUNT", infraerrors.Reason(err))
	}
}

func TestSharedPoolUpdateRejectsExcelBPSChange(t *testing.T) {
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{}}
	s, repo := newSharedTicketService(account)
	for _, value := range []bool{false, true} {
		in := SharedPoolAccountInput{Name: "mine", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 2, ExcelBPSEnabled: &value}
		_, err := s.Update(context.Background(), 7, 1, in)
		require.Equal(t, "SHARED_EXCEL_BPS_CREATE_ONLY", infraerrors.Reason(err))
	}
	require.Zero(t, repo.writes)
	require.NotContains(t, account.Extra, excelBPSExtraKey)
}
