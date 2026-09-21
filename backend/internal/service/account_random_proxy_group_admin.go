package service

import (
	"context"
	"maps"
)

func mergeRandomProxyRoutingExtra(extra, current map[string]any) map[string]any {
	merged := maps.Clone(extra)
	for _, key := range []string{ProxyModeExtraKey, RandomProxyPoolScopeExtraKey, RandomProxyPoolIDsExtraKey,
		RandomProxyGroupIDExtraKey, RandomProxyEmptyPoolPolicyExtraKey, RandomProxyMaxReuseMinutesExtraKey} {
		if _, supplied := extra[key]; supplied {
			continue
		}
		if value, exists := current[key]; exists {
			if merged == nil {
				merged = make(map[string]any)
			}
			merged[key] = value
		}
	}
	return merged
}

func hasRandomProxyGroupUpdates(extra map[string]any) bool {
	for _, key := range []string{ProxyModeExtraKey, RandomProxyPoolScopeExtraKey, RandomProxyGroupIDExtraKey} {
		if _, exists := extra[key]; exists {
			return true
		}
	}
	return false
}

// JSONB 局部合并需要显式 null，删除补丁里的键不会清除数据库已有值。
func explicitRandomProxyRoutingPatch(extra, requested map[string]any) map[string]any {
	if _, provided := requested[ProxyModeExtraKey]; !provided || (&Account{Extra: requested}).IsRandomProxy() {
		return extra
	}
	if extra == nil {
		extra = make(map[string]any)
	}
	for _, key := range []string{ProxyModeExtraKey, RandomProxyEmptyPoolPolicyExtraKey, RandomProxyGroupIDExtraKey,
		RandomProxyPoolScopeExtraKey, RandomProxyPoolIDsExtraKey} {
		extra[key] = nil
	}
	return extra
}

func (s *adminServiceImpl) validateAccountRandomProxyGroup(ctx context.Context, account *Account) error {
	if account == nil || !account.IsRandomProxy() || account.RandomProxyPoolScope() != RandomProxyPoolGroup {
		return nil
	}
	if err := ValidateRandomProxyPoolExtra(account.Extra); err != nil {
		return err
	}
	id := account.RandomProxyGroupID()
	return s.validateProxyGroup(ctx, &id)
}

func (s *adminServiceImpl) validateRandomProxyGroupUpdate(ctx context.Context, current *Account, updates map[string]any) error {
	if current == nil {
		return ErrAccountNotFound
	}
	return s.validateAccountRandomProxyGroup(ctx, &Account{Extra: mergeRandomProxyRoutingExtra(updates, current.Extra)})
}
