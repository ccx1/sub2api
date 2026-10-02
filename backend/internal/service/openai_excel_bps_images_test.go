package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountExcelBPSImagesEnabledForModel(t *testing.T) {
	account := excelAccount()
	require.True(t, account.IsExcelBPSImagesEnabledForModel("gpt-image-2"))
	require.False(t, account.IsExcelBPSImagesEnabledForModel("gpt-6-astra"))

	account.Extra[excelBPSModelsExtraKey] = []string{"gpt-6-astra"}
	require.False(t, account.IsExcelBPSImagesEnabledForModel("gpt-image-2"))
	account.Extra[excelBPSModelsExtraKey] = []any{"gpt-image-2"}
	require.True(t, account.IsExcelBPSImagesEnabledForModel("gpt-image-2"))
	account.Credentials["model_mapping"] = map[string]any{"image-alias": "gpt-image-2"}
	require.True(t, account.IsExcelBPSImagesEnabledForModel("image-alias"))

	account.Extra[excelBPSModelsExtraKey] = []string{}
	require.False(t, account.IsExcelBPSImagesEnabledForModel("image-alias"))
	delete(account.Extra, excelBPSModelsExtraKey)
	account.Extra[excelBPSExtraKey] = false
	require.False(t, account.IsExcelBPSImagesEnabledForModel("gpt-image-2"))

	account.Extra[excelBPSExtraKey] = true
	account.Credentials["plan_type"] = "free"
	require.False(t, account.IsExcelBPSImagesEnabledForModel("gpt-image-2"))
	account.Credentials["plan_type"] = "plus"
	account.Type = AccountTypeAPIKey
	require.False(t, account.IsExcelBPSImagesEnabledForModel("gpt-image-2"))

	var absent *Account
	require.False(t, absent.IsExcelBPSImagesEnabledForModel("gpt-image-2"))
}
