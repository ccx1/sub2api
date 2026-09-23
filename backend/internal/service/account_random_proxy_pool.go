package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	RandomProxyPoolScopeExtraKey = "random_proxy_pool_scope"
	RandomProxyPoolIDsExtraKey   = "random_proxy_pool_ids"
	RandomProxyGroupIDExtraKey   = "random_proxy_group_id"
	RandomProxyPoolAll           = "all"
	RandomProxyPoolSelected      = "selected"
	RandomProxyPoolGroup         = "group"
)

type RandomProxyPoolSelector interface {
	SelectRandomActiveProxyFromPool(ctx context.Context, ids []int64) (*Proxy, error)
}

type RandomProxyGroupResolver interface {
	GetRandomProxyGroupIDs(context.Context, int64) ([]int64, error)
}

func (a *Account) RandomProxyPoolScope() string {
	if a == nil {
		return RandomProxyPoolAll
	}
	scope, _ := a.Extra[RandomProxyPoolScopeExtraKey].(string)
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case RandomProxyPoolSelected:
		return RandomProxyPoolSelected
	case RandomProxyPoolGroup:
		return RandomProxyPoolGroup
	}
	return RandomProxyPoolAll
}

func (a *Account) RandomProxyGroupID() int64 {
	if a == nil {
		return 0
	}
	id, _ := parseRandomProxyGroupID(a.Extra[RandomProxyGroupIDExtraKey])
	return id
}

func parseRandomProxyGroupID(raw any) (int64, bool) {
	if raw == nil {
		return 0, true
	}
	ids, valid := parseRandomProxyPoolIDs([]any{raw})
	if !valid || len(ids) != 1 {
		return 0, false
	}
	return ids[0], true
}

func (a *Account) RandomProxyPoolIDs() []int64 {
	if a == nil {
		return nil
	}
	ids, _ := parseRandomProxyPoolIDs(a.Extra[RandomProxyPoolIDsExtraKey])
	return ids
}

func parseRandomProxyPoolIDs(raw any) ([]int64, bool) {
	if raw == nil {
		return nil, true
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}
	var values []any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if decoder.Decode(&values) != nil {
		return nil, false
	}
	ids := make([]int64, 0, len(values))
	for _, value := range values {
		number, ok := value.(json.Number)
		if !ok {
			return nil, false
		}
		id, err := number.Int64()
		if err != nil || id <= 0 || id > 1<<53-1 {
			return nil, false
		}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return slices.Compact(ids), true
}

// 显式指定空池不得退化为全池；保存时拦截，运行时仍按空池策略处理。
func ValidateRandomProxyPoolExtra(extra map[string]any) error {
	scope, exists := extra[RandomProxyPoolScopeExtraKey]
	if exists && scope != nil {
		value, ok := scope.(string)
		if !ok || (strings.TrimSpace(value) != RandomProxyPoolAll && strings.TrimSpace(value) != RandomProxyPoolSelected && strings.TrimSpace(value) != RandomProxyPoolGroup) {
			return infraerrors.BadRequest("INVALID_RANDOM_PROXY_SCOPE", "随机代理范围无效")
		}
		scope = strings.TrimSpace(value)
	}
	ids, valid := parseRandomProxyPoolIDs(extra[RandomProxyPoolIDsExtraKey])
	if !valid || len(ids) > math.MaxInt16 {
		return infraerrors.BadRequest("INVALID_RANDOM_PROXY_POOL", "指定代理必须是有效的代理 ID 列表")
	}
	if scope == RandomProxyPoolSelected && len(ids) == 0 {
		return infraerrors.BadRequest("EMPTY_RANDOM_PROXY_POOL", "请至少选择一条代理，或切换为整个代理池")
	}
	groupID, valid := parseRandomProxyGroupID(extra[RandomProxyGroupIDExtraKey])
	if !valid || (scope == RandomProxyPoolGroup && groupID == 0) {
		return infraerrors.BadRequest("INVALID_RANDOM_PROXY_GROUP", "请选择有效的代理分组")
	}
	return nil
}

func normalizeRandomProxyPoolExtra(extra map[string]any) {
	if scope, ok := extra[RandomProxyPoolScopeExtraKey].(string); ok {
		extra[RandomProxyPoolScopeExtraKey] = strings.ToLower(strings.TrimSpace(scope))
	}
	if raw, exists := extra[RandomProxyPoolIDsExtraKey]; exists {
		if ids, valid := parseRandomProxyPoolIDs(raw); valid {
			extra[RandomProxyPoolIDsExtraKey] = ids
		}
	}
	if raw, exists := extra[RandomProxyGroupIDExtraKey]; exists && raw != nil {
		if id, valid := parseRandomProxyGroupID(raw); valid {
			extra[RandomProxyGroupIDExtraKey] = id
		}
	}
}

func selectAccountRandomProxy(ctx context.Context, a *Account, selector RandomProxySelector) (*Proxy, error) {
	selection, err := ResolveAccountProxyPoolSelection(ctx, a, selector)
	if err != nil {
		return nil, err
	}
	if balanced, ok := selector.(BalancedProxySelector); ok {
		proxy, err := balanced.SelectBalancedProxy(ctx, selection)
		if err != nil {
			return nil, err
		}
		if selection.Restricted && proxy != nil && !slices.Contains(selection.IDs, proxy.ID) {
			return nil, nil
		}
		if proxy != nil {
			if err := validateProxyRegion(ctx, proxy, selection.CountryCode, selector); err != nil {
				return nil, err
			}
		}
		return proxy, err
	}
	if selection.CountryCode != "" {
		return nil, errors.New("proxy region selection requires a region-aware proxy pool")
	}
	if !selection.Restricted {
		return selector.SelectRandomActiveProxy(ctx)
	}
	ids := selection.IDs
	if len(ids) == 0 {
		return nil, nil
	}
	scoped, ok := selector.(RandomProxyPoolSelector)
	if !ok {
		return nil, nil
	}
	proxy, err := scoped.SelectRandomActiveProxyFromPool(ctx, ids)
	if err != nil {
		return nil, err
	}
	if proxy != nil && !slices.Contains(ids, proxy.ID) {
		return nil, nil
	}
	return proxy, err
}

// 组成员每次从数据库解析，组的增删成员会在下一次选路立即生效。
// 空组及无法解析的组仍为受限空池，不能退化成全局随机。
func ResolveAccountProxyPoolSelection(ctx context.Context, a *Account, source any) (ProxyPoolSelection, error) {
	selection := ProxyPoolSelection{AccountID: a.ID, MaxReuseDuration: a.RandomProxyMaxReuseDuration()}
	country, err := a.ProxyRegionCountry()
	if err != nil {
		return selection, err
	}
	selection.CountryCode = country
	switch a.RandomProxyPoolScope() {
	case RandomProxyPoolSelected:
		selection.Restricted, selection.IDs = true, a.RandomProxyPoolIDs()
	case RandomProxyPoolGroup:
		selection.Restricted = true
		resolver, ok := source.(RandomProxyGroupResolver)
		if !ok || a.RandomProxyGroupID() <= 0 {
			return selection, nil
		}
		ids, err := resolver.GetRandomProxyGroupIDs(ctx, a.RandomProxyGroupID())
		if err != nil {
			return selection, err
		}
		selection.IDs = ids
	}
	return selection, nil
}
