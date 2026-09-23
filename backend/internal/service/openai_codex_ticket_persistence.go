package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash/fnv"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

type codexTicketCompareAndSwapper interface {
	CompareAndSwapCodexTicket(context.Context, *Account, string, any) (bool, error)
}

func openAICodexTicketEgress(proxyURL string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(proxyURL)))
	return hex.EncodeToString(digest[:])
}

func (s *OpenAIGatewayService) codexTicketBinding(account *Account) string {
	return s.codexTicketBindingForConfig(account, s.openAICodexTicketConfig())
}

func (s *OpenAIGatewayService) codexTicketBindingForConfig(account *Account, cfg config.OpenAICodexTicketConfig) string {
	cfg = resolveCodexTicketCredentialConfig(account, cfg)
	if !config.CodexTicketUsesCookies(cfg) {
		cfg.CredentialMode, cfg.CookieTTLSeconds, cfg.CookieRefreshBeforeSeconds = "", 0, nil
	}
	account = account.ConfiguredProxySnapshot()
	cfg.Enabled = false
	// 新调度配置由独立准入版本保护；不能改变旧二进制计算的票据绑定。
	cfg.Protection = nil
	cfg.VerifyBusiness = nil
	cfg.BusinessVerificationRounds = 0
	// 更新策略只决定采集时机，不改变已发布票据的身份。
	cfg.RefreshStrategy = ""
	// 库存容量只控制补票数量，不改变已发布票的身份绑定。
	cfg.PoolCapacity = 0
	// 换绑阈值只控制采集调度，保持升级前有效票据的绑定编码。
	cfg.ProxyFailureThreshold = 0
	// 默认随机模式保持升级前的绑定编码；固定模式参与在途发布校验。
	if cfg.SessionMode == config.CodexTicketSessionRandom {
		cfg.SessionMode = ""
	}
	// 采集出口独立核验，不参与业务票据或通用规则绑定。
	cfg.HarvestProxyURL = ""
	extra := make(map[string]any)
	for _, key := range []string{ProxyModeExtraKey, OpenAICodexTicketEnabledExtraKey,
		RandomProxyEmptyPoolPolicyExtraKey, RandomProxyPoolScopeExtraKey,
		RandomProxyPoolIDsExtraKey, RandomProxyGroupIDExtraKey, RandomProxyMaxReuseMinutesExtraKey,
		DailyCooldownExtraKey, "enable_tls_fingerprint", "tls_fingerprint_builtin", "tls_fingerprint_profile_id",
		"codex_fingerprint_mode", AntiDegradeMarkerExtraKey, AntiDegradationExtraKey} {
		extra[key] = account.Extra[key]
	}
	identity := map[string]any{"account": account.ID, "platform": account.Platform, "type": account.Type, "extra": extra,
		"chatgpt_account_id": account.Credentials["chatgpt_account_id"], "organization_id": account.Credentials["organization_id"],
		"chatgpt_organization_id": account.Credentials["chatgpt_organization_id"], "config": cfg,
		"subscription_tier": openAICodexTicketSubscriptionTier(account)}
	identity["tls_enabled"] = s.cfg == nil || s.cfg.Gateway.TLSFingerprint.Enabled
	if !account.IsRandomProxy() {
		identity["proxy_id"], identity["proxy_url"] = account.ProxyID, resolveAccountProxyURL(account)
	}
	encoded, _ := json.Marshal(identity)
	return openAICodexTicketEgress(string(encoded))
}

func (s *OpenAIGatewayService) codexTicketLock(key string) *sync.Mutex {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return &s.openaiCodexTicketLocks[h.Sum32()%uint32(len(s.openaiCodexTicketLocks))]
}

func (s *OpenAIGatewayService) codexTicketRevoked(key string, ticket *openAICodexTicket) bool {
	if ticket == nil {
		return false
	}
	if ticket.Revoked {
		return true
	}
	index, ok := s.openaiCodexTicketRevoked.Load(codexTicketRevocationIndexKey(key))
	if !ok {
		return false
	}
	_, revoked := index.(map[codexTicketRevocationKey]time.Time)[codexTicketExactRevocationKey(ticket)]
	return revoked
}

