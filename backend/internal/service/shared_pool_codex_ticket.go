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

func (s *SharedPoolService) SetCodexTicketEnabled(ctx context.Context, userID, id int64, _ bool) error {
	_, _, err := s.OwnedAccount(ctx, userID, id)
	if err != nil {
		return err
	}
	return infraerrors.Forbidden("SHARED_CODEX_TICKET_ADMIN_ONLY", "打票功能由管理员统一控制")
}
