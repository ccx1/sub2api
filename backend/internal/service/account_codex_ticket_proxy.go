package service

import (
	"context"
	"encoding/json"
	"maps"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	CodexTicketProxyModeExtraKey = "codex_ticket_proxy_mode"
	CodexTicketProxyIDExtraKey   = "codex_ticket_proxy_id"
	CodexTicketProxyModeInherit  = "inherit"
	CodexTicketProxyModeAccount  = "account"
	CodexTicketProxyModeRandom   = "random"
	CodexTicketProxyModeFixed    = "fixed"
)

func (a *Account) CodexTicketProxyMode() string {
	if a == nil || a.Extra[CodexTicketProxyModeExtraKey] == nil {
		return CodexTicketProxyModeInherit
	}
	mode, _ := a.Extra[CodexTicketProxyModeExtraKey].(string)
	return strings.TrimSpace(mode)
}

func (a *Account) CodexTicketProxyID() int64 {
	if a == nil {
		return 0
	}
	id, _ := codexTicketProxyID(a.Extra[CodexTicketProxyIDExtraKey])
	return id
}

func codexTicketProxyID(raw any) (int64, bool) {
	if raw == nil {
		return 0, true
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return 0, false
	}
	id, err := strconv.ParseInt(string(encoded), 10, 64)
	return id, err == nil && id >= 0 && id <= 1<<53-1
}

// 对完整配置校验；局部更新先与账号已有配置合并。
func ValidateCodexTicketProxyExtra(extra map[string]any) error {
	account := &Account{Extra: extra}
	mode := account.CodexTicketProxyMode()
	if mode != CodexTicketProxyModeInherit && mode != CodexTicketProxyModeAccount && mode != CodexTicketProxyModeRandom && mode != CodexTicketProxyModeFixed {
		return infraerrors.BadRequest("INVALID_CODEX_TICKET_PROXY_MODE", "打票代理模式必须是跟随账号、跟随全局、随机或固定代理")
	}
	id, valid := codexTicketProxyID(extra[CodexTicketProxyIDExtraKey])
	if !valid || mode == CodexTicketProxyModeFixed && id <= 0 || mode != CodexTicketProxyModeFixed && id != 0 {
		return infraerrors.BadRequest("INVALID_CODEX_TICKET_PROXY_ID", "固定打票代理必须选择有效代理，其他模式不能指定代理 ID")
	}
	return nil
}

func mergeCodexTicketProxyExtra(extra, current map[string]any) map[string]any {
	mode, modeSupplied := extra[CodexTicketProxyModeExtraKey]
	_, idSupplied := extra[CodexTicketProxyIDExtraKey]
	for _, key := range []string{CodexTicketProxyModeExtraKey, CodexTicketProxyIDExtraKey} {
		if _, supplied := extra[key]; supplied {
			continue
		}
		if value, exists := current[key]; exists {
			if extra == nil {
				extra = make(map[string]any)
			}
			extra[key] = value
		}
	}
	if modeSupplied && !idSupplied && (mode == nil || mode == CodexTicketProxyModeInherit || mode == CodexTicketProxyModeAccount || mode == CodexTicketProxyModeRandom) {
		extra[CodexTicketProxyIDExtraKey] = int64(0)
	}
	return extra
}

func hasCodexTicketProxyUpdates(extra map[string]any) bool {
	_, mode := extra[CodexTicketProxyModeExtraKey]
	_, id := extra[CodexTicketProxyIDExtraKey]
	return mode || id
}

func normalizeCodexTicketProxyUpdate(extra, current map[string]any) (map[string]any, error) {
	normalized := mergeCodexTicketProxyExtra(maps.Clone(extra), current)
	return normalized, ValidateCodexTicketProxyExtra(normalized)
}

// 校验快照不代表更新意图；省略字段交由仓储在行锁内读取，避免覆盖并发设置。
func codexTicketProxyExplicitExtra(extra, requested map[string]any) map[string]any {
	result := maps.Clone(extra)
	for _, key := range []string{CodexTicketProxyModeExtraKey, CodexTicketProxyIDExtraKey} {
		if _, supplied := requested[key]; !supplied {
			delete(result, key)
		}
	}
	return mergeCodexTicketProxyExtra(result, nil)
}

func (s *adminServiceImpl) validateCodexTicketProxyAvailable(ctx context.Context, extra map[string]any) error {
	if err := ValidateCodexTicketProxyExtra(extra); err != nil {
		return err
	}
	account := &Account{Extra: extra}
	if account.CodexTicketProxyMode() != CodexTicketProxyModeFixed {
		return nil
	}
	if s.proxyRepo == nil {
		return infraerrors.BadRequest("CODEX_TICKET_PROXY_UNAVAILABLE", "固定打票代理不可用")
	}
	proxy, err := s.proxyRepo.GetByID(ctx, account.CodexTicketProxyID())
	if err != nil || !codexTicketProxyAvailable(proxy) || proxy.ID != account.CodexTicketProxyID() {
		return infraerrors.BadRequest("CODEX_TICKET_PROXY_UNAVAILABLE", "固定打票代理不存在、已停用或已过期")
	}
	return nil
}

func (s *adminServiceImpl) validateCodexTicketProxyAccountUpdate(ctx context.Context, account *Account, extra map[string]any) error {
	if !hasCodexTicketProxyUpdates(extra) {
		return nil
	}
	if !isOpenAICodexTicketAccount(account) {
		return infraerrors.BadRequest("CODEX_TICKET_PROXY_ACCOUNT_UNSUPPORTED", "仅支持为非影子 OpenAI OAuth 账号设置打票代理")
	}
	return s.validateCodexTicketProxyAvailable(ctx, extra)
}

func codexTicketProxyAvailable(proxy *Proxy) bool {
	return proxy != nil && proxy.IsActive() && !proxy.IsExpired(time.Now()) &&
		strings.TrimSpace(proxy.Host) != "" && proxy.Port > 0 && ValidateOpenAICodexTicketHarvestProxyURL(proxy.URL()) == nil
}