func (s *OpenAIGatewayService) lookupOpenAICodexTicket(account *Account, model string) *openAICodexTicket {
	return s.lookupOpenAICodexTicketForConfig(account, model, s.openAICodexTicketConfig())
}

func (s *OpenAIGatewayService) lookupOpenAICodexTicketForConfig(account *Account, model string, cfg config.OpenAICodexTicketConfig) *openAICodexTicket {
	if s == nil || account == nil || account.ID <= 0 || strings.TrimSpace(model) == "" {
		return nil
	}
	model = normalizeOpenAICodexTicketModel(model)
	key := openAICodexTicketKey(account.ID, model)
	lock := s.codexTicketLock(key)
	lock.Lock()
	defer lock.Unlock()
	inventory := s.availableCodexTicketInventory(key, s.codexTicketInventoryLocked(account, model))
	if inventory == nil {
		return nil
	}
	if selected := selectOpenAICodexTicket(inventory, account, cfg, time.Now()); selected != nil {
		return selected
	}
	for _, slot := range codexTicketSlots(inventory) {
		if slot != nil && !slot.Revoked && slot.accountCompatible(account) {
			return codexTicketLeaf(slot)
		}
	}
	return nil
}

// 返回成功前先持久化，避免写库失败时把旧可用票替换成仅存在于本机的新票。
func (s *OpenAIGatewayService) storeOpenAICodexTicket(ctx context.Context, account *Account, ticket *openAICodexTicket) bool {
	if s == nil || account == nil || ticket == nil || account.ID <= 0 || ctx.Err() != nil {
		return false
	}
	// 与同实例管理端保存串行，覆盖最终规则核验、数据库写入和本机发布。
	if (ticket.Verified || ticket.VerificationSkipped) && s.settingService != nil {
		s.settingService.codexTicketPublishMu.RLock()
		defer s.settingService.codexTicketPublishMu.RUnlock()
	}
	copyTicket := *codexTicketLeaf(ticket)
	copyTicket.Model, copyTicket.AccountID = normalizeOpenAICodexTicketModel(ticket.Model), account.ID
	key := openAICodexTicketKey(account.ID, copyTicket.Model)
	lock := s.codexTicketLock(key)
	lock.Lock()
	defer lock.Unlock()
	if s.codexTicketRevoked(key, &copyTicket) {
		return false
	}
	cfg := s.openAICodexTicketConfigForAccount(ctx, account)
	if copyTicket.Verified || copyTicket.VerificationSkipped {
		copyTicket.AccountBinding = openAICodexTicketAccountBinding(account)
		if !cfg.Enabled || !slices.Contains(cfg.Models, copyTicket.Model) || !copyTicket.usable(time.Now(), account, cfg) ||
			copyTicket.Binding != s.codexTicketBindingForConfig(account, cfg) {
			return false
		}
	}
	inventory := s.codexTicketInventoryLocked(account, copyTicket.Model)
	if codexTicketInventoryTime(inventory).After(copyTicket.CapturedAt) {
		return false
	}
	available := s.availableCodexTicketInventory(key, inventory)
	replacement := mergeCodexTicketPublication(available, &copyTicket, account, cfg)
	snapshot := cloneOpenAICodexTicketAccount(account)
	if _, conditional := s.accountRepo.(codexTicketCompareAndSwapper); !conditional && inventory != nil {
		snapshot.Extra[openAICodexTicketExtraKey(copyTicket.Model)] = inventory
	}
	if !s.persistOpenAICodexTicket(ctx, snapshot, copyTicket.Model, replacement) {
		return false
	}
	s.openaiCodexTickets.Store(key, replacement)
	return true
}

func (s *OpenAIGatewayService) persistOpenAICodexTicket(ctx context.Context, account *Account, model string, replacement any) bool {
	updated, err := s.persistOpenAICodexTicketResult(ctx, account, model, replacement)
	return updated && err == nil
}

