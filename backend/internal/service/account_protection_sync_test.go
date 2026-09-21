package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type protectionSyncAccounts struct {
	AccountRepository
	accounts    map[int64]*Account
	pages       [][]int64
	listErr     error
	listErrPage int
	writeErr    map[int64]error
	writes      []int64
	beforeGet   func(int64)
	afterWrite  func(int64)
	seenPages   []pagination.PaginationParams
	seenFilters []string
}

func (r *protectionSyncAccounts) ListWithFilters(ctx context.Context, p pagination.PaginationParams, platform, accountType, status, search string, groupID int64, privacyMode string) ([]Account, *pagination.PaginationResult, error) {
	r.seenPages = append(r.seenPages, p)
	r.seenFilters = append(r.seenFilters, SharedAccountFilter(ctx))
	if platform != PlatformOpenAI || accountType != "" || status != "" {
		return nil, nil, errors.New("unexpected protection sync filters")
	}
	if r.listErr != nil && (r.listErrPage == 0 || p.Page == r.listErrPage) {
		return nil, nil, r.listErr
	}
	result := []Account{}
	if p.Page <= len(r.pages) {
		for _, id := range r.pages[p.Page-1] {
			result = append(result, *r.accounts[id])
		}
	}
	return result, &pagination.PaginationResult{Pages: len(r.pages)}, nil
}

func (r *protectionSyncAccounts) GetAccount(_ context.Context, id int64) (*Account, error) {
	if r.beforeGet != nil {
		r.beforeGet(id)
	}
	return r.accounts[id], nil
}

func (r *protectionSyncAccounts) UpdateAccount(ctx context.Context, id int64, in *UpdateAccountInput) (*Account, error) {
	if _, ok := GetProtectionWriteExpectation(ctx); !ok || !ProtectionManagedWrite(ctx) {
		return nil, errors.New("missing atomic managed transition")
	}
	if err := r.writeErr[id]; err != nil {
		return nil, err
	}
	store := &protectionDraftStore{account: r.accounts[id]}
	a, err := store.UpdateAccount(ctx, id, in)
	if err != nil {
		return nil, err
	}
	r.accounts[id] = a
	r.writes = append(r.writes, id)
	if r.afterWrite != nil {
		r.afterWrite(id)
	}
	return a, nil
}

func protectionSyncFixture(t *testing.T) (*AntiDegradeService, *protectionSettingsRepoStub, *protectionSyncAccounts) {
	t.Helper()
	accounts := &protectionSyncAccounts{accounts: map[int64]*Account{}, writeErr: map[int64]error{}}
	for id := int64(1); id <= 5; id++ {
		store := newProtectionSettingsAccountStore()
		store.account.ID = id
		a, err := NewAntiDegradeService(store).ApplyAntiDegradeMode(context.Background(), id, AntiDegradeModeLegacy)
		require.NoError(t, err)
		accounts.accounts[id] = a
	}
	accounts.pages = [][]int64{{1, 2, 3}, {4, 5}}
	repo := &protectionSettingsRepoStub{value: "legacy"}
	return NewAntiDegradeServiceWithSettings(accounts, repo, accounts), repo, accounts
}

func TestProtectionSettingsSyncPaginatesSharedAndSkipsIneligible(t *testing.T) {
	svc, repo, accounts := protectionSyncFixture(t)
	accounts.accounts[2].Extra[SharedPoolOwnerKey] = int64(9)
	accounts.accounts[2].Status = StatusDisabled
	accounts.accounts[2].Type = AccountTypeSetupToken
	accounts.accounts[3].Extra[AntiDegradationExtraKey] = false
	parent := int64(1)
	accounts.accounts[4].ParentAccountID = &parent
	accounts.accounts[5].Type = AccountTypeAPIKey
	result, err := svc.UpdateProtectionSettings(WithSharedAccountFilter(context.Background(), "platform"), AntiDegradeMode2)
	require.NoError(t, err)
	require.Equal(t, "mode2", repo.value)
	require.Equal(t, []int64{1, 2}, accounts.writes)
	require.Equal(t, 2, result.Sync.Updated)
	require.Empty(t, result.Sync.Failed)
	require.Len(t, accounts.seenPages, 2)
	for i, page := range accounts.seenPages {
		require.Equal(t, i+1, page.Page)
		require.Equal(t, "id", page.SortBy)
		require.Equal(t, pagination.SortOrderAsc, page.SortOrder)
		require.Empty(t, accounts.seenFilters[i])
	}
	require.Equal(t, codexFingerprintFull, accounts.accounts[2].GetCodexFingerprintMode())
	require.False(t, accounts.accounts[3].AntiDegradationEnabled())
	result, err = svc.UpdateProtectionSettings(context.Background(), AntiDegradeMode2)
	require.NoError(t, err)
	require.Zero(t, result.Sync.Updated)
	require.Equal(t, 3, result.Sync.Unchanged)
	require.Equal(t, 2, repo.writes, "相同全局设置仍允许再次同步")
}

