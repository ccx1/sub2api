package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProtectionNewAccountUsesConfiguredDefaultAndReplacesClientState(t *testing.T) {
	for _, mode := range []AntiDegradeMode{AntiDegradeMode2, AntiDegradeModeLegacy, AntiDegradeMode1} {
		t.Run(string(mode), func(t *testing.T) {
			a := newProtectionSettingsAccountStore().account
			a.Extra = map[string]any{AntiDegradeMarkerExtraKey: map[string]any{"enabled": true, "mode": "unknown"}, codexFingerprintSeedExtraKey: "client-supplied", "custom": true}
			svc := NewAntiDegradeService(nil, &protectionSettingsRepoStub{value: string(mode)})
			require.NoError(t, svc.PrepareNewAccountProtection(context.Background(), a))
			require.Equal(t, string(mode), a.ProtectionMode())
			require.NotEqual(t, "client-supplied", a.Extra[codexFingerprintSeedExtraKey])
			require.Equal(t, true, a.Extra["custom"])
			require.NoError(t, ValidateAccountProtectionConfiguration(a))
		})
	}
}

func TestProtectionNewAccountSettingsFailureLeavesCallerUnchanged(t *testing.T) {
	a := newProtectionSettingsAccountStore().account
	a.Extra["custom"] = true
	repo := &protectionSettingsRepoStub{readErr: errors.New("settings unavailable")}
	err := NewAntiDegradeService(nil, repo).PrepareNewAccountProtection(context.Background(), a)
	require.ErrorIs(t, err, repo.readErr)
	require.Equal(t, map[string]any{"custom": true}, a.Extra)
}

type protectionSharedCreateRepo struct {
	SharedPoolRepository
	created *Account
	stop    error
}

func (*protectionSharedCreateRepo) SharedSettings(context.Context) (*SharedPoolSettings, error) {
	return &SharedPoolSettings{MaxConcurrency: 10}, nil
}

func (r *protectionSharedCreateRepo) CreateSharedAccount(_ context.Context, a *Account, _ int64, _ string) error {
	r.created = a
	return r.stop
}

func TestProtectionSharedPoolCreationUsesDynamicDefault(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "configured", true: "read failure"}[failed], func(t *testing.T) {
			repo := &protectionSharedCreateRepo{stop: errors.New("creation boundary")}
			settings := &protectionSettingsRepoStub{value: string(AntiDegradeModeMinimal)}
			if failed {
				settings.readErr = errors.New("settings unavailable")
			}
			svc := &SharedPoolService{repo: repo, protection: NewAntiDegradeService(nil, settings)}
			_, err := svc.Create(context.Background(), 7, SharedPoolAccountInput{Name: "shared", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Concurrency: 3, ProtectionEnabled: true, Credentials: map[string]any{"access_token": "test-token"}})
			if failed {
				require.ErrorIs(t, err, settings.readErr)
				require.Nil(t, repo.created)
				return
			}
			require.ErrorIs(t, err, repo.stop)
			require.Equal(t, "minimal_compat", repo.created.ProtectionMode())
			require.Equal(t, int64(7), repo.created.Extra[SharedPoolOwnerKey])
		})
	}
}
