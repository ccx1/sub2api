package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type protectionSettingsRepoStub struct {
	SettingRepository
	value             string
	readErr, writeErr error
	writes            int
}

type protectionSettingsEmptyAccounts struct{ AccountRepository }

func (*protectionSettingsEmptyAccounts) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, string, int64, string) ([]Account, *pagination.PaginationResult, error) {
	return nil, &pagination.PaginationResult{}, nil
}

func (r *protectionSettingsRepoStub) GetValue(context.Context, string) (string, error) {
	if r.readErr != nil {
		return "", r.readErr
	}
	if r.value == "" {
		return "", ErrSettingNotFound
	}
	return r.value, nil
}

func (r *protectionSettingsRepoStub) Set(_ context.Context, key, value string) error {
	if key != SettingKeyAccountProtectionDefaultMode {
		return errors.New("unexpected setting key")
	}
	if r.writeErr != nil {
		return r.writeErr
	}
	r.value = value
	r.writes++
	return nil
}

func TestProtectionSettingsDefaultAndPersistence(t *testing.T) {
	repo := &protectionSettingsRepoStub{}
	svc := NewAntiDegradeServiceWithSettings(nil, repo, &protectionSettingsEmptyAccounts{})
	settings, err := svc.GetProtectionSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, AntiDegradeMode2, settings.DefaultMode)
	for _, profile := range ListAntiDegradeStrategyProfiles() {
		if !profile.ApplySupported {
			continue
		}
		settings, err = svc.UpdateProtectionSettings(context.Background(), profile.ID)
		require.NoError(t, err)
		require.Equal(t, profile.ID, settings.DefaultMode)
		reread, err := NewAntiDegradeService(nil, repo).GetProtectionSettings(context.Background())
		require.NoError(t, err)
		require.Equal(t, settings.DefaultMode, reread.DefaultMode)
		require.NotNil(t, settings.Sync)
	}
}

func TestProtectionSettingsRejectInvalidAndPropagateStorageErrors(t *testing.T) {
	repo := &protectionSettingsRepoStub{value: string(AntiDegradeModeLegacy)}
	svc := NewAntiDegradeServiceWithSettings(nil, repo, &protectionSettingsEmptyAccounts{})
	for _, mode := range []AntiDegradeMode{"", "unknown", AntiDegradeModeNative} {
		_, err := svc.UpdateProtectionSettings(context.Background(), mode)
		require.Error(t, err)
		require.Zero(t, repo.writes)
	}
	repo.readErr = errors.New("settings database unavailable")
	_, err := svc.GetProtectionSettings(context.Background())
	require.ErrorIs(t, err, repo.readErr)
	repo.writeErr = errors.New("settings write failed")
	_, err = svc.UpdateProtectionSettings(context.Background(), AntiDegradeMode2)
	require.ErrorIs(t, err, repo.writeErr)
	require.Equal(t, string(AntiDegradeModeLegacy), repo.value)
}

func newProtectionSettingsAccountStore() *protectionSettingsAccountStore {
	return &protectionSettingsAccountStore{account: &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 4, Extra: map[string]any{}}}
}

type protectionSettingsAccountStore struct {
	account  *Account
	writes   int
	writeErr error
}

func (s *protectionSettingsAccountStore) GetAccount(context.Context, int64) (*Account, error) {
	return s.account, nil
}
func (s *protectionSettingsAccountStore) UpdateAccount(ctx context.Context, id int64, in *UpdateAccountInput) (*Account, error) {
	if _, ok := GetProtectionWriteExpectation(ctx); !ok {
		return nil, errors.New("missing atomic transition expectation")
	}
	if s.writeErr != nil {
		return nil, s.writeErr
	}
	draft := &protectionDraftStore{account: s.account}
	a, err := draft.UpdateAccount(ctx, id, in)
	if err != nil {
		return nil, err
	}
	s.account, s.writes = a, s.writes+1
	return a, nil
}

