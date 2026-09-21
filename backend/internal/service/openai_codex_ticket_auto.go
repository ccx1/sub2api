package service

import (
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// 只验证传输形状和边界，不从加密票长度推断套餐或模型资格。
func codexTicketAutoStateShape(state string) bool {
	if len(state) < 16 || len(state) > 8192 || !strings.HasPrefix(state, openAICodexTicketStatePrefix) {
		return false
	}
	padding := 0
	for _, b := range []byte(state) {
		if b == '=' {
			padding++
			if padding > 2 {
				return false
			}
			continue
		}
		if padding != 0 || !(b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '-' || b == '_') {
			return false
		}
	}
	return true
}

func codexTicketCandidateAccepted(state string, account *Account, cfg config.OpenAICodexTicketConfig) bool {
	if cfg.LengthMode == config.CodexTicketLengthAuto {
		return codexTicketAutoStateShape(state)
	}
	return len(state) == openAICodexTicketTargetLength(account, cfg) &&
		strings.HasPrefix(state, openAICodexTicketStatePrefix) && !codexTicketStateRejected(state, cfg)
}

func (ticket *openAICodexTicket) autoUsable(now time.Time, account *Account) bool {
	return ticket != nil && ticket.Verified && ticket.AccountBinding != "" &&
		ticket.valid(now, ticket.Length) && codexTicketAutoStateShape(ticket.State) && ticket.accountCompatible(account)
}