func (s *OpenAIGatewayService) persistOpenAICodexTicketResult(ctx context.Context, account *Account, model string, replacement any) (bool, error) {
	if s.accountRepo == nil {
		return true, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if repo, ok := s.accountRepo.(codexTicketCompareAndSwapper); ok {
		updated, err := repo.CompareAndSwapCodexTicket(ctx, account, model, replacement)
		if err != nil {
			logger.L().Warn("openai_codex_ticket conditional persistence failed", zap.Int64("account_id", account.ID))
		}
		return updated, err
	}
	// 兼容测试替身及旧仓储；同进程发布/撤销仍由同一分片锁串行。
	if used, ok := replacement.(*openAICodexTicket); ok && used.Revoked {
		inventory := parseOpenAICodexTicketFromAny(account.ID, model, account.Extra[openAICodexTicketExtraKey(model)])
		replacement = revokeCodexTicketSlot(inventory, used)
	}
	err := s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{openAICodexTicketExtraKey(model): replacement})
	if err != nil {
		logger.L().Warn("openai_codex_ticket persistence failed", zap.Int64("account_id", account.ID))
	}
	return err == nil, err
}

func (s *OpenAIGatewayService) invalidateOpenAICodexTicket(ctx context.Context, account *Account, used *openAICodexTicket, details ...*CodexTicketInvalidation) {
	var detail *CodexTicketInvalidation
	if len(details) > 0 {
		detail = details[0]
	}
	used = codexTicketWithInvalidation(used, detail)
	// 一次瞬时写库失败可立即重试；每次重试重新加锁核对，不能覆盖期间发布的新票。
	for range 2 {
		if !s.invalidateOpenAICodexTicketOnce(ctx, account, used) {
			return
		}
	}
}

func (s *OpenAIGatewayService) invalidateOpenAICodexTicketOnce(ctx context.Context, account *Account, used *openAICodexTicket) bool {
	if s == nil || account == nil || used == nil {
		return false
	}
	key := openAICodexTicketKey(account.ID, used.Model)
	lock := s.codexTicketLock(key)
	lock.Lock()
	defer lock.Unlock()
	inventory := s.codexTicketInventoryLocked(account, used.Model)
	if !codexTicketInventoryContains(inventory, used) {
		return false
	}
	// 写库失败仍阻断本机实际被拒绝的票，备用保持独立；同身份允许有界重试。
	s.rememberCodexTicketRevocation(key, used)
	snapshot := cloneOpenAICodexTicketAccount(account)
	keyExtra := openAICodexTicketExtraKey(used.Model)
	snapshot.Extra[keyExtra] = inventory
	tombstone := *codexTicketLeaf(used)
	tombstone.Revoked = true
	updated, err := s.persistOpenAICodexTicketResult(context.WithoutCancel(ctx), snapshot, used.Model, &tombstone)
	if updated && err == nil {
		s.openaiCodexTickets.Store(key, revokeCodexTicketSlot(inventory, used))
	}
	return err != nil
}

func cloneOpenAICodexTicketAccount(account *Account) *Account {
	copyAccount := *account
	copyAccount.Extra, copyAccount.Credentials = maps.Clone(account.Extra), maps.Clone(account.Credentials)
	if copyAccount.Extra == nil {
		copyAccount.Extra = make(map[string]any)
	}
	return &copyAccount
}

func (s *OpenAIGatewayService) codexTicketSchedulingAccount(ctx context.Context, account *Account) *Account {
	if ctx.Err() != nil {
		return nil
	}
	if s.schedulerSnapshot == nil || !account.CreatedAt.IsZero() || !account.UpdatedAt.IsZero() ||
		account.GetCredential("access_token") != "" || account.GetCredential("refresh_token") != "" || account.GetChatGPTAccountID() != "" {
		return account
	}
	// 调度 metadata 会省略代理、身份和票据。只补读完整快照，不提前选择随机出口。
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	full, err := s.schedulerSnapshot.GetAccount(ctx, account.ID)
	if err != nil || full == nil || full.ID != account.ID {
		return nil
	}
	return full
}
