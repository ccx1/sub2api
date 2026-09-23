//go:build unit

package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type sharedImportRepositoryStub struct {
	service.SharedPoolRepository
	service.AccountRepository
	mu       sync.Mutex
	accounts map[int64]*service.Account
	owners   map[int64]int64
	seen     map[string]bool
}

func (r *sharedImportRepositoryStub) SharedSettings(context.Context) (*service.SharedPoolSettings, error) {
	return &service.SharedPoolSettings{MaxConcurrency: 10, DefaultPriority: 17, DefaultGroupIDs: service.SharedPoolDefaultGroupIDs{"gemini": {8, 10}},
		SettlementMultiplier: 1,
		SubscriptionGroupIDs: service.SharedPoolSubscriptionGroupIDs{"gemini": {"gcp_enterprise": {9}}}}, nil
}
func (r *sharedImportRepositoryStub) SharedUserRates(context.Context) ([]service.SharedPoolUserRate, error) {
	return nil, nil
}
func (r *sharedImportRepositoryStub) CreateSharedAccount(_ context.Context, account *service.Account, owner int64, fingerprint string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen[fingerprint] {
		return infraerrors.Conflict("SHARED_ACCOUNT_EXISTS", "该凭证已加入共享池")
	}
	r.seen[fingerprint] = true
	account.ID = int64(len(r.accounts) + 1)
	r.accounts[account.ID] = account
	r.owners[account.ID] = owner
	return nil
}
func (r *sharedImportRepositoryStub) GetSharedAccount(_ context.Context, owner, id int64) (*service.SharedPoolAccountRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.owners[id] != owner {
		return nil, service.ErrSharedPoolAccountNotFound
	}
	return &service.SharedPoolAccountRecord{AccountID: id, OwnerUserID: owner, Enabled: true}, nil
}
func (r *sharedImportRepositoryStub) GetByID(_ context.Context, id int64) (*service.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.accounts[id], nil
}

type sharedImportEarningsStub struct {
	service.SharedPoolEarningsRepository
}

type sharedImportGroupsStub struct{ service.GroupRepository }

func (sharedImportGroupsStub) GetByID(_ context.Context, id int64) (*service.Group, error) {
	return &service.Group{ID: id, Platform: service.PlatformGemini, IsSharedPool: true, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard}, nil
}

func (sharedImportEarningsStub) AccountTotals(context.Context, int64, []int64) (map[int64]service.SharedPoolAccountEarnings, error) {
	return map[int64]service.SharedPoolAccountEarnings{}, nil
}

func TestSharedImportUsesSharedServiceBoundaryAndDoesNotUpdateDuplicates(t *testing.T) {
	r := &sharedImportRepositoryStub{accounts: map[int64]*service.Account{}, owners: map[int64]int64{}, seen: map[string]bool{}}
	pool := service.NewSharedPoolService(r, r, sharedImportGroupsStub{}, nil, nil, sharedImportEarningsStub{}, nil)
	content := `[{"name":"mine","platform":"gemini","type":"oauth","credentials":{"access_token":"token","base_url":"http://127.0.0.1"},"extra":{"shared_pool_owner_id":77},"group_ids":[999],"rate_multiplier":0,"proxy_id":3,"priority":0}]`
	entries, err := parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Content: content}}, Defaults: importTestDefaults()})
	require.NoError(t, err)
	result, err := executeSharedImport(context.Background(), 901, entries, pool.Create)
	require.NoError(t, err)
	require.Equal(t, 1, result.Created)
	account := r.accounts[1]
	require.Equal(t, int64(901), r.owners[1])
	require.Equal(t, int64(901), account.Extra[service.SharedPoolOwnerKey])
	require.Equal(t, []int64{8, 10}, account.GroupIDs)
	require.Equal(t, 17, account.Priority, "使用平台配置的优先级，不能接受用户导入的覆盖值")
	require.Nil(t, account.RateMultiplier)
	require.Nil(t, account.ProxyID)
	require.True(t, account.IsRandomProxy())
	require.NotContains(t, account.Credentials, "base_url")
	for _, owner := range []int64{901, 902} {
		result, err = executeSharedImport(context.Background(), owner, entries, pool.Create)
		require.NoError(t, err)
		require.Zero(t, result.Created)
		require.Equal(t, 1, result.Failed)
	}
	require.Len(t, r.accounts, 1)
	require.Equal(t, int64(901), r.owners[1])
}

func TestSharedImportSubscriptionGroupAssignment(t *testing.T) {
	r := &sharedImportRepositoryStub{accounts: map[int64]*service.Account{}, owners: map[int64]int64{}, seen: map[string]bool{}}
	pool := service.NewSharedPoolService(r, r, sharedImportGroupsStub{}, nil, nil, sharedImportEarningsStub{}, nil)
	content := `[{"name":"enterprise","platform":"gemini","type":"oauth","credentials":{"access_token":"tier-test-token","oauth_type":"code_assist","tier_id":"ENTERPRISE"},"group_ids":[999]}]`
	entries, err := parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Content: content}}, Defaults: importTestDefaults()})
	require.NoError(t, err)
	result, err := executeSharedImport(context.Background(), 901, entries, pool.Create)
	require.NoError(t, err)
	require.Equal(t, 1, result.Created)
	require.Equal(t, []int64{9}, r.accounts[1].GroupIDs)
}

func TestSharedImportIdempotencyReplaysAndSeparatesUsers(t *testing.T) {
	previous := service.DefaultIdempotencyCoordinator()
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(newUserMemoryIdempotencyRepoStub(), service.DefaultIdempotencyConfig()))
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(previous) })
	request := sharedImportRequest{Sources: []sharedImportSource{{Content: "secret-token"}}, Defaults: importTestDefaults()}
	executed := 0
	invoke := func(owner int64) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/shared-pool/accounts/import", strings.NewReader("{}"))
		c.Request.Header.Set("Idempotency-Key", "shared-import-test-replay")
		executeSharedImportIdempotent(c, owner, request, func(context.Context) (any, error) {
			executed++
			return sharedImportResult{Total: 1, Created: 1, Items: []sharedImportItem{{Index: 1, Name: "mine", AccountID: 1}}, Warnings: []string{}}, nil
		})
		return w
	}
	first := invoke(903)
	second := invoke(903)
	third := invoke(904)
	require.Equal(t, 200, first.Code)
	require.Equal(t, "true", second.Header().Get("X-Idempotency-Replayed"))
	require.Equal(t, 200, third.Code)
	require.Equal(t, 2, executed)
	require.NotContains(t, first.Body.String(), "secret-token")
}
