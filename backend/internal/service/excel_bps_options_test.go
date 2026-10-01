//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExcelBPSOptionsIgnoreEncryptedContentRoundTrip(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		options, err := normalizeExcelBPSOptions(ExcelBPSOptions{IgnoreEncryptedContent: enabled})
		require.NoError(t, err)
		extra := excelBPSExtra(options)
		if enabled {
			require.Equal(t, true, extra[ExcelBPSIgnoreEncryptedContentKey])
		} else {
			require.NotContains(t, extra, ExcelBPSIgnoreEncryptedContentKey)
		}
		account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: extra}
		require.Equal(t, enabled, account.IsExcelBPSIgnoreEncryptedContentEnabled())
		reread := ExcelBPSOptionsFromAccount(account)
		require.NotNil(t, reread)
		require.Equal(t, options, *reread)
		account.Extra[excelBPSExtraKey] = false
		require.False(t, account.IsExcelBPSIgnoreEncryptedContentEnabled())
		require.Nil(t, ExcelBPSOptionsFromAccount(account))
	}
}

func TestExcelBPSOptionsReplaceClearsIgnoreEncryptedContent(t *testing.T) {
	require.Contains(t, ExcelBPSExtraKeys(), ExcelBPSIgnoreEncryptedContentKey)
	for name, patch := range map[string]map[string]any{
		"disable BPS":    nil,
		"disable option": excelBPSExtra(ExcelBPSOptions{}),
	} {
		t.Run(name, func(t *testing.T) {
			extra := excelBPSExtra(ExcelBPSOptions{IgnoreEncryptedContent: true})
			extra["keep"] = "unchanged"
			replaceExcelBPSExtra(extra, patch)
			require.NotContains(t, extra, ExcelBPSIgnoreEncryptedContentKey)
			require.Equal(t, "unchanged", extra["keep"])
			if patch == nil {
				require.NotContains(t, extra, excelBPSExtraKey)
			} else {
				require.Equal(t, true, extra[excelBPSExtraKey])
			}
		})
	}
}

func TestAccountImportSettingsExcelBPSLegacyEncryptedContentDefaultsOff(t *testing.T) {
	repo := &accountImportSettingsRepoStub{raw: `{"enabled":true,"excel_bps_enabled":true,"excel_bps_options":{"auto_disable_on_403":true},"proxy_mode":"preserve"}`}
	settings, err := (&SettingService{settingRepo: repo}).GetAccountImportSettings(context.Background())
	require.NoError(t, err)
	require.True(t, settings.ExcelBPSEnabled)
	require.True(t, settings.ExcelBPSOptions.AutoDisableOn403)
	require.False(t, settings.ExcelBPSOptions.IgnoreEncryptedContent)
	require.False(t, settings.ExcelBPSOptions.OmitUnsupportedTools)
}

func TestAccountImportSettingsExcelBPSLegacyUnsupportedToolsDefaultsOff(t *testing.T) {
	repo := &accountImportSettingsRepoStub{raw: `{"enabled":true,"excel_bps_enabled":true,"excel_bps_options":{"ignore_encrypted_content":true},"proxy_mode":"preserve"}`}
	settings, err := (&SettingService{settingRepo: repo}).GetAccountImportSettings(context.Background())
	require.NoError(t, err)
	require.True(t, settings.ExcelBPSEnabled)
	require.True(t, settings.ExcelBPSOptions.IgnoreEncryptedContent)
	require.False(t, settings.ExcelBPSOptions.OmitUnsupportedTools)
}

func TestAccountImportSettingsExcelBPSOptionsContentFlagsAreIndependent(t *testing.T) {
	for _, tc := range []struct {
		name      string
		encrypted bool
		omitTools bool
	}{
		{"disabled", false, false}, {"only encrypted", true, false},
		{"only tools", false, true}, {"both", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := excelBPSImportSettings()
			settings.ExcelBPSOptions = ExcelBPSOptions{IgnoreEncryptedContent: tc.encrypted, OmitUnsupportedTools: tc.omitTools}
			svc := &SettingService{settingRepo: &accountImportSettingsRepoStub{}}
			saved, err := svc.UpdateAccountImportSettings(context.Background(), settings)
			require.NoError(t, err)
			reread, err := svc.GetAccountImportSettings(context.Background())
			require.NoError(t, err)
			require.Equal(t, settings.ExcelBPSOptions, saved.ExcelBPSOptions)
			require.Equal(t, settings.ExcelBPSOptions, reread.ExcelBPSOptions)
			wantExtra := map[string]any{excelBPSExtraKey: true}
			if tc.encrypted {
				wantExtra[ExcelBPSIgnoreEncryptedContentKey] = true
			}
			if tc.omitTools {
				wantExtra[ExcelBPSOmitUnsupportedToolsKey] = true
			}
			account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: excelBPSExtra(reread.ExcelBPSOptions)}
			require.Equal(t, wantExtra, account.Extra)
			require.Equal(t, tc.encrypted, account.IsExcelBPSIgnoreEncryptedContentEnabled())
			require.Equal(t, tc.omitTools, account.IsExcelBPSOmitUnsupportedToolsEnabled())
			require.Equal(t, &settings.ExcelBPSOptions, ExcelBPSOptionsFromAccount(account))
			account.Extra[excelBPSExtraKey] = false
			require.False(t, account.IsExcelBPSIgnoreEncryptedContentEnabled())
			require.False(t, account.IsExcelBPSOmitUnsupportedToolsEnabled())
			require.Nil(t, ExcelBPSOptionsFromAccount(account))
		})
	}
}