func TestProtectionSettingsSyncReportsPerAccountFailureAndRetries(t *testing.T) {
	svc, _, accounts := protectionSyncFixture(t)
	accounts.writeErr[2] = ErrProtectionConflict
	result, err := svc.UpdateProtectionSettings(context.Background(), AntiDegradeMode2)
	require.NoError(t, err)
	require.Equal(t, 4, result.Sync.Updated)
	require.Len(t, result.Sync.Failed, 1)
	require.Equal(t, int64(2), result.Sync.Failed[0].AccountID)
	require.Equal(t, []int64{1, 3, 4, 5}, accounts.writes)
	require.Equal(t, "legacy", accounts.accounts[2].ProtectionMode())
	delete(accounts.writeErr, 2)
	result, err = svc.UpdateProtectionSettings(context.Background(), AntiDegradeMode2)
	require.NoError(t, err)
	require.Equal(t, 1, result.Sync.Updated)
	require.Equal(t, 4, result.Sync.Unchanged)
	require.Empty(t, result.Sync.Failed)
}

func TestProtectionSettingsSyncDoesNotReenableConcurrentlyDisabledAccount(t *testing.T) {
	svc, _, accounts := protectionSyncFixture(t)
	accounts.beforeGet = func(id int64) {
		if id == 2 {
			accounts.accounts[id].Extra[AntiDegradationExtraKey] = false
		}
	}
	result, err := svc.UpdateProtectionSettings(context.Background(), AntiDegradeMode2)
	require.NoError(t, err)
	require.Equal(t, 4, result.Sync.Updated)
	require.Equal(t, 1, result.Sync.Unchanged)
	require.NotContains(t, accounts.writes, int64(2))
	require.False(t, accounts.accounts[2].AntiDegradationEnabled())
}

func TestProtectionSettingsSyncListFailureReportsSavedDefault(t *testing.T) {
	svc, repo, accounts := protectionSyncFixture(t)
	accounts.listErr, accounts.listErrPage = errors.New("second page unavailable"), 2
	result, err := svc.UpdateProtectionSettings(context.Background(), AntiDegradeMode2)
	require.NoError(t, err)
	require.Contains(t, result.Sync.Error, accounts.listErr.Error())
	require.Equal(t, "mode2", repo.value)
	require.Equal(t, 1, repo.writes)
	require.Empty(t, accounts.writes)
}

func TestProtectionSettingsSyncStopsWhenSupersededOrCanceled(t *testing.T) {
	for _, scenario := range []string{"superseded", "canceled", "settings unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			svc, repo, accounts := protectionSyncFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			accounts.afterWrite = func(int64) {
				switch scenario {
				case "superseded":
					repo.value = "minimal_compat"
				case "canceled":
					cancel()
				default:
					repo.readErr = errors.New("settings unavailable")
				}
			}
			result, err := svc.UpdateProtectionSettings(ctx, AntiDegradeMode2)
			require.NoError(t, err)
			require.Equal(t, 1, result.Sync.Updated)
			require.NotEmpty(t, result.Sync.Error)
			require.Equal(t, "legacy", accounts.accounts[2].ProtectionMode())
		})
	}
}

func TestProtectionSettingsSyncUpgradesExistingMode1Version(t *testing.T) {
	svc, _, accounts := protectionSyncFixture(t)
	_, err := svc.ApplyAntiDegradeMode(context.Background(), 1, AntiDegradeMode1)
	require.NoError(t, err)
	a := accounts.accounts[1]
	mode1Marker(a)["policy_version"] = 2
	a.Extra["enable_tls_fingerprint"] = true
	a.Extra["tls_fingerprint_builtin"] = "nodejs24"
	seed, snapshot := a.Extra[codexFingerprintSeedExtraKey], antiDegradePrev(a)
	accounts.pages = [][]int64{{1}}
	result, err := svc.UpdateProtectionSettings(context.Background(), AntiDegradeMode1)
	require.NoError(t, err)
	require.Equal(t, 1, result.Sync.Updated)
	a = accounts.accounts[1]
	require.Equal(t, mode1PolicyVersion, mode1Int(mode1Marker(a)["policy_version"]))
	require.False(t, a.IsTLSFingerprintEnabled())
	require.Equal(t, seed, a.Extra[codexFingerprintSeedExtraKey])
	require.Equal(t, snapshot, antiDegradePrev(a))
}
