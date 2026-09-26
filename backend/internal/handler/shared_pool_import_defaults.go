package handler

import (
	"context"
	"slices"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type sharedAccountImportSettings interface {
	GetAccountImportSettings(context.Context) (*service.AccountImportSettings, error)
}

// 只公开共享池允许继承的配置，代理和分组仍由共享池自己的规则决定。
type sharedAccountImportDefaults struct {
	ProtectionEnabled  bool                    `json:"protection_enabled"`
	CodexTicketEnabled bool                    `json:"codex_ticket_enabled"`
	ExcelBPSEnabled    bool                    `json:"excel_bps_enabled"`
	ExcelBPSOptions    service.ExcelBPSOptions `json:"excel_bps_options"`
}

type sharedAccountCreateRequest struct {
	service.SharedPoolAccountInput
	ProtectionEnabled *bool `json:"protection_enabled"`
}

func (h *SharedPoolHandler) accountImportDefaults(ctx context.Context) (*sharedAccountImportDefaults, error) {
	if h.importSettings == nil {
		return nil, nil
	}
	settings, err := h.importSettings.GetAccountImportSettings(ctx)
	if err != nil {
		return nil, err
	}
	if settings == nil || !settings.Enabled {
		return nil, nil
	}
	return &sharedAccountImportDefaults{
		ProtectionEnabled: settings.ProtectionEnabled, CodexTicketEnabled: settings.CodexTicketEnabled,
		ExcelBPSEnabled: settings.ExcelBPSEnabled, ExcelBPSOptions: settings.ExcelBPSOptions,
	}, nil
}

func applySharedAccountImportDefaults(input *service.SharedPoolAccountInput, defaults *sharedAccountImportDefaults, choices sharedImportDefaults) {
	if choices.ProtectionEnabled != nil {
		input.ProtectionEnabled = *choices.ProtectionEnabled
	} else if defaults != nil {
		input.ProtectionEnabled = defaults.ProtectionEnabled
	}
	// 兼容旧客户端字段，但打票选择只由平台决定，用户提交值不能覆盖。
	input.CodexTicketEnabled = nil
	if input.Platform != service.PlatformOpenAI || input.Type != service.AccountTypeOAuth {
		return
	}
	input.CodexTicketEnabled = new(true)
	if defaults == nil {
		return
	}
	input.CodexTicketEnabled = new(defaults.CodexTicketEnabled)
	account := &service.Account{Platform: input.Platform, Type: input.Type, Credentials: input.Credentials}
	// 与后台导入一致：显式填写任一 BPS 字段时，整个配置族使用当次提交值。
	if choices.ExcelBPSEnabled != nil || choices.ExcelBPSOptions != nil || account.IsOpenAIAgentIdentity() || account.IsOpenAIPersonalAccessToken() {
		return
	}
	input.ExcelBPSEnabled = new(defaults.ExcelBPSEnabled)
	if defaults.ExcelBPSEnabled {
		options := defaults.ExcelBPSOptions
		if options.Models != nil {
			models := slices.Clone(*options.Models)
			options.Models = &models
		}
		input.ExcelBPSOptions = &options
	}
}
