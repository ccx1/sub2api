package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func TestProtectionSettingsSyncSerializesDefaultEnable(t *testing.T) {
	svc, _, accounts := protectionSyncFixture(t)
	accounts.accounts[2].Extra[AntiDegradationExtraKey] = false
	delete(accounts.accounts[2].Extra, AntiDegradeMarkerExtraKey)
	started, release := make(chan struct{}), make(chan struct{})
	accounts.beforeGet = func(id int64) {
		if id == 1 {
			close(started)
			<-release
		}
	}
	saved := make(chan error, 1)
	go func() { _, err := svc.UpdateProtectionSettings(context.Background(), AntiDegradeMode2); saved <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("global sync did not start")
	}
	enabled := make(chan error, 1)
	enableAttempted := make(chan struct{})
	go func() {
		close(enableAttempted)
		_, err := svc.SetProtection(context.Background(), 2, true, false)
		enabled <- err
	}()
	<-enableAttempted
	select {
	case <-enabled:
		close(release)
		t.Fatal("default enable bypassed the in-progress global sync")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-saved:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("global sync did not finish")
	}
	select {
	case err := <-enabled:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("default enable did not finish")
	}
	require.Equal(t, "mode2", accounts.accounts[2].ProtectionMode())
	require.Equal(t, codexFingerprintFull, accounts.accounts[2].GetCodexFingerprintMode())
}

type protectionSyncAdminRepo struct {
	*upstreamBillingProbeAccountRepo
	managedWrites int
}

func (r *protectionSyncAdminRepo) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, string, int64, string) ([]Account, *pagination.PaginationResult, error) {
	return []Account{*r.accounts[1]}, &pagination.PaginationResult{Pages: 1}, nil
}

func (r *protectionSyncAdminRepo) Update(ctx context.Context, a *Account) error {
	if _, ok := GetProtectionWriteExpectation(ctx); ok && ProtectionManagedWrite(ctx) {
		r.managedWrites++
	}
	return r.upstreamBillingProbeAccountRepo.Update(ctx, a)
}

func TestProtectionSettingsSyncUsesAdminUpdateRepositoryPath(t *testing.T) {
	repo := &protectionSyncAdminRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
		1: {ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 4, Extra: map[string]any{"custom": "preserved"}},
	}}}
	settings := &protectionSettingsRepoStub{value: "legacy"}
	svc := NewAntiDegradeServiceWithSettings(&adminServiceImpl{accountRepo: repo}, settings, repo)
	_, err := svc.SetProtection(context.Background(), 1, true, false)
	require.NoError(t, err)
	result, err := svc.UpdateProtectionSettings(context.Background(), AntiDegradeMode2)
	require.NoError(t, err)
	require.Equal(t, 1, result.Sync.Updated)
	require.Equal(t, 2, repo.managedWrites)
	a, err := repo.GetByID(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, codexFingerprintFull, a.GetCodexFingerprintMode())
	require.Equal(t, "preserved", a.Extra["custom"])
}

func TestProtectionSettingsSyncIncludesConcurrentlyCreatedAccount(t *testing.T) {
	svc, repo, accounts := protectionSyncFixture(t)
	prepared, commit := make(chan struct{}), make(chan struct{})
	created := make(chan error, 1)
	a := newProtectionSettingsAccountStore().account
	a.ID = 6
	go func() {
		created <- svc.CreateProtectedAccount(context.Background(), a, func() error {
			close(prepared)
			<-commit
			accounts.accounts[6] = a
			accounts.pages[1] = append(accounts.pages[1], 6)
			return nil
		})
	}()
	select {
	case <-prepared:
	case <-time.After(5 * time.Second):
		t.Fatal("creation did not prepare protection")
	}
	saved := make(chan error, 1)
	saveAttempted := make(chan struct{})
	go func() {
		close(saveAttempted)
		_, err := svc.UpdateProtectionSettings(context.Background(), AntiDegradeMode2)
		saved <- err
	}()
	<-saveAttempted
	select {
	case <-saved:
		close(commit)
		t.Fatal("global sync bypassed the in-progress protected account creation")
	case <-time.After(100 * time.Millisecond):
	}
	close(commit)
	select {
	case err := <-created:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("creation did not commit")
	}
	select {
	case err := <-saved:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("global sync did not finish")
	}
	require.Equal(t, "mode2", repo.value)
	require.Equal(t, "mode2", accounts.accounts[6].ProtectionMode())
	require.Equal(t, codexFingerprintFull, accounts.accounts[6].GetCodexFingerprintMode())
}
