//go:build sharedpoolintegration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func sharedReimportPGAccount(f sharedAccountPG, ownerID int64) *service.Account {
	a := f.account("synthetic-reimport", f.a)
	a.Credentials["refresh_token"] = "fixture-refresh-token"
	a.Extra[service.SharedPoolOwnerKey] = ownerID
	return a
}

func sharedReimportPGEarning(t *testing.T, f sharedAccountPG, accountID int64) {
	t.Helper()
	_, err := f.db.Exec(`INSERT INTO shared_pool_earnings
		(request_id,api_key_id,consumer_user_id,owner_user_id,account_id,group_id,
		 billing_amount,platform_rate_bps,proxy_rate_bps,uses_platform_proxy,platform_amount,owner_amount,status)
		VALUES('fixture-history',1,$1,$2,$3,$4,10,2000,0,FALSE,2,8,'available')`,
		f.other, f.owner, accountID, f.a)
	require.NoError(t, err)
}

func requireSharedReimportHistory(t *testing.T, f sharedAccountPG, accountID int64, enabled, adminDisabled bool) {
	t.Helper()
	var oldOwner, earningsOwner, earningsAccount int64
	var fingerprint, earningAmount string
	var oldEnabled, oldDisabled bool
	var deletedAt sql.NullTime
	require.NoError(t, f.db.QueryRow(`SELECT s.owner_user_id,s.credential_fingerprint,s.enabled,s.admin_disabled,a.deleted_at
		FROM shared_pool_accounts s JOIN accounts a ON a.id=s.account_id WHERE s.account_id=$1`, accountID).
		Scan(&oldOwner, &fingerprint, &oldEnabled, &oldDisabled, &deletedAt))
	require.Equal(t, f.owner, oldOwner)
	require.Equal(t, fmt.Sprintf("deleted:%d", accountID), fingerprint)
	require.Equal(t, enabled, oldEnabled)
	require.Equal(t, adminDisabled, oldDisabled)
	require.True(t, deletedAt.Valid)
	require.NoError(t, f.db.QueryRow(`SELECT owner_user_id,account_id,owner_amount FROM shared_pool_earnings WHERE request_id='fixture-history'`).
		Scan(&earningsOwner, &earningsAccount, &earningAmount))
	require.Equal(t, f.owner, earningsOwner)
	require.Equal(t, accountID, earningsAccount)
	require.Equal(t, "8.00000000", earningAmount)
}

func TestSharedPoolReimportPostgresDeletedAllowsNewOwnerAndPreservesHistory(t *testing.T) {
	for _, tc := range []struct {
		name                                   string
		sameOwner, adminDelete, ordinaryDelete bool
		adminDisabled                          bool
	}{
		{name: "same owner after self deletion", sameOwner: true},
		{name: "different owner after self deletion"},
		{name: "different owner after administrator deletion", adminDelete: true},
		{name: "administrator disabled then deleted", adminDisabled: true, adminDelete: true},
		{name: "ordinary account deletion retains shared enabled flag", ordinaryDelete: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := sharedAccountPostgresFixture(t)
			ctx := context.Background()
			old := sharedReimportPGAccount(f, f.owner)
			require.NoError(t, f.repo.CreateSharedAccount(ctx, old, f.owner, "fixture-reimport"))
			sharedReimportPGEarning(t, f, old.ID)
			if tc.adminDisabled {
				require.NoError(t, f.repo.SetSharedAccountState(ctx, old.ID, service.SharedPoolAccountState{AdminDisabled: new(true)}))
			}
			if tc.ordinaryDelete {
				require.NoError(t, f.repo.accounts.Delete(ctx, old.ID))
			} else {
				deleteOwner := f.owner
				if tc.adminDelete {
					deleteOwner = 0
				}
				require.NoError(t, f.repo.RemoveSharedAccount(ctx, deleteOwner, old.ID))
			}
			newOwner := f.other
			if tc.sameOwner {
				newOwner = f.owner
			}
			fresh := sharedReimportPGAccount(f, newOwner)
			require.NoError(t, f.repo.CreateSharedAccount(ctx, fresh, newOwner, "fixture-reimport"))
			require.NotEqual(t, old.ID, fresh.ID)
			record, err := f.repo.GetSharedAccount(ctx, newOwner, fresh.ID)
			require.NoError(t, err)
			require.Equal(t, newOwner, record.OwnerUserID)
			require.False(t, record.AdminDisabled)
			requireSharedReimportHistory(t, f, old.ID, tc.ordinaryDelete, tc.adminDisabled)
		})
	}
}

