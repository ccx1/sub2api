package service

import (
	"slices"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func usableCodexTicketPool(inventory *openAICodexTicket, account *Account, cfg config.OpenAICodexTicketConfig, now time.Time) []*openAICodexTicket {
	var tickets []*openAICodexTicket
	seen := make(map[string]bool)
	for _, ticket := range codexTicketSlots(inventory) {
		if ticket.usable(now, account, cfg) && !seen[ticket.credentialIdentity()] {
			seen[ticket.credentialIdentity()] = true
			copy := codexTicketLeaf(ticket)
			copy.ExpiresAt = ticket.effectiveExpiresAt(resolveCodexTicketCredentialConfig(account, cfg))
			tickets = append(tickets, copy)
		}
	}
	return tickets
}

func composeCodexTicketPool(tickets []*openAICodexTicket, cfg config.OpenAICodexTicketConfig) *openAICodexTicket {
	if len(tickets) == 0 {
		return nil
	}
	capacity := config.NormalizeOpenAICodexTicketConfig(cfg).PoolCapacity
	for len(tickets) > capacity {
		// 保留当前票；空间不足时淘汰最早到期的备用，容量为一时直接续票。
		remove := 0
		if capacity > 1 {
			remove = 1
		}
		for index := remove + 1; index < len(tickets); index++ {
			if tickets[index].ExpiresAt.Before(tickets[remove].ExpiresAt) {
				remove = index
			}
		}
		tickets = slices.Delete(tickets, remove, remove+1)
	}
	current := codexTicketLeaf(tickets[0])
	if len(tickets) > 1 {
		current.Standby = codexTicketLeaf(tickets[1])
	}
	for _, ticket := range tickets[min(2, len(tickets)):] {
		current.Reserve = append(current.Reserve, codexTicketLeaf(ticket))
	}
	return current
}

func codexTicketPoolNeedsRefresh(inventory *openAICodexTicket, account *Account, cfg config.OpenAICodexTicketConfig, now time.Time) bool {
	cfg = resolveCodexTicketCredentialConfig(account, config.NormalizeOpenAICodexTicketConfig(cfg))
	inventory = cloneCodexTicketInventory(inventory)
	hydrateCodexTicketSoftRevalidate(inventory, cfg)
	tickets := usableCodexTicketPool(inventory, account, cfg, now)
	if len(tickets) < cfg.PoolCapacity {
		return true
	}
	refreshBefore := time.Duration(cfg.RefreshBeforeSeconds) * time.Second
	if cfg.RefreshStrategy == config.CodexTicketRefreshReplace {
		for _, ticket := range tickets {
			if ticket.needsRefresh(now, refreshBefore) {
				return true
			}
		}
		return false
	}
	for _, ticket := range tickets {
		if ticket.needsRefresh(now, 0) {
			return true
		}
	}
	if cfg.PoolCapacity == 1 {
		return tickets[0].needsRefresh(now, refreshBefore)
	}
	freshBackups := 0
	for _, ticket := range tickets[1:] {
		if !ticket.needsRefresh(now, refreshBefore) {
			freshBackups++
		}
	}
	return freshBackups < cfg.PoolCapacity-1
}

func normalizeCodexTicketPool(inventory *openAICodexTicket) {
	normalize := func(ticket *openAICodexTicket) *openAICodexTicket {
		if ticket == nil || ticket.Model != "" && normalizeOpenAICodexTicketModel(ticket.Model) != inventory.Model {
			return nil
		}
		leaf := codexTicketLeaf(ticket)
		leaf.AccountID, leaf.Model = inventory.AccountID, inventory.Model
		leaf.State = strings.TrimSpace(leaf.State)
		if leaf.State == "" && !leaf.usesCookies() {
			return nil
		}
		if leaf.Length == 0 {
			leaf.Length = len(leaf.State)
		}
		return leaf
	}
	inventory.Standby = normalize(inventory.Standby)
	var reserve []*openAICodexTicket
	for _, ticket := range inventory.Reserve {
		if len(reserve) >= config.MaxCodexTicketPoolCapacity-2 {
			break
		}
		if leaf := normalize(ticket); leaf != nil {
			reserve = append(reserve, leaf)
		}
	}
	inventory.Reserve = reserve
}

// 旧快照没有软复验时间时，按配置补齐；调用方必须先克隆共享库存。
func hydrateCodexTicketSoftRevalidate(inventory *openAICodexTicket, cfg config.OpenAICodexTicketConfig) {
	if inventory == nil || cfg.TTLSeconds <= 0 {
		return
	}
	for _, ticket := range codexTicketSlots(inventory) {
		if ticket == nil || !ticket.RevalidateAt.IsZero() {
			continue
		}
		validatedAt := ticket.RevalidatedAt
		if validatedAt.IsZero() {
			validatedAt = ticket.CapturedAt
		}
		if validatedAt.IsZero() {
			continue
		}
		seconds := cfg.TTLSeconds
		if ticket.usesCookies() && cfg.CookieTTLSeconds > 0 && cfg.CookieTTLSeconds < seconds {
			seconds = cfg.CookieTTLSeconds
		}
		if seconds > 0 {
			ticket.RevalidateAt = validatedAt.Add(time.Duration(seconds) * time.Second)
		}
	}
}

func codexTicketPrimaryReason(ticket *openAICodexTicket, account *Account, cfg config.OpenAICodexTicketConfig, now time.Time) string {
	if ticket == nil {
		return "missing"
	}
	if ticket.Revoked {
		return "revoked"
	}
	if !ticket.accountCompatible(account) {
		return "binding"
	}
	resolved := resolveCodexTicketCredentialConfig(account, cfg)
	if config.CodexTicketUsesCookies(resolved) {
		if !ticket.usesCookies() || ticket.CredentialMode != resolved.CredentialMode {
			return "credential"
		}
		if len(ticket.Cookies) == 0 {
			return "cookie_missing"
		}
		if !ticket.effectiveExpiresAt(resolved).After(now) {
			return "expired"
		}
	} else if ticket.usesCookies() {
		return "credential"
	} else if !ticket.hardExpiresAt().After(now) {
		return "expired"
	}
	if ticket.VerificationSkipped && config.CodexTicketBusinessVerificationEnabled(resolved) {
		return "unverified"
	}
	if !ticket.usable(now, account, resolved) {
		return "unavailable"
	}
	return ""
}

func codexTicketPoolStatus(model string, inventory *openAICodexTicket, account *Account, cfg config.OpenAICodexTicketConfig, now time.Time) OpenAICodexTicketStatus {
	cfg = resolveCodexTicketCredentialConfig(account, cfg)
	inventory = cloneCodexTicketInventory(inventory)
	hydrateCodexTicketSoftRevalidate(inventory, cfg)
	tickets := usableCodexTicketPool(inventory, account, cfg, now)
	status := OpenAICodexTicketStatus{Model: model, Capacity: cfg.PoolCapacity, CredentialState: "missing",
		AvailableCount: len(tickets), ReserveCount: max(0, len(tickets)-1), Blocked: cfg.FailClosed && len(tickets) == 0}
	setCodexTicketPrimaryStatus(&status, inventory, account, cfg, now)
	if len(tickets) == 0 {
		if status.PrimaryReason == "expired" || status.PrimaryReason == "revoked" {
			status.CredentialState = status.PrimaryReason
		}
		return status
	}
	current := tickets[0]
	status.Ready, status.Length, status.ExpiresAt = true, current.Length, &current.ExpiresAt
	status.RemainingSeconds = int64(current.ExpiresAt.Sub(now) / time.Second)
	status.UsingStandby = !sameCodexTicket(current, inventory)
	status.CredentialState = "available"
	if !current.RevalidateAt.IsZero() {
		status.RevalidateAt = &current.RevalidateAt
	}
	status.RevalidationRequired = current.needsRefresh(now, 0)
	if status.RevalidationRequired {
		status.CredentialState = "revalidation_required"
	}
	for _, ticket := range tickets {
		if !ticket.ExpiresAt.After(now.Add(time.Duration(cfg.RefreshBeforeSeconds) * time.Second)) {
			status.ExpiringCount++
		}
		if status.NextExpiresAt == nil || ticket.ExpiresAt.Before(*status.NextExpiresAt) {
			status.NextExpiresAt = &ticket.ExpiresAt
		}
		if !sameCodexTicket(ticket, inventory) && !status.StandbyReady {
			status.StandbyReady, status.StandbyExpiresAt = true, &ticket.ExpiresAt
		}
	}
	return status
}

func setCodexTicketPrimaryStatus(status *OpenAICodexTicketStatus, inventory *openAICodexTicket, account *Account, cfg config.OpenAICodexTicketConfig, now time.Time) {
	if inventory != nil {
		status.PrimaryPresent = true
		status.PrimaryReason = codexTicketPrimaryReason(inventory, account, cfg, now)
		status.PrimaryReady = status.PrimaryReason == ""
		primaryExpiry := inventory.effectiveExpiresAt(cfg)
		if !primaryExpiry.IsZero() {
			status.PrimaryExpiresAt = &primaryExpiry
			status.PrimaryRemainingSeconds = int64(primaryExpiry.Sub(now) / time.Second)
			if status.PrimaryRemainingSeconds < 0 {
				status.PrimaryRemainingSeconds = 0
			}
		}
	}
}