func TestExcelBPSOptionsReplaceClearsOmitUnsupportedTools(t *testing.T) {
	require.Contains(t, ExcelBPSExtraKeys(), ExcelBPSOmitUnsupportedToolsKey)
	for name, patch := range map[string]map[string]any{
		"disable BPS":     nil,
		"disable options": excelBPSExtra(ExcelBPSOptions{}),
		"keep encrypted":  excelBPSExtra(ExcelBPSOptions{IgnoreEncryptedContent: true}),
	} {
		t.Run(name, func(t *testing.T) {
			extra := excelBPSExtra(ExcelBPSOptions{IgnoreEncryptedContent: true, OmitUnsupportedTools: true})
			extra["keep"] = "unchanged"
			replaceExcelBPSExtra(extra, patch)
			require.NotContains(t, extra, ExcelBPSOmitUnsupportedToolsKey)
			require.Equal(t, "unchanged", extra["keep"])
			require.Equal(t, patch[excelBPSExtraKey], extra[excelBPSExtraKey])
			require.Equal(t, patch[ExcelBPSIgnoreEncryptedContentKey], extra[ExcelBPSIgnoreEncryptedContentKey])
		})
	}
}

func TestAccountImportDefaultsExcelBPSKeepsExplicitUnsupportedTools(t *testing.T) {
	settings := excelBPSImportSettings()
	settings.ExcelBPSOptions = ExcelBPSOptions{IgnoreEncryptedContent: true, OmitUnsupportedTools: true}
	for _, tc := range []struct {
		name       string
		enabledBPS bool
		omitTools  bool
	}{
		{"option false only", false, false}, {"option true only", false, true},
		{"enabled BPS explicit false", true, false}, {"enabled BPS explicit true", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := accountImportDefaultTestService(t, settings)
			input := accountImportDefaultTestInput()
			input.Type = AccountTypeOAuth
			input.Extra = map[string]any{ExcelBPSOmitUnsupportedToolsKey: tc.omitTools}
			if tc.enabledBPS {
				input.Extra[excelBPSExtraKey] = true
			}
			account, err := svc.CreateAccount(context.Background(), input)
			require.NoError(t, err)
			require.Same(t, account, repo.created)
			require.Equal(t, tc.omitTools, account.Extra[ExcelBPSOmitUnsupportedToolsKey])
			require.Equal(t, tc.enabledBPS, account.IsExcelBPSEnabled())
			require.Equal(t, tc.enabledBPS && tc.omitTools, account.IsExcelBPSOmitUnsupportedToolsEnabled())
			require.NotContains(t, account.Extra, ExcelBPSIgnoreEncryptedContentKey, "显式子选项应保留整族配置，不能混入默认开启的加密内容选项")
			require.Equal(t, tc.omitTools, input.Extra[ExcelBPSOmitUnsupportedToolsKey])
			require.NotContains(t, input.Extra, ExcelBPSIgnoreEncryptedContentKey)
		})
	}
}

func TestAccountImportDefaultsExcelBPSKeepsExplicitEncryptedContent(t *testing.T) {
	settings := excelBPSImportSettings()
	settings.ExcelBPSOptions.IgnoreEncryptedContent = true
	for _, enabled := range []bool{false, true} {
		svc, repo := accountImportDefaultTestService(t, settings)
		input := accountImportDefaultTestInput()
		input.Type = AccountTypeOAuth
		input.Extra = map[string]any{ExcelBPSIgnoreEncryptedContentKey: enabled}
		account, err := svc.CreateAccount(context.Background(), input)
		require.NoError(t, err)
		require.Same(t, account, repo.created)
		require.Equal(t, enabled, account.Extra[ExcelBPSIgnoreEncryptedContentKey])
		require.NotContains(t, account.Extra, excelBPSExtraKey, "显式子选项按整族保留，不能由默认设置隐式启用 BPS")
		require.Equal(t, map[string]any{ExcelBPSIgnoreEncryptedContentKey: enabled}, input.Extra)
	}
}
