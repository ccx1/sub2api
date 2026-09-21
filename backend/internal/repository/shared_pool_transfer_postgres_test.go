//go:build sharedpoolintegration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedPoolPostgresTransferHistoryConcurrentExactlyOnce(t *testing.T) {
	db, cmd := sharedPoolPostgresFixture(t)
	ctx := context.Background()
	_, err := NewUsageBillingRepository(nil, db).Apply(ctx, cmd)
	require.NoError(t, err)
	earnings := NewSharedPoolEarningsRepository(db)
	successes := 0
	for _, err := range sharedPoolRunConcurrent(func() error { _, err := earnings.Transfer(ctx, 6); return err }) {
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, service.ErrSharedPoolEarningsEmpty)
		}
	}
	require.Equal(t, 1, successes)
	requireSharedTransferCounts(t, db, 1, 1)
	requireSharedTransferBalance(t, db, "14.00000000")
	var transferID int64
	var transferredAt time.Time
	require.NoError(t, db.QueryRow(`SELECT id, created_at FROM shared_pool_earnings_transfers`).Scan(&transferID, &transferredAt))
	requireSharedTransferHistoryRow(t, db, transferID, "4.00000000", transferredAt)
	var state string
	var linkedID int64
	require.NoError(t, db.QueryRow(`SELECT status, transfer_id FROM shared_pool_earnings`).Scan(&state, &linkedID))
	require.Equal(t, "transferred", state)
	require.Equal(t, transferID, linkedID)
}

func TestSharedPoolPostgresTransferHistoryInsertFailureRollsBack(t *testing.T) {
	db, cmd := sharedPoolPostgresFixture(t)
	ctx := context.Background()
	_, err := NewUsageBillingRepository(nil, db).Apply(ctx, cmd)
	require.NoError(t, err)
	_, err = db.Exec(`ALTER TABLE redeem_codes ADD CONSTRAINT reject_shared_transfer_history CHECK (type <> 'shared_pool_transfer')`)
	require.NoError(t, err)
	earnings := NewSharedPoolEarningsRepository(db)
	_, err = earnings.Transfer(ctx, 6)
	require.ErrorContains(t, err, "reject_shared_transfer_history")
	requireSharedTransferCounts(t, db, 0, 0)
	requireSharedTransferBalance(t, db, "10.00000000")
	var state string
	var transferID sql.NullInt64
	var transferredAt sql.NullTime
	require.NoError(t, db.QueryRow(`SELECT status, transfer_id, transferred_at FROM shared_pool_earnings`).Scan(&state, &transferID, &transferredAt))
	require.Equal(t, "available", state)
	require.False(t, transferID.Valid)
	require.False(t, transferredAt.Valid)
	_, err = db.Exec(`ALTER TABLE redeem_codes DROP CONSTRAINT reject_shared_transfer_history`)
	require.NoError(t, err)
	result, err := earnings.Transfer(ctx, 6)
	require.NoError(t, err)
	require.Equal(t, 4.0, result.Amount)
	requireSharedTransferCounts(t, db, 1, 1)
	requireSharedTransferBalance(t, db, "14.00000000")
}

