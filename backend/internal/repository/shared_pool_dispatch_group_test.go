package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedPoolDispatchGroupReadsLatestStateAndExcludesDeleted(t *testing.T) {
	_, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	repo := newAccountRepositoryWithSQL(client, nil, nil)
	row, err := client.Group.Create().SetName("dispatch").SetPlatform(service.PlatformOpenAI).SetRateMultiplier(2).Save(ctx)
	require.NoError(t, err)
	first, err := repo.SharedPoolDispatchGroup(ctx, row.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusActive, first.Status)
	require.Equal(t, 2.0, first.RateMultiplier)
	require.NoError(t, client.Group.UpdateOneID(row.ID).SetStatus(service.StatusDisabled).SetRateMultiplier(.4).Exec(ctx))
	second, err := repo.SharedPoolDispatchGroup(ctx, row.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusDisabled, second.Status)
	require.Equal(t, .4, second.RateMultiplier)
	require.Equal(t, service.StatusActive, first.Status)
	require.Equal(t, 2.0, first.RateMultiplier)
	require.NoError(t, client.Group.UpdateOneID(row.ID).SetDeletedAt(time.Now()).Exec(ctx))
	_, err = repo.SharedPoolDispatchGroup(ctx, row.ID)
	require.ErrorIs(t, err, service.ErrGroupNotFound)
}
