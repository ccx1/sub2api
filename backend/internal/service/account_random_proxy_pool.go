package service

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"slices"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	RandomProxyPoolScopeExtraKey = "random_proxy_pool_scope"
	RandomProxyPoolIDsExtraKey   = "random_proxy_pool_ids"
	RandomProxyPoolAll           = "all"
	RandomProxyPoolSelected      = "selected"
)

type RandomProxyPoolSelector interface {
	SelectRandomActiveProxyFromPool(ctx context.Context, ids []int64) (*Proxy, error)
}

func (a *Account) RandomProxyPoolScope() string {
	if a == nil {
		return RandomProxyPoolAll
	}
	scope, _ := a.Extra[RandomProxyPoolScopeExtraKey].(string)
	if strings.EqualFold(strings.TrimSpace(scope), RandomProxyPoolSelected) {
		return RandomProxyPoolSelected
	}
	return RandomProxyPoolAll
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
		if !ok || (strings.TrimSpace(value) != RandomProxyPoolAll && strings.TrimSpace(value) != RandomProxyPoolSelected) {
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
}

func selectAccountRandomProxy(ctx context.Context, a *Account, selector RandomProxySelector) (*Proxy, error) {
	if balanced, ok := selector.(BalancedProxySelector); ok {
		selection := ProxyPoolSelection{
			AccountID: a.ID, Restricted: a.RandomProxyPoolScope() == RandomProxyPoolSelected,
			MaxReuseDuration: a.RandomProxyMaxReuseDuration(),
		}
		if selection.Restricted {
			selection.IDs = a.RandomProxyPoolIDs()
			if len(selection.IDs) == 0 {
				return nil, nil
			}
		}
		proxy, err := balanced.SelectBalancedProxy(ctx, selection)
		if selection.Restricted && proxy != nil && !slices.Contains(selection.IDs, proxy.ID) {
			return nil, nil
		}
		return proxy, err
	}
	if a.RandomProxyPoolScope() != RandomProxyPoolSelected {
		return selector.SelectRandomActiveProxy(ctx)
	}
	ids := a.RandomProxyPoolIDs()
	if len(ids) == 0 {
		return nil, nil
	}
	scoped, ok := selector.(RandomProxyPoolSelector)
	if !ok {
		return nil, nil
	}
	proxy, err := scoped.SelectRandomActiveProxyFromPool(ctx, ids)
	if proxy != nil && !slices.Contains(ids, proxy.ID) {
		return nil, nil
	}
	return proxy, err
}
