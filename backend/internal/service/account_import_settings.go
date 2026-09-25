package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const SettingKeyAccountImportSettings = "account_import_settings"

type AccountImportSettings struct {
	Enabled            bool           `json:"enabled"`
	ProtectionEnabled  bool           `json:"protection_enabled"`
	CodexTicketEnabled bool           `json:"codex_ticket_enabled"`
	ExcelBPSEnabled    bool           `json:"excel_bps_enabled"`
	ProxyMode          string         `json:"proxy_mode"`
	ProxyID            *int64         `json:"proxy_id"`
	Extra              map[string]any `json:"extra"`
}

func DefaultAccountImportSettings() AccountImportSettings {
	return AccountImportSettings{
		ProtectionEnabled: true, CodexTicketEnabled: true,
		ProxyMode: "preserve", Extra: map[string]any{},
	}
}

func (s *SettingService) GetAccountImportSettings(ctx context.Context) (*AccountImportSettings, error) {
	if s == nil || s.settingRepo == nil {
		return nil, infraerrors.ServiceUnavailable("ACCOUNT_IMPORT_SETTINGS_UNAVAILABLE", "账号导入默认设置服务不可用")
	}
	settings := DefaultAccountImportSettings()
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyAccountImportSettings)
	if errors.Is(err, ErrSettingNotFound) {
		return &settings, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read account import settings: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if strings.TrimSpace(raw) == "null" || decoder.Decode(&settings) != nil {
		return nil, invalidStoredAccountImportSettings()
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, invalidStoredAccountImportSettings()
	}
	if err := normalizeAccountImportSettings(&settings); err != nil {
		return nil, invalidStoredAccountImportSettings()
	}
	return &settings, nil
}

func invalidStoredAccountImportSettings() error {
	return infraerrors.InternalServer("ACCOUNT_IMPORT_SETTINGS_INVALID", "已保存的账号导入默认设置无效，请重新配置")
}