func TestSharedPoolReimportPostgresUndeletedStatesRemainDuplicate(t *testing.T) {
	for _, state := range []string{"active", "owner disabled", "administrator disabled"} {
		t.Run(state, func(t *testing.T) {
			f := sharedAccountPostgresFixture(t)
			ctx := context.Background()
			old := sharedReimportPGAccount(f, f.owner)
			require.NoError(t, f.repo.CreateSharedAccount(ctx, old, f.owner, "fixture-reimport"))
			if state == "owner disabled" {
				require.NoError(t, f.repo.SetSharedAccountState(ctx, old.ID, service.SharedPoolAccountState{OwnerID: f.owner, Enabled: new(false)}))
			}
			if state == "administrator disabled" {
				require.NoError(t, f.repo.SetSharedAccountState(ctx, old.ID, service.SharedPoolAccountState{AdminDisabled: new(true)}))
			}
			fresh := sharedReimportPGAccount(f, f.other)
			err := f.repo.CreateSharedAccount(ctx, fresh, f.other, "fixture-reimport")
			require.Equal(t, "SHARED_ACCOUNT_EXISTS", infraerrors.Reason(err))
			var count int
			require.NoError(t, f.db.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&count))
			require.Equal(t, 1, count)
		})
	}
}

func TestSharedPoolReimportPostgresRepeatedDeleteKeepsDistinctHistory(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	current := sharedReimportPGAccount(f, f.owner)
	require.NoError(t, f.repo.CreateSharedAccount(ctx, current, f.owner, "fixture-repeated"))
	for range 2 {
		oldID := current.ID
		require.NoError(t, f.repo.RemoveSharedAccount(ctx, 0, oldID))
		current = sharedReimportPGAccount(f, f.other)
		require.NoError(t, f.repo.CreateSharedAccount(ctx, current, f.other, "fixture-repeated"))
		require.NotEqual(t, oldID, current.ID)
		var retired string
		require.NoError(t, f.db.QueryRow(`SELECT credential_fingerprint FROM shared_pool_accounts WHERE account_id=$1`, oldID).Scan(&retired))
		require.Equal(t, fmt.Sprintf("deleted:%d", oldID), retired)
	}
	var retiredCount, activeCount int
	require.NoError(t, f.db.QueryRow(`SELECT COUNT(*) FROM shared_pool_accounts WHERE credential_fingerprint LIKE 'deleted:%'`).Scan(&retiredCount))
	require.NoError(t, f.db.QueryRow(`SELECT COUNT(*) FROM shared_pool_accounts WHERE credential_fingerprint='fixture-repeated'`).Scan(&activeCount))
	require.Equal(t, 2, retiredCount)
	require.Equal(t, 1, activeCount)
}

func TestSharedPoolReimportPostgresOutboxFailureRestoresFingerprint(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	old := sharedReimportPGAccount(f, f.owner)
	require.NoError(t, f.repo.CreateSharedAccount(ctx, old, f.owner, "fixture-rollback"))
	require.NoError(t, f.repo.RemoveSharedAccount(ctx, f.owner, old.ID))
	_, err := f.db.Exec(`ALTER TABLE scheduler_outbox ADD CONSTRAINT reject_reimport_outbox CHECK(false) NOT VALID`)
	require.NoError(t, err)
	fresh := sharedReimportPGAccount(f, f.other)
	require.Error(t, f.repo.CreateSharedAccount(ctx, fresh, f.other, "fixture-rollback"))
	var fingerprint string
	var accountCount, sharedCount int
	require.NoError(t, f.db.QueryRow(`SELECT credential_fingerprint FROM shared_pool_accounts WHERE account_id=$1`, old.ID).Scan(&fingerprint))
	require.Equal(t, "fixture-rollback", fingerprint)
	require.NoError(t, f.db.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&accountCount))
	require.NoError(t, f.db.QueryRow(`SELECT COUNT(*) FROM shared_pool_accounts`).Scan(&sharedCount))
	require.Equal(t, 1, accountCount)
	require.Equal(t, 1, sharedCount)
}

func TestSharedPoolReimportPostgresConcurrentImportsHaveOneWinner(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	old := sharedReimportPGAccount(f, f.owner)
	require.NoError(t, f.repo.CreateSharedAccount(ctx, old, f.owner, "fixture-concurrent"))
	require.NoError(t, f.repo.RemoveSharedAccount(ctx, f.owner, old.ID))
	start, results := make(chan struct{}), make(chan error, 8)
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			<-start
			a := sharedReimportPGAccount(f, f.other)
			results <- f.repo.CreateSharedAccount(ctx, a, f.other, "fixture-concurrent")
		})
	}
	close(start)
	workers.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else {
			require.Equal(t, "SHARED_ACCOUNT_EXISTS", infraerrors.Reason(err))
		}
	}
	require.Equal(t, 1, winners)
	var count, activeCount int
	require.NoError(t, f.db.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&count))
	require.NoError(t, f.db.QueryRow(`SELECT COUNT(*) FROM shared_pool_accounts s JOIN accounts a ON a.id=s.account_id
		WHERE s.credential_fingerprint='fixture-concurrent' AND a.deleted_at IS NULL`).Scan(&activeCount))
	require.Equal(t, 2, count)
	require.Equal(t, 1, activeCount)
}
