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
	cfg.Enabled = false
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
	cutoff, ok := s.openaiCodexTicketRevoked.Load(key)
	return ok && !ticket.CapturedAt.After(cutoff.(time.Time))
}

func (s *OpenAIGatewayService) lookupOpenAICodexTicket(account *Account, model string) *openAICodexTicket {
	if s == nil || account == nil || account.ID <= 0 || strings.TrimSpace(model) == "" {
		return nil
	}
	model = normalizeOpenAICodexTicketModel(model)
	key := openAICodexTicketKey(account.ID, model)
	lock := s.codexTicketLock(key)
	lock.Lock()
	defer lock.Unlock()
	var mem *openAICodexTicket
	if raw, ok := s.openaiCodexTickets.Load(key); ok {
		mem, _ = raw.(*openAICodexTicket)
	}
	extra := parseOpenAICodexTicketFromAny(account.ID, model, account.Extra[openAICodexTicketExtraKey(model)])
	if extra != nil && extra.Revoked && (mem == nil || !mem.CapturedAt.After(extra.CapturedAt)) {
		// 旧调度快照只能维持或提高撤销水位，不能让更晚撤销的票复活。
		if !s.codexTicketRevoked(key, extra) {
			s.openaiCodexTicketRevoked.Store(key, extra.CapturedAt)
		}
		s.openaiCodexTickets.Delete(key)
		return nil
	}
	if extra != nil && !s.codexTicketRevoked(key, extra) && (mem == nil || extra.CapturedAt.After(mem.CapturedAt)) {
		mem = extra
		s.openaiCodexTickets.Store(key, mem)
	}
	if s.codexTicketRevoked(key, mem) || mem != nil && !mem.accountCompatible(account) {
		return nil
	}
	return mem
}

// 返回成功前先持久化，避免写库失败时把旧可用票替换成仅存在于本机的新票。
func (s *OpenAIGatewayService) storeOpenAICodexTicket(ctx context.Context, account *Account, ticket *openAICodexTicket) bool {
	if s == nil || account == nil || ticket == nil || account.ID <= 0 || ctx.Err() != nil {
		return false
	}
	// 与同实例管理端保存串行，覆盖最终规则核验、数据库写入和本机发布。
	if ticket.Verified && s.settingService != nil {
		s.settingService.codexTicketPublishMu.RLock()
		defer s.settingService.codexTicketPublishMu.RUnlock()
	}
	copyTicket := *ticket
	copyTicket.Model, copyTicket.AccountID = normalizeOpenAICodexTicketModel(ticket.Model), account.ID
	key := openAICodexTicketKey(account.ID, copyTicket.Model)
	lock := s.codexTicketLock(key)
	lock.Lock()
	defer lock.Unlock()
	if s.codexTicketRevoked(key, &copyTicket) {
		return false
	}
	if copyTicket.Verified {
		cfg := s.openAICodexTicketConfigContext(ctx)
		copyTicket.AccountBinding = openAICodexTicketAccountBinding(account)
		if !cfg.Enabled || !slices.Contains(cfg.Models, copyTicket.Model) || !copyTicket.usable(time.Now(), account, cfg) ||
			copyTicket.Binding != s.codexTicketBindingForConfig(account, cfg) {
			return false
		}
	}
	if raw, ok := s.openaiCodexTickets.Load(key); ok && raw.(*openAICodexTicket).CapturedAt.After(copyTicket.CapturedAt) {
		return false
	}
	if !s.persistOpenAICodexTicket(ctx, account, copyTicket.Model, &copyTicket) {
		return false
	}
	s.openaiCodexTickets.Store(key, &copyTicket)
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
	err := s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{openAICodexTicketExtraKey(model): replacement})
	if err != nil {
		logger.L().Warn("openai_codex_ticket persistence failed", zap.Int64("account_id", account.ID))
	}
	return err == nil, err
}

func (s *OpenAIGatewayService) invalidateOpenAICodexTicket(ctx context.Context, account *Account, used *openAICodexTicket) {
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
	if raw, ok := s.openaiCodexTickets.Load(key); ok {
		current := raw.(*openAICodexTicket)
		if current.State != used.State || !current.CapturedAt.Equal(used.CapturedAt) {
			return false
		}
	}
	if cutoff, ok := s.openaiCodexTicketRevoked.Load(key); ok && used.CapturedAt.Before(cutoff.(time.Time)) {
		return false
	}
	// 相同水位仍可重试写库；水位只证明本机禁用，不能证明上次持久化成功。
	s.openaiCodexTicketRevoked.Store(key, used.CapturedAt)
	s.openaiCodexTickets.Delete(key)
	snapshot := cloneOpenAICodexTicketAccount(account)
	keyExtra := openAICodexTicketExtraKey(used.Model)
	expected := parseOpenAICodexTicketFromAny(account.ID, used.Model, snapshot.Extra[keyExtra])
	if expected == nil || expected.State != used.State || !expected.CapturedAt.Equal(used.CapturedAt) {
		snapshot.Extra[keyExtra] = used
	}
	tombstone := *used
	tombstone.Revoked = true
	_, err := s.persistOpenAICodexTicketResult(context.WithoutCancel(ctx), snapshot, used.Model, &tombstone)
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
