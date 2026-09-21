package service

import (
	"context"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func validateSharedCodexTicketAccount(account *Account) error {
	if !isOpenAICodexTicketAccount(account) {
		return infraerrors.BadRequest("CODEX_TICKET_UNSUPPORTED_ACCOUNT", "打票开关仅支持非影子 OpenAI OAuth 账号")
	}
	return nil
}

func SharedPoolCodexTicketRequired(account *Account) bool {
	if !isOpenAICodexTicketAccount(account) || sharedPoolBillingOwnerID(account.Extra[SharedPoolOwnerKey]) <= 0 {
		return false
	}
	tier := sharedAccountSubscriptionTier(account)
	return tier == "pro" || tier == "prolite"
}

func normalizeSharedCodexTicket(account *Account) {
	if SharedPoolCodexTicketRequired(account) {
		account.Extra[OpenAICodexTicketEnabledExtraKey] = true
	}
}

func (s *SharedPoolService) SetCodexTicketEnabled(ctx context.Context, userID, id int64, enabled bool) error {
	_, account, err := s.OwnedAccount(ctx, userID, id)
	if err != nil {
		return err
	}
	if err = validateSharedCodexTicketAccount(account); err != nil {
		return err
	}
	if !enabled && SharedPoolCodexTicketRequired(account) {
		return infraerrors.BadRequest("SHARED_CODEX_TICKET_REQUIRED", "共享 Pro 账号必须开启打票，不能关闭")
	}
	return s.admin.UpdateAccountExtra(ctx, id, map[string]any{OpenAICodexTicketEnabledExtraKey: enabled})
}
