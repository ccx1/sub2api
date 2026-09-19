package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type spendGuardAPIKeyRepoStub struct {
	APIKeyRepository
	key       APIKey
	frozen    bool
	readErr   error
	updateErr error
	updates   int
}

func (r *spendGuardAPIKeyRepoStub) GetByID(context.Context, int64) (*APIKey, error) {
	key := r.key
	return &key, nil
}

func (r *spendGuardAPIKeyRepoStub) IsAPIKeySpendGuardFrozen(context.Context, int64) (bool, error) {
	return r.frozen, r.readErr
}

func (r *spendGuardAPIKeyRepoStub) Update(_ context.Context, key *APIKey, _ APIKeyUpdateFields) error {
	r.updates++
	if r.updateErr != nil {
		return r.updateErr
	}
	r.key = *key
	return nil
}

func TestAPIKeySpendGuardRejectsOwnerReactivation(t *testing.T) {
	repo := &spendGuardAPIKeyRepoStub{key: APIKey{ID: 1, UserID: 2, Status: StatusAPIKeyDisabled}, frozen: true}
	svc := &APIKeyService{apiKeyRepo: repo}
	active := StatusAPIKeyActive
	_, err := svc.Update(context.Background(), 1, 2, UpdateAPIKeyRequest{Status: &active})
	require.ErrorIs(t, err, ErrAPIKeySpendGuardFrozen)
	require.Zero(t, repo.updates)
	require.Equal(t, StatusAPIKeyDisabled, repo.key.Status)

	name := "renamed"
	_, err = svc.Update(context.Background(), 1, 2, UpdateAPIKeyRequest{Name: &name})
	require.NoError(t, err)
	require.Equal(t, StatusAPIKeyDisabled, repo.key.Status)
}

func TestAPIKeySpendGuardReactivationFailsClosedOnReadError(t *testing.T) {
	failure := errors.New("database unavailable")
	repo := &spendGuardAPIKeyRepoStub{key: APIKey{ID: 1, UserID: 2, Status: StatusAPIKeyDisabled}, readErr: failure}
	active := StatusAPIKeyActive
	_, err := (&APIKeyService{apiKeyRepo: repo}).Update(context.Background(), 1, 2, UpdateAPIKeyRequest{Status: &active})
	require.ErrorIs(t, err, failure)
	require.Zero(t, repo.updates)
}

func TestAPIKeySpendGuardConcurrentFreezeErrorSurvivesUpdate(t *testing.T) {
	// 预检查之后另一实例冻结：仓储的事务保护仍必须阻止旧 active 写回。
	repo := &spendGuardAPIKeyRepoStub{
		key: APIKey{ID: 1, UserID: 2, Status: "inactive"}, updateErr: ErrAPIKeySpendGuardFrozen,
	}
	active := StatusAPIKeyActive
	_, err := (&APIKeyService{apiKeyRepo: repo}).Update(context.Background(), 1, 2, UpdateAPIKeyRequest{Status: &active})
	require.ErrorIs(t, err, ErrAPIKeySpendGuardFrozen)
	require.Equal(t, 1, repo.updates)
	require.Equal(t, "inactive", repo.key.Status)
}

func TestAPIKeySpendGuardAllowsOwnerReactivationAfterAdminRelease(t *testing.T) {
	repo := &spendGuardAPIKeyRepoStub{key: APIKey{ID: 1, UserID: 2, Status: "inactive"}}
	active := StatusAPIKeyActive
	key, err := (&APIKeyService{apiKeyRepo: repo}).Update(context.Background(), 1, 2, UpdateAPIKeyRequest{Status: &active})
	require.NoError(t, err)
	require.Equal(t, StatusAPIKeyActive, key.Status)
}
