//go:build sharedpoolintegration

package repository

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedPoolPostgresAutoTransferConcurrentExactlyOnce(t *testing.T) {
	db, cmd := sharedPoolPostgresFixture(t)
	applySharedPoolAutoTransferMigration(t, db)
	ctx := context.Background()
	_, err := NewUsageBillingRepository(nil, db).Apply(ctx, cmd)
	require.NoError(t, err)
	_, err = NewSharedPoolAutoTransferRepository(db).Save(ctx, 6, service.SharedPoolAutoTransferUpdate{
		Enabled: true, Threshold: 1, DailyTime: "00:00",
	})
	require.NoError(t, err)
	now := time.Now()
	repo := NewSharedPoolAutoTransferRepository(db)
	var mu sync.Mutex
	successes := 0
	errs := sharedPoolRunConcurrent(func() error {
		result, err := repo.RunDue(ctx, 6, now)
		if err == nil && result != nil {
			mu.Lock()
			successes++
			mu.Unlock()
		}
		return err
	})
	for _, err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 1, successes)
	var balance, amount string
	require.NoError(t, db.QueryRow(`SELECT balance::text FROM users WHERE id = 6`).Scan(&balance))
	require.Equal(t, "14.00000000", balance)
	require.NoError(t, db.QueryRow(`SELECT COALESCE(SUM(amount), 0)::text FROM shared_pool_earnings_transfers WHERE user_id = 6`).Scan(&amount))
	require.Equal(t, "4.00000000", amount)
}

func TestSharedPoolPostgresAutoTransferBelowThresholdMarksDay(t *testing.T) {
	db, cmd := sharedPoolPostgresFixture(t)
	applySharedPoolAutoTransferMigration(t, db)
	ctx := context.Background()
	_, err := NewUsageBillingRepository(nil, db).Apply(ctx, cmd)
	require.NoError(t, err)
	now := time.Now()
	repo := NewSharedPoolAutoTransferRepository(db)
	_, err = repo.Save(ctx, 6, service.SharedPoolAutoTransferUpdate{Enabled: true, Threshold: 5, DailyTime: "00:00"})
	require.NoError(t, err)
	result, err := repo.RunDue(ctx, 6, now)
	require.NoError(t, err)
	require.Nil(t, result)
	var marker string
	require.NoError(t, db.QueryRow(`SELECT last_run_date::text FROM shared_pool_auto_transfer_settings WHERE user_id = 6`).Scan(&marker))
	require.Equal(t, now.In(time.Local).Format("2006-01-02"), marker)
	var balance string
	require.NoError(t, db.QueryRow(`SELECT balance::text FROM users WHERE id = 6`).Scan(&balance))
	require.Equal(t, "10.00000000", balance)
}

func applySharedPoolAutoTransferMigration(t *testing.T, db *sql.DB) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "migrations", "258_shared_pool_auto_transfer.sql"))
	require.NoError(t, err)
	_, err = db.Exec(string(body))
	require.NoError(t, err)
}
