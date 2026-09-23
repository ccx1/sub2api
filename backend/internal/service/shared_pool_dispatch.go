package service

import (
	"context"
	"math"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func SharedPoolDispatchConsented(a *Account) bool {
	if a == nil {
		return false
	}
	consent, _ := a.Extra[SharedPoolDispatchConsentKey].(bool)
	return consent
}

func ValidSharedPoolSettlementMultiplier(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 100
}

func (s *SharedPoolService) prepareSharedDispatch(ctx context.Context, cfg *SharedPoolSettings, a *Account, consent bool) error {
	if !consent {
		if enabled, _ := a.Extra[SharedPoolEnabledKey].(bool); enabled {
			return infraerrors.BadRequest("SHARED_DISPATCH_CONSENT_REQUIRED", "启用账号前，请明确授权账号参与平台调度")
		}
		return nil
	}
	if enabled, _ := a.Extra[SharedPoolEnabledKey].(bool); !enabled {
		return nil
	}
	ids, err := s.initialSharedGroups(ctx, cfg, a)
	if err == nil {
		a.GroupIDs = ids
	}
	return err
}

// 授权升级只在专用开关入口发生，普通资料编辑不能改变授权或结算倍率。
func (s *SharedPoolService) setSharedEnabled(ctx context.Context, userID, id int64, enabled bool, consent *bool) error {
	record, a, err := s.OwnedAccount(ctx, userID, id)
	if err != nil {
		return err
	}
	if enabled && record.AdminDisabled {
		return infraerrors.Forbidden("SHARED_ADMIN_DISABLED", "账号已被管理员停用")
	}
	state := SharedPoolAccountState{OwnerID: userID, Enabled: &enabled, DispatchConsent: consent}
	if consent != nil && (!*consent || !enabled) {
		return infraerrors.BadRequest("INVALID_SHARED_CONSENT", "请在开启调度时明确同意授权，暂停账号请关闭启用开关")
	}
	if _, modern := a.Extra[SharedPoolDispatchConsentKey]; enabled && modern && !SharedPoolDispatchConsented(a) && consent == nil {
		return infraerrors.BadRequest("SHARED_DISPATCH_CONSENT_REQUIRED", "启用账号前，请明确授权账号参与平台调度")
	}
	upgrade := consent != nil && *consent && !SharedPoolDispatchConsented(a)
	if enabled && (upgrade || (len(a.GroupIDs) == 0 && !record.Assigned)) {
		cfg, err := s.repo.SharedSettings(ctx)
		if err != nil {
			return err
		}
		if upgrade {
			copy := *a
			copy.Extra = make(map[string]any, len(a.Extra)+1)
			for key, value := range a.Extra {
				copy.Extra[key] = value
			}
			copy.Extra[SharedPoolDispatchConsentKey] = true
			a = &copy
		}
		if len(a.GroupIDs) == 0 && !record.Assigned {
			groupIDs, err := s.initialSharedGroups(ctx, cfg, a)
			if err != nil {
				return err
			}
			if len(groupIDs) > 0 {
				state.GroupIDs = &groupIDs
			}
		}
	}
	return s.repo.SetSharedAccountState(ctx, id, state)
}