func (s *SettingService) UpdateAccountImportSettings(ctx context.Context, input AccountImportSettings) (*AccountImportSettings, error) {
	if s == nil || s.settingRepo == nil {
		return nil, infraerrors.ServiceUnavailable("ACCOUNT_IMPORT_SETTINGS_UNAVAILABLE", "账号导入默认设置服务不可用")
	}
	input.Extra = cloneAccountImportMap(input.Extra)
	input.ProxyID = cloneAccountValuePointer(input.ProxyID)
	if err := normalizeAccountImportSettings(&input); err != nil {
		return nil, err
	}
	if input.Enabled {
		if err := validateAccountImportProxyReferences(ctx, s.proxyRepo, &input); err != nil {
			return nil, err
		}
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	if err := s.settingRepo.Set(ctx, SettingKeyAccountImportSettings, string(raw)); err != nil {
		return nil, fmt.Errorf("save account import settings: %w", err)
	}
	return &input, nil
}

var accountImportRoutingKeys = []string{
	ProxyModeExtraKey, RandomProxyPoolScopeExtraKey, RandomProxyPoolIDsExtraKey,
	RandomProxyGroupIDExtraKey, RandomProxyEmptyPoolPolicyExtraKey, RandomProxyMaxReuseMinutesExtraKey,
	RandomProxyRegionFallbackExtraKey,
}

var accountImportRegionKeys = []string{
	ProxyRegionModeExtraKey, ProxyRegionCountryExtraKey, ProxyRegionFallbackCountryExtraKey,
}

var accountImportTicketProxyKeys = []string{
	CodexTicketProxyModeExtraKey, CodexTicketProxyIDExtraKey,
	CodexTicketProxyStrategyExtraKey,
}

var accountImportTicketCredentialKeys = []string{CodexTicketCredentialPolicyExtraKey}

func normalizeAccountImportSettings(settings *AccountImportSettings) error {
	settings.ProxyMode = strings.TrimSpace(settings.ProxyMode)
	switch settings.ProxyMode {
	case "preserve", "direct", "random":
		if settings.ProxyID != nil && *settings.ProxyID != 0 {
			return infraerrors.BadRequest("INVALID_ACCOUNT_IMPORT_PROXY", "只有固定代理模式可以设置代理 ID")
		}
		settings.ProxyID = nil
	case "fixed":
		if settings.ProxyID == nil || *settings.ProxyID <= 0 || *settings.ProxyID > 1<<53-1 {
			return infraerrors.BadRequest("INVALID_ACCOUNT_IMPORT_PROXY", "固定代理模式必须选择有效代理")
		}
	default:
		return infraerrors.BadRequest("INVALID_ACCOUNT_IMPORT_PROXY_MODE", "导入默认代理模式必须是保留、直连、固定或随机")
	}
	if settings.Extra == nil {
		settings.Extra = map[string]any{}
	}
	if err := validateAccountImportExtra(settings.Extra); err != nil {
		return err
	}
	normalizeRandomProxyPoolExtra(settings.Extra)
	normalizeProxyRegionExtra(settings.Extra)
	return nil
}

func validateAccountImportExtra(extra map[string]any) error {
	allowed := make(map[string]bool)
	for _, keys := range [][]string{accountImportRoutingKeys[1:], accountImportRegionKeys, accountImportTicketProxyKeys, accountImportTicketCredentialKeys} {
		for _, key := range keys {
			allowed[key] = true
		}
	}
	for key := range extra {
		if !allowed[key] {
			return infraerrors.BadRequest("INVALID_ACCOUNT_IMPORT_EXTRA", "导入默认设置包含不支持的字段: "+key)
		}
	}
	if raw, exists := extra[RandomProxyEmptyPoolPolicyExtraKey]; exists {
		policy, ok := raw.(string)
		if !ok || (policy != RandomProxyEmptyPoolPolicyReject && policy != RandomProxyEmptyPoolPolicyDisable && policy != RandomProxyEmptyPoolPolicyDirect) {
			return infraerrors.BadRequest("INVALID_ACCOUNT_IMPORT_EMPTY_POOL_POLICY", "随机代理空池策略无效")
		}
	}
	for _, validate := range []func(map[string]any) error{
		ValidateRandomProxyPoolExtra, ValidateRandomProxyReuseExtra, ValidateProxyRegionExtra, ValidateCodexTicketProxyExtra,
	} {
		if err := validate(extra); err != nil {
			return err
		}
	}
	return nil
}

func validateAccountImportProxyReferences(ctx context.Context, repo ProxyRepository, settings *AccountImportSettings) error {
	if settings.ProxyMode == "fixed" {
		if err := validateAccountImportFixedProxy(ctx, repo, *settings.ProxyID); err != nil {
			return err
		}
	}
	admin := &adminServiceImpl{proxyRepo: repo}
	if settings.ProxyMode == "random" {
		extra := maps.Clone(settings.Extra)
		extra[ProxyModeExtraKey] = ProxyModeRandom
		if err := admin.validateAccountRandomProxyGroup(ctx, &Account{Extra: extra}); err != nil {
			return err
		}
	}
	return admin.validateCodexTicketProxyAvailable(ctx, settings.Extra)
}

func validateAccountImportFixedProxy(ctx context.Context, repo ProxyRepository, id int64) error {
	if repo == nil {
		return infraerrors.BadRequest("ACCOUNT_IMPORT_PROXY_UNAVAILABLE", "默认固定代理不可用")
	}
	proxy, err := repo.GetByID(ctx, id)
	if err != nil || proxy == nil || proxy.ID != id || !proxy.IsActive() || proxy.IsExpired(time.Now()) {
		return infraerrors.BadRequest("ACCOUNT_IMPORT_PROXY_UNAVAILABLE", "默认固定代理不存在、已停用或已过期")
	}
	return nil
}
