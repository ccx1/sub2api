//go:build unit

package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type sharedAdminStateRepository struct {
	service.SharedPoolRepository
	service.AccountRepository
	account *service.Account
	state   service.SharedPoolAccountState
	writes  int
	err     error
}

func (r *sharedAdminStateRepository) GetSharedAccount(_ context.Context, owner, id int64) (*service.SharedPoolAccountRecord, error) {
	if owner != 0 || id != r.account.ID {
		return nil, service.ErrSharedPoolAccountNotFound
	}
	return &service.SharedPoolAccountRecord{AccountID: id, OwnerUserID: 7, Assigned: true}, nil
}
func (r *sharedAdminStateRepository) GetByID(context.Context, int64) (*service.Account, error) {
	return r.account, nil
}
func (r *sharedAdminStateRepository) SharedSettings(context.Context) (*service.SharedPoolSettings, error) {
	return &service.SharedPoolSettings{SettlementMultiplier: 1}, nil
}
func (r *sharedAdminStateRepository) SharedUserRates(context.Context) ([]service.SharedPoolUserRate, error) {
	return nil, nil
}
func (r *sharedAdminStateRepository) SetSharedAccountState(_ context.Context, _ int64, state service.SharedPoolAccountState) error {
	r.state = state
	r.writes++
	return r.err
}

func TestSharedAdminStateHandlerAcceptsEnableAndTierFields(t *testing.T) {
	for _, fail := range []bool{false, true} {
		repo := &sharedAdminStateRepository{account: &service.Account{ID: 11, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Extra: map[string]any{service.SharedPoolDispatchConsentKey: true}}}
		if fail {
			repo.err = errors.New("state failed")
		}
		h := &SharedPoolHandler{pool: service.NewSharedPoolService(repo, repo, nil, nil, nil, sharedImportEarningsStub{}, nil)}
		c, w := sharedTestContext(99, `{"enabled":true,"admin_disabled":false,"subscription_tier":"pro","priority":17}`)
		h.AdminAssign(c)
		if fail {
			require.Equal(t, 500, w.Code)
		} else {
			require.Equal(t, 200, w.Code)
		}
		require.Equal(t, 1, repo.writes)
		require.Equal(t, new(true), repo.state.Enabled)
		require.Equal(t, new(false), repo.state.AdminDisabled)
		require.Equal(t, new("pro"), repo.state.SubscriptionTier)
		require.Equal(t, new(17), repo.state.Priority)
		require.Nil(t, repo.state.DispatchConsent)
	}
}

func TestSharedAdminStateHandlerForbidsAuthorizationAndOwnerTierWrites(t *testing.T) {
	for _, body := range []string{`{"enabled":true,"dispatch_consent":true}`, `{"owner_id":7,"enabled":true}`, `{}`} {
		h := &SharedPoolHandler{}
		c, w := sharedTestContext(7, body)
		h.AdminAssign(c)
		require.Equal(t, 400, w.Code)
	}
	h := &SharedPoolHandler{}
	c, w := sharedTestContext(7, `{"subscription_tier":"pro"}`)
	h.Update(c)
	require.Equal(t, 400, w.Code)
}
