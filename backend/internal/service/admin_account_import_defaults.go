package service

import (
	"context"
	"maps"
	"slices"
)

// 创建专用入口；显式字段按配置族优先，避免默认值把请求的代理改为另一种模式。
func (s *adminServiceImpl) applyAccountImportDefaults(ctx context.Context, input *CreateAccountInput) (*CreateAccountInput, error) {
	prepared := *input
	prepared.Extra = cloneAccountImportMap(input.Extra)
	prepared.Credentials = cloneAccountImportMap(input.Credentials)
	prepared.GroupIDs = slices.Clone(input.GroupIDs)
	prepared.ProxyID = cloneAccountValuePointer(input.ProxyID)
	defer func() {
		// 请求中的 0 表示显式直连；只在默认值决策后转换为数据库空关联。
		if prepared.ProxyID != nil && *prepared.ProxyID == 0 {
			prepared.ProxyID = nil
		}
	}()
	if input.SkipImportDefaults || s.settingService == nil {
		return &prepared, nil
	}
	settings, err := s.settingService.GetAccountImportSettings(ctx)
	if err != nil {
		return nil, err
	}
	if !settings.Enabled {
		return &prepared, nil
	}
	if prepared.Extra == nil {
		prepared.Extra = make(map[string]any)
	}
	if prepared.ProtectionEnabled == nil && !hasAccountImportExtra(prepared.Extra, []string{AntiDegradationExtraKey, AntiDegradeMarkerExtraKey, ProtectionScopeExtraKey}) {
		prepared.ProtectionEnabled = cloneAccountValuePointer(&settings.ProtectionEnabled)
	}
	if err := s.applyAccountImportProxyDefaults(ctx, &prepared, settings); err != nil {
		return nil, err
	}
	copyAccountImportExtraFamily(prepared.Extra, settings.Extra, accountImportRegionKeys)
	applyAccountImportExcelBPSDefault(&prepared, settings)
	// BPS 账号不走打票链路，打票默认值不适用。
	if isOpenAICodexTicketAccount(&Account{Platform: prepared.Platform, Type: prepared.Type, Credentials: prepared.Credentials, Extra: prepared.Extra}) {
		if prepared.CodexTicketEnabled == nil && !hasAccountImportExtra(prepared.Extra, []string{OpenAICodexTicketEnabledExtraKey}) {
			prepared.CodexTicketEnabled = cloneAccountValuePointer(&settings.CodexTicketEnabled)
		}
		copyAccountImportExtraFamily(prepared.Extra, settings.Extra, accountImportTicketProxyKeys)
		copyAccountImportExtraFamily(prepared.Extra, settings.Extra, accountImportTicketCredentialKeys)
	}
	return &prepared, nil
}

// 只给满足 BPS 条件的 ChatGPT OAuth 账号补齐整族 BPS 配置；导入数据显式填写任一 BPS 字段（含 false）时保留原值。
func applyAccountImportExcelBPSDefault(input *CreateAccountInput, settings *AccountImportSettings) {
	if !settings.ExcelBPSEnabled || hasAccountImportExtra(input.Extra, excelBPSExtraKeys) {
		return
	}
	if excelBPSEligible(input.Platform, input.Type, input.Credentials) {
		replaceExcelBPSExtra(input.Extra, excelBPSExtra(settings.ExcelBPSOptions))
	}
}

func (s *adminServiceImpl) applyAccountImportProxyDefaults(ctx context.Context, input *CreateAccountInput, settings *AccountImportSettings) error {
	if input.ProxyID != nil || hasAccountImportExtra(input.Extra, accountImportRoutingKeys) {
		return nil
	}
	switch settings.ProxyMode {
	case "fixed":
		if err := validateAccountImportFixedProxy(ctx, s.proxyRepo, *settings.ProxyID); err != nil {
			return err
		}
		input.ProxyID = cloneAccountValuePointer(settings.ProxyID)
	case "random":
		copyAccountImportExtraFamily(input.Extra, settings.Extra, accountImportRoutingKeys[1:])
		input.Extra[ProxyModeExtraKey] = ProxyModeRandom
	case "direct":
		input.ProxyID = nil
	}
	return nil
}

func hasAccountImportExtra(extra map[string]any, keys []string) bool {
	for _, key := range keys {
		if _, exists := extra[key]; exists {
			return true
		}
	}
	return false
}

func copyAccountImportExtraFamily(target, defaults map[string]any, keys []string) {
	if hasAccountImportExtra(target, keys) {
		return
	}
	for _, key := range keys {
		if value, exists := defaults[key]; exists {
			target[key] = cloneAccountImportValue(value)
		}
	}
}

func cloneAccountImportMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = cloneAccountImportValue(value)
	}
	return cloned
}

func cloneAccountImportValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAccountImportMap(typed)
	case map[string]string:
		return maps.Clone(typed)
	case []any:
		cloned := make([]any, len(typed))
		for i, item := range typed {
			cloned[i] = cloneAccountImportValue(item)
		}
		return cloned
	case []int64:
		return slices.Clone(typed)
	case []string:
		return slices.Clone(typed)
	default:
		return value
	}
}
