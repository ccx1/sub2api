package repository

import (
	"encoding/json"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func validateSharedStateActor(state service.SharedPoolAccountState) error {
	if state.OwnerID < 0 || (state.OwnerID == 0 && state.DispatchConsent != nil) || (state.OwnerID > 0 && (state.AdminDisabled != nil || state.SubscriptionTier != nil || state.DefaultGroupIDs != nil || state.Priority != nil)) {
		return infraerrors.Forbidden("SHARED_STATE_FORBIDDEN", "无权修改此账号授权状态")
	}
	if state.Priority != nil && (*state.Priority < 0 || *state.Priority > 100) {
		return infraerrors.BadRequest("INVALID_SHARED_PRIORITY", "共享账号优先级须为0至100")
	}
	return nil
}

// 必须在账号与归属行加锁后调用，防止用户开关和管理员分配相互覆盖。
func prepareSharedDispatchState(extra map[string]any, state *service.SharedPoolAccountState) (bool, string, error) {
	consented := service.SharedPoolDispatchConsented(&service.Account{Extra: extra})
	patch := map[string]any{}
	if state.DispatchConsent != nil {
		if !*state.DispatchConsent || state.Enabled == nil || !*state.Enabled {
			return false, "", infraerrors.BadRequest("INVALID_SHARED_CONSENT", "请在开启调度时明确同意授权")
		}
		if !consented {
			patch[service.SharedPoolDispatchConsentKey] = true
			consented = true
		}
	}
	if _, modern := extra[service.SharedPoolDispatchConsentKey]; modern && !consented && state.Enabled != nil && *state.Enabled {
		return false, "", infraerrors.Forbidden("SHARED_DISPATCH_CONSENT_REQUIRED", "启用账号前，请明确授权账号参与平台调度")
	}
	encoded, err := json.Marshal(patch)
	return consented, string(encoded), err
}
