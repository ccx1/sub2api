package service

import (
	"context"
	"maps"
)

func (s *adminServiceImpl) prepareAccountCreationOptions(ctx context.Context, account *Account, input *CreateAccountInput) error {
	if input.ProtectionEnabled != nil {
		if *input.ProtectionEnabled {
			var settings SettingRepository
			if s.settingService != nil {
				settings = s.settingService.settingRepo
			}
			if err := NewAntiDegradeService(nil, settings).PrepareNewAccountProtection(ctx, account); err != nil {
				return err
			}
		} else {
			account.Extra = maps.Clone(account.Extra)
			if account.Extra == nil {
				account.Extra = make(map[string]any)
			}
			delete(account.Extra, AntiDegradeMarkerExtraKey)
			account.Extra[AntiDegradationExtraKey] = false
			account.Extra[ProtectionScopeExtraKey] = "disabled"
		}
	}
	if input.CodexTicketEnabled != nil {
		account.Extra = maps.Clone(account.Extra)
		if account.Extra == nil {
			account.Extra = make(map[string]any)
		}
		if isOpenAICodexTicketAccount(account) {
			account.Extra[OpenAICodexTicketEnabledExtraKey] = *input.CodexTicketEnabled
		} else {
			delete(account.Extra, OpenAICodexTicketEnabledExtraKey)
		}
	}
	return nil
}
