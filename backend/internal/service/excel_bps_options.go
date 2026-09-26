package service

import (
	"maps"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	excelBPSModelsExtraKey               = "openai_excel_bps_models"
	excelBPSCacheCreationAsInputExtraKey = "openai_excel_bps_cache_creation_as_input"
	excelBPSAutoDisableOn403ExtraKey     = "openai_excel_bps_auto_disable_on_403"

	maxExcelBPSModels      = 100
	maxExcelBPSModelLength = 128
)

// ExcelBPSOptions 是开启 Excel / BPS 时的子选项；Models 为 nil 表示对所有模型启用（兼容原设置）。
type ExcelBPSOptions struct {
	Models               *[]string `json:"models"`
	AutoDisableOn403     bool      `json:"auto_disable_on_403"`
	CacheCreationAsInput bool      `json:"cache_creation_as_input"`
}

var excelBPSExtraKeys = []string{excelBPSExtraKey, excelBPSModelsExtraKey, excelBPSCacheCreationAsInputExtraKey, excelBPSAutoDisableOn403ExtraKey}

// ExcelBPSExtraKeys 返回 BPS 配置族的全部 extra 键。
func ExcelBPSExtraKeys() []string { return append([]string{}, excelBPSExtraKeys...) }

// normalizeExcelBPSOptions 去除空白与重复模型；空列表有效，表示不对任何模型启用 BPS。
func normalizeExcelBPSOptions(options ExcelBPSOptions) (ExcelBPSOptions, error) {
	if options.Models == nil {
		return options, nil
	}
	if len(*options.Models) > maxExcelBPSModels {
		return options, infraerrors.BadRequest("EXCEL_BPS_MODELS_INVALID", "Excel / BPS 模型数量过多")
	}
	normalized := make([]string, 0, len(*options.Models))
	seen := make(map[string]bool, len(*options.Models))
	for _, model := range *options.Models {
		model = strings.TrimSpace(model)
		if len(model) > maxExcelBPSModelLength {
			return options, infraerrors.BadRequest("EXCEL_BPS_MODELS_INVALID", "Excel / BPS 模型名称过长")
		}
		if model != "" && !seen[model] {
			normalized = append(normalized, model)
			seen[model] = true
		}
	}
	options.Models = &normalized
	return options, nil
}

// excelBPSExtra 生成开启 BPS 时的整族配置；关闭的子选项不写入，与账号编辑保存的形态一致。
func excelBPSExtra(options ExcelBPSOptions) map[string]any {
	extra := map[string]any{excelBPSExtraKey: true}
	if options.Models != nil {
		extra[excelBPSModelsExtraKey] = append([]string{}, *options.Models...)
	}
	if options.AutoDisableOn403 {
		extra[excelBPSAutoDisableOn403ExtraKey] = true
	}
	if options.CacheCreationAsInput {
		extra[excelBPSCacheCreationAsInputExtraKey] = true
	}
	return extra
}

// replaceExcelBPSExtra 先清除整族 BPS 键再写入新配置，避免残留旧模型范围或子选项。
func replaceExcelBPSExtra(target, patch map[string]any) {
	for _, key := range excelBPSExtraKeys {
		delete(target, key)
	}
	maps.Copy(target, patch)
}

// excelBPSEligible 只判断账号身份是否支持 BPS，不看当前开关。
func excelBPSEligible(platform, accountType string, credentials map[string]any) bool {
	return (&Account{Platform: platform, Type: accountType, Credentials: credentials, Extra: map[string]any{excelBPSExtraKey: true}}).IsExcelBPSEnabled()
}

// ExcelBPSBlocksCodexTicket 报告全模型 BPS 是否占用了打票；两者互斥。
// 只选部分模型时其余模型仍走原 Codex 路径，不阻止打票。
func ExcelBPSBlocksCodexTicket(a *Account) bool { return a.isExcelBPSAllModelsEnabled() }

// disableCodexTicketForExcelBPS 在账号（按写入后的 extra 计算）开启全模型 BPS 时关闭打票，
// 关闭 BPS 后打票保持关闭，由管理员手动重新开启。
func disableCodexTicketForExcelBPS(account *Account, extra map[string]any) {
	if account == nil || extra == nil {
		return
	}
	next := *account
	next.Extra = extra
	if next.isExcelBPSAllModelsEnabled() {
		extra[OpenAICodexTicketEnabledExtraKey] = false
	}
}

// ExcelBPSOptionsFromAccount 读取已保存的 BPS 配置；未开启时返回 nil。
func ExcelBPSOptionsFromAccount(a *Account) *ExcelBPSOptions {
	if !a.IsExcelBPSEnabled() {
		return nil
	}
	options := &ExcelBPSOptions{
		AutoDisableOn403:     a.IsExcelBPSAutoDisableOn403Enabled(),
		CacheCreationAsInput: a.IsExcelBPSCacheCreationAsInputEnabled(),
	}
	if raw, scoped := a.Extra[excelBPSModelsExtraKey]; scoped {
		models := []string{}
		switch values := raw.(type) {
		case []string:
			models = append(models, values...)
		case []any:
			for _, value := range values {
				if model, ok := value.(string); ok {
					models = append(models, model)
				}
			}
		}
		options.Models = &models
	}
	return options
}