func TestSharedPoolPostgresTransferHistoryIsVisibleAndNotRecharge(t *testing.T) {
	db, cmd := sharedPoolPostgresFixture(t)
	ctx := context.Background()
	_, err := NewUsageBillingRepository(nil, db).Apply(ctx, cmd)
	require.NoError(t, err)
	transfer, err := NewSharedPoolEarningsRepository(db).Transfer(ctx, 6)
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	repo := NewRedeemCodeRepository(client)
	redeem := service.NewRedeemService(repo, nil, nil, nil, nil, client, nil, nil)
	items, page, err := redeem.GetUserHistoryPaginated(ctx, 6, pagination.PaginationParams{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	require.Len(t, items, 1)
	require.Equal(t, fmt.Sprintf("SHARED-%d", transfer.ID), items[0].Code)
	require.Equal(t, "shared_pool_transfer", items[0].Type)
	require.Equal(t, "共享收益转入余额", items[0].Notes)
	require.Equal(t, transfer.Amount, items[0].Value)
	legacy, err := redeem.GetUserHistory(ctx, 6, 25)
	require.NoError(t, err)
	require.Len(t, legacy, 1)
	require.Equal(t, items[0].ID, legacy[0].ID)
	other, page, err := redeem.GetUserHistoryPaginated(ctx, 3, pagination.PaginationParams{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Empty(t, other)
	require.Zero(t, page.Total)
	requireSharedTransferBalance(t, db, "14.00000000")
	// 保留一条真实充值记录，验证汇总排除共享收益而非意外返回空结果。
	_, err = db.Exec(`INSERT INTO redeem_codes(code, type, value, status, used_by, used_at) VALUES ('REAL-TOPUP', 'balance', 2, 'used', 6, NOW())`)
	require.NoError(t, err)
	recharged, err := repo.SumPositiveBalanceByUser(ctx, 6)
	require.NoError(t, err)
	require.Equal(t, 2.0, recharged)
}

func TestSharedPoolPostgresTransferHistoryMigrationIsIdempotent(t *testing.T) {
	db, _ := sharedPoolPostgresFixture(t)
	stamp := time.Date(2026, 9, 18, 3, 4, 5, 123456000, time.UTC)
	_, err := db.Exec(`INSERT INTO shared_pool_earnings_transfers(id, user_id, amount, balance_after, created_at)
		VALUES (71, 6, 4.12345678, 10, $1), (72, 6, 0, 10, $1)`, stamp)
	require.NoError(t, err)
	for range 2 {
		require.NoError(t, runSharedTransferHistoryMigration(t, db))
		requireSharedTransferCounts(t, db, 2, 1)
		requireSharedTransferHistoryRow(t, db, 71, "4.12345678", stamp)
		requireSharedTransferBalance(t, db, "10.00000000")
	}
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM redeem_codes WHERE code = 'SHARED-72'`).Scan(&count))
	require.Zero(t, count, "零金额历史转入不生成兑换记录")
}

func TestSharedPoolPostgresTransferHistoryMigrationRejectsConflictsAtomically(t *testing.T) {
	stamp := time.Date(2026, 9, 18, 3, 4, 5, 123456000, time.UTC)
	for _, tc := range []struct {
		name      string
		owner     int64
		amount    string
		kind      string
		status    string
		usedAt    any
		createdAt time.Time
	}{
		{"owner", 3, "4.12345678", "shared_pool_transfer", "used", stamp, stamp},
		{"amount", 6, "4.12345679", "shared_pool_transfer", "used", stamp, stamp},
		{"type", 6, "4.12345678", "balance", "used", stamp, stamp},
		{"status", 6, "4.12345678", "shared_pool_transfer", "unused", stamp, stamp},
		{"used time", 6, "4.12345678", "shared_pool_transfer", "used", stamp.Add(time.Second), stamp},
		{"null used time", 6, "4.12345678", "shared_pool_transfer", "used", nil, stamp},
		{"created time", 6, "4.12345678", "shared_pool_transfer", "used", stamp, stamp.Add(time.Second)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := sharedPoolPostgresFixture(t)
			_, err := db.Exec(`INSERT INTO shared_pool_earnings_transfers(id, user_id, amount, balance_after, created_at)
				VALUES (71, 6, 4.12345678, 10, $1), (72, 6, 4.12345678, 10, $1)`, stamp)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO redeem_codes(code, type, value, status, used_by, used_at, created_at, notes)
				VALUES ('SHARED-72', $1, $2::numeric, $3, $4, $5, $6, 'original history')`, tc.kind, tc.amount, tc.status, tc.owner, tc.usedAt, tc.createdAt)
			require.NoError(t, err)
			require.Error(t, runSharedTransferHistoryMigration(t, db))
			requireSharedTransferCounts(t, db, 2, 1)
			requireSharedTransferBalance(t, db, "10.00000000")
			var preserved bool
			require.NoError(t, db.QueryRow(`SELECT type=$1 AND value=$2::numeric AND status=$3 AND used_by=$4
				AND used_at IS NOT DISTINCT FROM $5::timestamptz AND created_at=$6 AND notes='original history'
				FROM redeem_codes WHERE code='SHARED-72'`, tc.kind, tc.amount, tc.status, tc.owner, tc.usedAt, tc.createdAt).Scan(&preserved))
			require.True(t, preserved, "迁移失败不能覆写既有兑换记录或补入其它转入")
		})
	}
}

func runSharedTransferHistoryMigration(t *testing.T, db *sql.DB) error {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "migrations", "254_shared_pool_transfer_history.sql"))
	require.NoError(t, err)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(string(body)); err != nil {
		return err
	}
	return tx.Commit()
}

func requireSharedTransferCounts(t *testing.T, db *sql.DB, transfers, histories int) {
	t.Helper()
	var transferCount, historyCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM shared_pool_earnings_transfers`).Scan(&transferCount))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM redeem_codes`).Scan(&historyCount))
	require.Equal(t, transfers, transferCount)
	require.Equal(t, histories, historyCount)
}

func requireSharedTransferBalance(t *testing.T, db *sql.DB, expected string) {
	t.Helper()
	var balance, recharged string
	require.NoError(t, db.QueryRow(`SELECT balance::text, total_recharged::text FROM users WHERE id=6`).Scan(&balance, &recharged))
	require.Equal(t, expected, balance)
	require.Equal(t, "0.00000000", recharged)
}

func requireSharedTransferHistoryRow(t *testing.T, db *sql.DB, transferID int64, amount string, stamp time.Time) {
	t.Helper()
	var kind, value, status, notes string
	var owner int64
	var usedAt, createdAt time.Time
	require.NoError(t, db.QueryRow(`SELECT type, value::text, status, used_by, used_at, created_at, notes
		FROM redeem_codes WHERE code=$1`, fmt.Sprintf("SHARED-%d", transferID)).Scan(&kind, &value, &status, &owner, &usedAt, &createdAt, &notes))
	require.Equal(t, "shared_pool_transfer", kind)
	require.Equal(t, amount, value)
	require.Equal(t, "used", status)
	require.EqualValues(t, 6, owner)
	require.True(t, stamp.Equal(usedAt))
	require.True(t, stamp.Equal(createdAt))
	require.Equal(t, "共享收益转入余额", notes)
}
