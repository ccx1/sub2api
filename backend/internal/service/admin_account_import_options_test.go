package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

type importOptionsAccountRepo struct {
	AccountRepository
	created *Account
	err     error
}

func (r *importOptionsAccountRepo) Create(_ context.Context, account *Account) error {
	r.created = account
	return r.err
}

func importOptionsInput() *CreateAccountInput {
	return &CreateAccountInput{
		Name: "imported", Platform: PlatformOpenAI, Type: AccountTypeSetupToken,
		Credentials: map[string]any{"access_token": "test-token"}, Concurrency: 8,
		SkipDefaultGroupBind: true, SkipImportDefaults: true,
	}
}

func TestAdminCreateAccountImportOptionsIndependent(t *testing.T) {
	for _, protection := range []bool{false, true} {
		for _, ticket := range []bool{false, true} {
			t.Run(fmt.Sprintf("protection=%v/ticket=%v", protection, ticket), func(t *testing.T) {
				repo := &importOptionsAccountRepo{}
				svc := &adminServiceImpl{accountRepo: repo, settingService: &SettingService{
					settingRepo: &protectionSettingsRepoStub{value: string(AntiDegradeModeMinimal)},
				}}
				input := importOptionsInput()
				input.ProtectionEnabled, input.CodexTicketEnabled = &protection, &ticket
				input.Extra = map[string]any{
					AntiDegradationExtraKey:          !protection,
					AntiDegradeMarkerExtraKey:        map[string]any{"enabled": true, "mode": "untrusted"},
					OpenAICodexTicketEnabledExtraKey: !ticket,
					"custom":                         true,
				}

				account, err := svc.CreateAccount(context.Background(), input)
				require.NoError(t, err)
				require.Same(t, account, repo.created)
				require.Equal(t, protection, account.AntiDegradationEnabled())
				require.Equal(t, ticket, account.Extra[OpenAICodexTicketEnabledExtraKey])
				require.Equal(t, true, account.Extra["custom"])
				require.NoError(t, ValidateAccountProtectionConfiguration(account))
				if protection {
					require.Equal(t, string(AntiDegradeModeMinimal), account.ProtectionMode())
				} else {
					require.NotContains(t, account.Extra, AntiDegradeMarkerExtraKey)
					require.Equal(t, "disabled", account.ProtectionScope())
				}
			})
		}
	}
}

func TestAdminCreateAccountImportOptionsOmittedPreservesExistingBehavior(t *testing.T) {
	for _, extra := range []map[string]any{nil, {
		AntiDegradationExtraKey:          true,
		AntiDegradeMarkerExtraKey:        map[string]any{"enabled": true, "mode": "existing"},
		OpenAICodexTicketEnabledExtraKey: false,
	}} {
		repo := &importOptionsAccountRepo{}
		svc := &adminServiceImpl{accountRepo: repo, settingService: &SettingService{
			settingRepo: &protectionSettingsRepoStub{readErr: errors.New("must not read settings")},
		}}
		input := importOptionsInput()
		input.Extra = extra
		account, err := svc.CreateAccount(context.Background(), input)
		require.NoError(t, err)
		for _, key := range []string{AntiDegradationExtraKey, AntiDegradeMarkerExtraKey, OpenAICodexTicketEnabledExtraKey} {
			require.Equal(t, extra[key], account.Extra[key])
		}
	}
}

func TestAdminCreateAccountImportProtectionFailureDoesNotPersist(t *testing.T) {
	readErr := errors.New("protection settings unavailable")
	repo := &importOptionsAccountRepo{}
	svc := &adminServiceImpl{accountRepo: repo, settingService: &SettingService{
		settingRepo: &protectionSettingsRepoStub{readErr: readErr},
	}}
	input := importOptionsInput()
	enabled := true
	input.ProtectionEnabled, input.CodexTicketEnabled = &enabled, &enabled
	account, err := svc.CreateAccount(context.Background(), input)
	require.ErrorIs(t, err, readErr)
	require.Nil(t, account)
	require.Nil(t, repo.created)
}

func TestAdminCreateAccountImportTicketsOnlyForSupportedAccounts(t *testing.T) {
	for _, tc := range []struct {
		platform, accountType string
		eligible              bool
	}{
		{PlatformOpenAI, AccountTypeOAuth, true},
		{PlatformOpenAI, AccountTypeSetupToken, true},
		{PlatformOpenAI, AccountTypeAPIKey, false},
		{PlatformAnthropic, AccountTypeOAuth, false},
		{PlatformGemini, AccountTypeAPIKey, false},
	} {
		t.Run(tc.platform+"/"+tc.accountType, func(t *testing.T) {
			stop := errors.New("creation boundary")
			repo := &importOptionsAccountRepo{err: stop}
			svc := &adminServiceImpl{accountRepo: repo}
			input := importOptionsInput()
			input.Platform, input.Type = tc.platform, tc.accountType
			enabled := true
			input.CodexTicketEnabled = &enabled
			input.Extra = map[string]any{OpenAICodexTicketEnabledExtraKey: true}
			_, err := svc.CreateAccount(context.Background(), input)
			require.ErrorIs(t, err, stop)
			require.NotNil(t, repo.created)
			if tc.eligible {
				require.Equal(t, true, repo.created.Extra[OpenAICodexTicketEnabledExtraKey])
			} else {
				require.NotContains(t, repo.created.Extra, OpenAICodexTicketEnabledExtraKey)
			}
		})
	}
}

func TestAdminCreateAccountImportProtectionPreservesTicketChoiceWhenOmitted(t *testing.T) {
	for _, protection := range []bool{false, true} {
		repo := &importOptionsAccountRepo{}
		svc := &adminServiceImpl{accountRepo: repo}
		input := importOptionsInput()
		input.ProtectionEnabled = &protection
		input.Extra = map[string]any{OpenAICodexTicketEnabledExtraKey: false}
		account, err := svc.CreateAccount(context.Background(), input)
		require.NoError(t, err)
		require.Equal(t, false, account.Extra[OpenAICodexTicketEnabledExtraKey])
	}
}
