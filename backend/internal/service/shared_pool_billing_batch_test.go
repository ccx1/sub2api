//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSharedPoolLegacyMarkerDoesNotBlockOrdinaryBatchImageAccounts(t *testing.T) {
	svc, _, _, _, _ := newTestBatchImagePublicService(true)
	groupID := int64(7)
	svc.GroupRepo = &publicBatchImageGroupRepo{groups: map[int64]*Group{
		groupID: {ID: groupID, Platform: PlatformGemini, IsSharedPool: true, AllowBatchImageGeneration: true},
	}}
	require.NoError(t, svc.ensureGroupAllowsBatchImage(context.Background(), &groupID))
}

func TestSharedPoolBatchImageCannotSelectSharedAccountWithoutGroup(t *testing.T) {
	svc, repo, queue, provider, _ := newTestBatchImagePublicService(true)
	accounts := svc.AccountRepo.(*publicBatchImageAccountRepo)
	for i := range accounts.accounts {
		accounts.accounts[i].Extra = map[string]any{"shared_pool_owner_id": int64(6)}
	}
	_, err := svc.Submit(context.Background(), testBatchImageOwner(), validBatchImageSubmitRequest(), "")
	require.ErrorIs(t, err, ErrBatchImageNoAccountAvailable)
	require.Empty(t, repo.jobs)
	require.Empty(t, queue.enqueued)
	require.Empty(t, provider.submits)
}
