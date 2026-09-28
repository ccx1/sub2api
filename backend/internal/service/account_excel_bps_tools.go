package service

import "github.com/Wei-Shaw/sub2api/internal/service/basispoints"

const ExcelBPSOmitUnsupportedToolsKey = "openai_excel_bps_omit_unsupported_tools"

// IsExcelBPSOmitUnsupportedToolsEnabled 仅允许显式开启的 BPS 账号省略托管工具。
func (a *Account) IsExcelBPSOmitUnsupportedToolsEnabled() bool {
	if !a.IsExcelBPSEnabled() {
		return false
	}
	enabled, _ := a.Extra[ExcelBPSOmitUnsupportedToolsKey].(bool)
	return enabled
}

func (a *Account) excelBPSNativeFallbackReason(body []byte) string {
	if a.IsExcelBPSOmitUnsupportedToolsEnabled() {
		return ""
	}
	return basispoints.NativeFallbackReason(body)
}
