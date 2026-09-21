package service

import "context"

func (s *OpenAIGatewayService) keepUsableCodexTicketForProxy(ctx context.Context, account *Account) bool {
	if account.IsRandomProxy() || account.CodexTicketProxyMode() == CodexTicketProxyModeRandom || account.CodexTicketProxyMode() == CodexTicketProxyModeAccount {
		return true
	}
	if account.CodexTicketProxyMode() != CodexTicketProxyModeInherit {
		return false
	}
	policy, err := s.codexTicketProxyPolicy(ctx, account)
	return err == nil && policy.mode == "pool"
}

func (s *OpenAIGatewayService) codexTicketProxyPolicyCurrent(ctx context.Context, input openAICodexTicketProbeInput) bool {
	if input.HarvestProxyPolicy == nil {
		return true
	}
	policy, err := s.codexTicketProxyPolicy(ctx, input.Account)
	return err == nil && policy == *input.HarvestProxyPolicy
}

func (s *OpenAIGatewayService) codexTicketAccountCurrentBeforePublish(ctx context.Context, input openAICodexTicketProbeInput) bool {
	account := s.reloadOpenAICodexTicketProbeAccount(ctx, input.Account)
	if account == nil || input.Config == nil ||
		s.codexTicketBindingForConfig(account, *input.Config) != s.codexTicketBindingForConfig(input.Account, *input.Config) ||
		account.GetCredential("access_token") != input.Account.GetCredential("access_token") ||
		account.GetCredential("refresh_token") != input.Account.GetCredential("refresh_token") {
		return false
	}
	input.Account = account
	return s.codexTicketProxyPolicyCurrent(ctx, input)
}
