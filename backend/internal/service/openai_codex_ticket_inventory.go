package service

import (
	"crypto/sha256"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func codexTicketLeaf(ticket *openAICodexTicket) *openAICodexTicket {
	if ticket == nil {
		return nil
	}
	copy := *ticket
	// 发送投影不进入库存或候选；原票仍按完整凭据复验。
	copy.CookieMode, copy.CookiePolicyVerified, copy.CookiePolicyProofKey = "", false, ""
	copy.CookieSessionKeys = append([]string(nil), ticket.CookieSessionKeys...)
	copy.Cookies = nil
	for _, cookie := range ticket.Cookies {
		if cookie != nil {
			value := *cookie
			copy.Cookies = append(copy.Cookies, &value)
		}
	}
	copy.Standby = nil
	copy.Reserve = nil
	return &copy
}

func cloneCodexTicketInventory(ticket *openAICodexTicket) *openAICodexTicket {
	copy := codexTicketLeaf(ticket)
	if copy != nil {
		copy.Standby = codexTicketLeaf(ticket.Standby)
		for _, reserve := range ticket.Reserve {
			copy.Reserve = append(copy.Reserve, codexTicketLeaf(reserve))
		}
	}
	return copy
}

// lineageCapturedAt 返回凭据谱系的稳定采集时间：软复验刷新 CapturedAt 时，
// OriginCapturedAt 仍指向首次采集时间。旧数据没有该字段时回退到 CapturedAt。
func (t *openAICodexTicket) lineageCapturedAt() time.Time {
	if t == nil {
		return time.Time{}
	}
	if !t.OriginCapturedAt.IsZero() {
		return t.OriginCapturedAt
	}
	return t.CapturedAt
}

// sameCodexTicketLineage 判断两张票是否属于同一条凭据谱系。用于质量检测与路由
// 状态：它们只关心“同一把票是否仍在服务”，而不应被每约 20s 一次的 Cookie 软复验
// （会改变 credentialIdentity 与 CapturedAt）重置。
func sameCodexTicketLineage(a, b *openAICodexTicket) bool {
	if a == nil || b == nil {
		return false
	}
	if a.AccountID != b.AccountID || a.Model != b.Model || a.SessionID != b.SessionID ||
		a.Egress != b.Egress || a.CredentialMode != b.CredentialMode {
		return false
	}
	origin := a.lineageCapturedAt()
	return !origin.IsZero() && origin.Equal(b.lineageCapturedAt())
}

func sameCodexTicket(a, b *openAICodexTicket) bool {
	return a != nil && b != nil && a.credentialIdentity() == b.credentialIdentity() && a.CapturedAt.Equal(b.CapturedAt)
}

func codexTicketSlots(inventory *openAICodexTicket) []*openAICodexTicket {
	if inventory == nil {
		return nil
	}
	slots := []*openAICodexTicket{inventory}
	if inventory.Standby != nil {
		slots = append(slots, inventory.Standby)
	}
	for _, ticket := range inventory.Reserve {
		if ticket != nil {
			slots = append(slots, ticket)
		}
	}
	return slots
}

func codexTicketInventoryContains(inventory, ticket *openAICodexTicket) bool {
	for _, slot := range codexTicketSlots(inventory) {
		if sameCodexTicket(slot, ticket) {
			return true
		}
	}
	return false
}

func codexTicketInventoryTime(inventory *openAICodexTicket) time.Time {
	var latest time.Time
	for _, slot := range codexTicketSlots(inventory) {
		if slot != nil && slot.CapturedAt.After(latest) {
			latest = slot.CapturedAt
		}
	}
	return latest
}

// 只返回实际发送的叶票，receipt 不能携带之后可能变化的库存关系。
func selectOpenAICodexTicket(inventory *openAICodexTicket, account *Account, cfg config.OpenAICodexTicketConfig, now time.Time) *openAICodexTicket {
	for _, slot := range codexTicketSlots(inventory) {
		if slot.usable(now, account, cfg) {
			return codexTicketLeaf(slot)
		}
	}
	return nil
}

type codexTicketRevocationKey struct {
	state    [32]byte
	captured string
}

type codexTicketRevocationIndexKey string

func codexTicketExactRevocationKey(ticket *openAICodexTicket) codexTicketRevocationKey {
	return codexTicketRevocationKey{sha256.Sum256([]byte(ticket.credentialIdentity())), ticket.CapturedAt.UTC().Format(time.RFC3339Nano)}
}

func (s *OpenAIGatewayService) rememberCodexTicketRevocation(key string, ticket *openAICodexTicket) {
	if ticket == nil {
		return
	}
	indexKey := codexTicketRevocationIndexKey(key)
	index := make(map[codexTicketRevocationKey]time.Time)
	if raw, ok := s.openaiCodexTicketRevoked.Load(indexKey); ok {
		for identity, expires := range raw.(map[codexTicketRevocationKey]time.Time) {
			if expires.After(time.Now()) {
				index[identity] = expires
			}
		}
	}
	index[codexTicketExactRevocationKey(ticket)] = ticket.ExpiresAt
	s.openaiCodexTicketRevoked.Store(indexKey, index)
	// 兼容历史观测水位；是否撤票只按精确身份判断，不能误杀更早采集的备用。
	if cutoff, ok := s.openaiCodexTicketRevoked.Load(key); !ok || ticket.CapturedAt.After(cutoff.(time.Time)) {
		s.openaiCodexTicketRevoked.Store(key, ticket.CapturedAt)
	}
}

// 调用者持有该账号模型的分片锁。备用变更也参与新旧判断；撤票信息只合并命中槽。
func (s *OpenAIGatewayService) codexTicketInventoryLocked(account *Account, model string) *openAICodexTicket {
	key := openAICodexTicketKey(account.ID, model)
	var mem *openAICodexTicket
	if raw, ok := s.openaiCodexTickets.Load(key); ok {
		mem, _ = raw.(*openAICodexTicket)
	}
	extra := parseOpenAICodexTicketFromAny(account.ID, model, account.Extra[openAICodexTicketExtraKey(model)])
	current := mem
	if current == nil || codexTicketInventoryTime(extra).After(codexTicketInventoryTime(current)) {
		current = extra
	}
	current = cloneCodexTicketInventory(current)
	refreshCodexTicketAccountBindings(current, extra, account)
	for _, source := range []*openAICodexTicket{mem, extra} {
		for _, old := range codexTicketSlots(source) {
			if old == nil || !old.Revoked {
				continue
			}
			s.rememberCodexTicketRevocation(key, old)
			for _, slot := range codexTicketSlots(current) {
				if sameCodexTicket(slot, old) {
					slot.Revoked = true
				}
			}
		}
	}
	if current != nil {
		s.openaiCodexTickets.Store(key, current)
	}
	return current
}

// 保护更新不改变采集时间；相同票的持久绑定可刷新旧缓存，撤销状态仍在下方合并。
func refreshCodexTicketAccountBindings(inventory, persisted *openAICodexTicket, account *Account) {
	for _, ticket := range codexTicketSlots(inventory) {
		if ticket.accountCompatible(account) {
			continue
		}
		for _, updated := range codexTicketSlots(persisted) {
			if sameCodexTicket(ticket, updated) && updated.accountCompatible(account) {
				ticket.AccountBinding = updated.AccountBinding
				break
			}
		}
	}
}

func (s *OpenAIGatewayService) availableCodexTicketInventory(key string, inventory *openAICodexTicket) *openAICodexTicket {
	copy := cloneCodexTicketInventory(inventory)
	for _, slot := range codexTicketSlots(copy) {
		if slot != nil && s.codexTicketRevoked(key, slot) {
			slot.Revoked = true
		}
	}
	return copy
}

func (s *OpenAIGatewayService) codexTicketInventoryNeedsRefresh(account *Account, model string, cfg config.OpenAICodexTicketConfig, now time.Time) bool {
	if s == nil || account == nil {
		return false
	}
	if !OpenAICodexTicketAccountEnabled(account) || !codexTicketConfigGatesModel(cfg, model) {
		return false
	}
	key := openAICodexTicketKey(account.ID, model)
	lock := s.codexTicketLock(key)
	lock.Lock()
	defer lock.Unlock()
	inventory := s.availableCodexTicketInventory(key, s.codexTicketInventoryLocked(account, model))
	if cfg.RefreshStrategy != config.CodexTicketRefreshReplace {
		if pending := s.pendingCodexTicketCookies(account.ID, model); pending != nil && codexTicketInventoryContains(inventory, pending.Ticket) {
			return true
		}
	}
	return codexTicketPoolNeedsRefresh(inventory, account, cfg, now)
}

func revokeCodexTicketSlot(inventory, used *openAICodexTicket) *openAICodexTicket {
	copy := cloneCodexTicketInventory(inventory)
	for _, slot := range codexTicketSlots(copy) {
		if sameCodexTicket(slot, used) {
			slot.Revoked, slot.Invalidation = true, used.Invalidation
		}
	}
	return copy
}

func mergeCodexTicketPublication(inventory, incoming *openAICodexTicket, account *Account, cfg config.OpenAICodexTicketConfig) *openAICodexTicket {
	cfg = resolveCodexTicketCredentialConfig(account, config.NormalizeOpenAICodexTicketConfig(cfg))
	now := time.Now()
	tickets := usableCodexTicketPool(inventory, account, cfg, now)
	for index, ticket := range tickets {
		if ticket.credentialIdentity() != incoming.credentialIdentity() {
			continue
		}
		// 重复采集不能延长旧票有效期或改变在途请求对应的票据身份。
		if sameCodexTicket(ticket, incoming) {
			tickets[index] = incoming
		}
		return composeCodexTicketPool(tickets, cfg)
	}
	if cfg.RefreshStrategy == config.CodexTicketRefreshReplace {
		for index, ticket := range tickets {
			hydrateCodexTicketSoftRevalidate(ticket, cfg)
			if ticket.needsRefresh(now, time.Duration(cfg.RefreshBeforeSeconds)*time.Second) {
				// 按软期限原位更新；仅按硬期限淘汰会丢掉新票并持续触发新采。
				tickets[index] = incoming
				return composeCodexTicketPool(tickets, cfg)
			}
		}
	}
	return composeCodexTicketPool(append(tickets, incoming), cfg)
}