func TestProtectionEnableUsesConfiguredDefaultAndPreservesExistingLegacy(t *testing.T) {
	for _, mode := range []AntiDegradeMode{"", AntiDegradeModeMinimal, AntiDegradeModeLegacy, AntiDegradeMode1} {
		t.Run(string(mode), func(t *testing.T) {
			store := newProtectionSettingsAccountStore()
			repo := &protectionSettingsRepoStub{value: string(mode)}
			svc := NewAntiDegradeService(store, repo)
			a, err := svc.SetProtection(context.Background(), 1, true, false)
			require.NoError(t, err)
			expected := mode
			if expected == "" {
				expected = AntiDegradeMode2
			}
			require.Equal(t, string(expected), a.ProtectionMode())
			fingerprint, _, _ := antiDegradeModeSettings(expected)
			require.Equal(t, fingerprint, a.GetCodexFingerprintMode())
			require.Equal(t, 1, store.writes)
			repo.value = string(AntiDegradeMode2)
			a, err = svc.SetProtection(context.Background(), 1, true, false)
			require.NoError(t, err)
			require.Equal(t, string(expected), a.ProtectionMode())
			require.Equal(t, 1, store.writes)
		})
	}
}

func TestProtectionEmptyModeUsesDefaultForPreviewAndAtomicApply(t *testing.T) {
	store := newProtectionSettingsAccountStore()
	repo := &protectionSettingsRepoStub{value: string(AntiDegradeModeLegacy)}
	svc := NewAntiDegradeService(store, repo)
	_, err := svc.Apply(context.Background(), 1)
	require.NoError(t, err)
	repo.value = string(AntiDegradeMode2)
	preview, err := svc.Preview(context.Background(), 1)
	require.NoError(t, err)
	require.True(t, preview.Eligible)
	require.Equal(t, "mode2", preview.Changes[0].To)
	store.writeErr = errors.New("transition conflict")
	_, err = svc.ApplyAntiDegradeMode(context.Background(), 1, "")
	require.ErrorIs(t, err, store.writeErr)
	require.Equal(t, "legacy", store.account.ProtectionMode())
	store.writeErr = nil
	a, err := svc.ApplyAntiDegradeMode(context.Background(), 1, "")
	require.NoError(t, err)
	require.Equal(t, "mode2", a.ProtectionMode())
	require.Equal(t, codexFingerprintFull, a.GetCodexFingerprintMode())
	require.Equal(t, 2, store.writes)
}

func TestProtectionDefaultReadFailureDoesNotWriteAccount(t *testing.T) {
	store := newProtectionSettingsAccountStore()
	repo := &protectionSettingsRepoStub{readErr: errors.New("settings unavailable")}
	svc := NewAntiDegradeService(store, repo)
	_, err := svc.SetProtection(context.Background(), 1, true, false)
	require.ErrorIs(t, err, repo.readErr)
	_, err = svc.PreviewMode(context.Background(), 1, "")
	require.ErrorIs(t, err, repo.readErr)
	require.Zero(t, store.writes)
	_, err = svc.ApplyAntiDegradeMode(context.Background(), 1, AntiDegradeModeLegacy)
	require.NoError(t, err)
}

func TestProtectionDefaultPreservesOtherPlatformsAndShadow(t *testing.T) {
	for _, platform := range []string{PlatformAnthropic, PlatformGemini, PlatformOpenAI} {
		t.Run(platform, func(t *testing.T) {
			store := newProtectionSettingsAccountStore()
			store.account.Platform = platform
			if platform == PlatformOpenAI {
				parent := int64(9)
				store.account.ParentAccountID = &parent
			}
			svc := NewAntiDegradeService(store, &protectionSettingsRepoStub{readErr: errors.New("must not read OpenAI settings")})
			a, err := svc.SetProtection(context.Background(), 1, true, false)
			require.NoError(t, err)
			if platform == PlatformAnthropic {
				require.Equal(t, "legacy", a.ProtectionMode())
				require.Equal(t, "nodejs24", a.Extra["tls_fingerprint_builtin"])
			} else {
				require.Equal(t, "generic_v1", a.ProtectionScope())
			}
		})
	}
}
