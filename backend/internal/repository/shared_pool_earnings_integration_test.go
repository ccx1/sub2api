//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedPoolEarningsConcurrentBillingAndTransfer(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	suffix := time.Now().UnixNano()
	consumer := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("pool-consumer-%d@example.com", suffix), PasswordHash: "hash", Balance: 100})
	owner := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("pool-owner-%d@example.com", suffix), PasswordHash: "hash", Balance: 10})
	group := mustCreateGroup(t, client, &service.Group{Name: fmt.Sprintf("pool-%d", suffix), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard})
	account := mustCreateAccount(t, client, &service.Account{Name: fmt.Sprintf("pool-%d", suffix), Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth})
	_, err := integrationDB.ExecContext(ctx, `UPDATE groups SET is_shared_pool = TRUE WHERE id = $1`, group.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO shared_pool_accounts (account_id, owner_user_id, enabled, admin_disabled, credential_fingerprint) VALUES ($1, $2, TRUE, FALSE, $3)`, account.ID, owner.ID, fmt.Sprintf("pool-credential-%d", suffix))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO shared_pool_user_rates (user_id, platform_rate_bps, proxy_rate_bps) VALUES ($1, 2000, 100)`, owner.ID)
	require.NoError(t, err)
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: consumer.ID, Name: "pool", Key: fmt.Sprintf("sk-pool-%d", suffix), GroupID: &group.ID})
	cmd := service.UsageBillingCommand{RequestID: fmt.Sprintf("pool-request-%d", suffix), APIKeyID: key.ID, UserID: consumer.ID,
		AccountID: account.ID, GroupID: group.ID, SharedPoolOwnerID: owner.ID, SharedPoolGroup: true, BalanceCost: 5}
	billing := NewUsageBillingRepository(nil, integrationDB)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); copy := cmd; _, err := billing.Apply(ctx, &copy); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM shared_pool_earnings WHERE owner_user_id = $1`, owner.ID).Scan(&count))
	require.Equal(t, 1, count)
	earnings := NewSharedPoolEarningsRepository(integrationDB)
	errs = make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := earnings.Transfer(ctx, owner.ID); errs <- err }()
	}
	wg.Wait()
	close(errs)
	succeeded := 0
	for err := range errs {
		if err == nil {
			succeeded++
		} else {
			require.True(t, errors.Is(err, service.ErrSharedPoolEarningsEmpty), "%v", err)
		}
	}
	require.Equal(t, 1, succeeded)
	var balance, totalRecharged float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance, total_recharged FROM users WHERE id = $1`, owner.ID).Scan(&balance, &totalRecharged))
	require.Equal(t, 14.0, balance)
	require.Zero(t, totalRecharged)
	summary, err := earnings.Summary(ctx, owner.ID)
	require.NoError(t, err)
	require.Zero(t, summary.Available)
	require.Equal(t, 4.0, summary.Transferred)
	require.Equal(t, 5.0, summary.BillingAmount)
}
