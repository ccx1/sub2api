package service

import "context"

// openAICodexTicketReuseKey carries the immutable ticket selected by an
// earlier transport attempt so a fallback request does not select another
// ticket for the same upstream turn.
type openAICodexTicketReuseKey struct{}

type openAICodexTicketReuse struct {
	accountID int64
	ticket    *openAICodexTicket
}

func withOpenAICodexTicketReuse(ctx context.Context, accountID int64, ticket *openAICodexTicket) context.Context {
	if ctx == nil || accountID <= 0 || ticket == nil {
		return ctx
	}
	return context.WithValue(ctx, openAICodexTicketReuseKey{}, &openAICodexTicketReuse{
		accountID: accountID,
		ticket:    codexTicketLeaf(ticket),
	})
}

func openAICodexTicketReuseFrom(ctx context.Context, account *Account, model string) *openAICodexTicket {
	if ctx == nil || account == nil {
		return nil
	}
	reuse, _ := ctx.Value(openAICodexTicketReuseKey{}).(*openAICodexTicketReuse)
	if reuse == nil || reuse.ticket == nil || reuse.accountID != account.ID || reuse.ticket.AccountID != account.ID {
		return nil
	}
	if normalizeOpenAICodexTicketModel(reuse.ticket.Model) != normalizeOpenAICodexTicketModel(model) {
		return nil
	}
	return codexTicketLeaf(reuse.ticket)
}
