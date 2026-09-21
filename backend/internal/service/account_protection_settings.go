package service

import (
	"context"
	"errors"
	"fmt"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const SettingKeyAccountProtectionDefaultMode = "account_protection_default_mode"

type AccountProtectionSettings struct {
	DefaultMode AntiDegradeMode              `json:"default_mode"`
	Sync        *AccountProtectionSyncResult `json:"sync,omitempty"`
}

func (s *AntiDegradeService) GetProtectionSettings(ctx context.Context) (*AccountProtectionSettings, error) {
	settings := &AccountProtectionSettings{DefaultMode: DefaultAntiDegradeMode}
	if s == nil || s.settingRepo == nil {
		return settings, nil
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyAccountProtectionDefaultMode)
	if errors.Is(err, ErrSettingNotFound) {
		return settings, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read account protection settings: %w", err)
	}
	mode := AntiDegradeMode(raw)
	if !antiDegradeStrategyProfile(mode).ApplySupported {
		return nil, infraerrors.InternalServer("PROTECTION_SETTINGS_INVALID", "已保存的账号保护默认策略无效，请重新配置")
	}
	settings.DefaultMode = mode
	return settings, nil
}

func (s *AntiDegradeService) UpdateProtectionSettings(ctx context.Context, mode AntiDegradeMode) (*AccountProtectionSettings, error) {
	if !antiDegradeStrategyProfile(mode).ApplySupported {
		return nil, infraerrors.BadRequest("INVALID_PROTECTION_DEFAULT_MODE", "请选择可应用的账号保护策略")
	}
	if s == nil || s.settingRepo == nil {
		return nil, infraerrors.ServiceUnavailable("PROTECTION_SETTINGS_UNAVAILABLE", "账号保护设置服务不可用")
	}
	if s.accountRepo == nil {
		return nil, infraerrors.ServiceUnavailable("PROTECTION_SYNC_UNAVAILABLE", "账号保护同步服务不可用")
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if err := s.settingRepo.Set(ctx, SettingKeyAccountProtectionDefaultMode, string(mode)); err != nil {
		return nil, fmt.Errorf("save account protection settings: %w", err)
	}
	ids, err := s.protectionSyncAccountIDs(ctx)
	if err != nil {
		return &AccountProtectionSettings{DefaultMode: mode, Sync: &AccountProtectionSyncResult{
			Failed: []AccountProtectionSyncFailure{}, Error: "默认策略已保存，读取待同步账号失败；请重新保存以重试: " + err.Error(),
		}}, nil
	}
	return &AccountProtectionSettings{DefaultMode: mode, Sync: s.syncProtectionDefaults(ctx, ids, mode)}, nil
}

func defaultAccountProtectionMode(a *Account) AntiDegradeMode {
	if isOpenAIOAuthLike(a) && !a.IsShadow() {
		return DefaultAntiDegradeMode
	}
	return AntiDegradeModeLegacy
}

func (s *AntiDegradeService) resolveProtectionMode(ctx context.Context, a *Account, mode AntiDegradeMode) (AntiDegradeMode, error) {
	if mode != "" {
		if antiDegradeStrategyProfile(mode).ID == "" {
			return "", ErrUnknownAntiDegradeMode
		}
		return mode, nil
	}
	if !isOpenAIOAuthLike(a) || a.IsShadow() {
		return defaultAccountProtectionMode(a), nil
	}
	settings, err := s.GetProtectionSettings(ctx)
	if err != nil {
		return "", err
	}
	return settings.DefaultMode, nil
}

// 新账号仅在内存中准备策略，读取设置失败时不修改调用方对象或继续创建。
func (s *AntiDegradeService) PrepareNewAccountProtection(ctx context.Context, a *Account) error {
	if a == nil {
		return nil
	}
	draft := *a
	resetNewAccountProtection(&draft)
	if (isOpenAIOAuthLike(&draft) || isAnthropicOAuthLike(&draft)) && !draft.IsShadow() {
		mode, err := s.resolveProtectionMode(ctx, &draft, "")
		if err != nil {
			return err
		}
		store := &protectionDraftStore{account: &draft}
		planner := NewAntiDegradeService(store)
		prepared, err := planner.applyAntiDegradeMode(ctx, draft.ID, mode)
		if err != nil {
			return err
		}
		draft = *prepared
	} else {
		configureAccountProtection(&draft)
	}
	*a = draft
	return nil
}

// 与全局策略保存互斥到创建提交，避免旧默认策略在同步枚举结束后才入库。
func (s *AntiDegradeService) CreateProtectedAccount(ctx context.Context, a *Account, persist func() error) error {
	if s != nil {
		s.settingsMu.Lock()
		defer s.settingsMu.Unlock()
	}
	if err := s.PrepareNewAccountProtection(ctx, a); err != nil {
		return err
	}
	return persist()
}
