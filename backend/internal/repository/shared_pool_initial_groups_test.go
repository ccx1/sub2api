//go:build unit

package repository

import (
	"context"
	"errors"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedPoolCreateAcceptsConsentedProliteTierGroup(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	repo := &sharedPoolRepository{client: client, db: db}
	account := &service.Account{
		Name: "import@example.test", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "fixture-token", "plan_type": "prolite"},
		GroupIDs:    []int64{11}, Extra: map[string]any{service.SharedPoolDispatchConsentKey: true},
	}
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "groups"`).WithArgs(int64(11)).WillReturnRows(sqlmock.NewRows(
		[]string{"id", "platform", "status", "subscription_type", "is_exclusive"}).
		AddRow(11, "openai", "active", "standard", true))
	mock.ExpectQuery("SELECT subscription_group_ids FROM shared_pool_settings").WillReturnRows(
		sqlmock.NewRows([]string{"subscription_group_ids"}).AddRow([]byte(`{"openai":{"prolite":[11]}}`)))
	expectSharedCredentialAbsent(mock)
	writeErr := errors.New("synthetic account write failure")
	mock.ExpectQuery(`INSERT INTO "accounts"`).WillReturnError(writeErr)
	mock.ExpectRollback()
	require.ErrorIs(t, repo.CreateSharedAccount(context.Background(), account, 7, "fixture-fingerprint"), writeErr)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSharedPoolCreatePreservesGroupLookupFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	repo := &sharedPoolRepository{client: client, db: db}
	mock.ExpectBegin()
	lookupErr := errors.New("synthetic database unavailable")
	mock.ExpectQuery(`SELECT .* FROM "groups"`).WillReturnError(lookupErr)
	mock.ExpectRollback()
	err = repo.CreateSharedAccount(context.Background(), &service.Account{GroupIDs: []int64{11}}, 7, "fixture-fingerprint")
	require.ErrorIs(t, err, lookupErr)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSharedPoolTierGroupExceptionPreservesBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, rules string
		change      func(*service.Account, *service.Group)
		allowed     bool
		queryErr    bool
		wantErr     bool
	}{
		{name: "configured prolite", rules: `{"openai":{"prolite":[11]}}`, allowed: true},
		{name: "different tier", rules: `{"openai":{"pro":[11]}}`},
		{name: "different group", rules: `{"openai":{"prolite":[12]}}`},
		{name: "different rule platform", rules: `{"gemini":{"prolite":[11]}}`},
		{name: "unconfigured", rules: `{}`},
		{name: "malformed rules", rules: `{`, wantErr: true},
		{name: "query failure", queryErr: true, wantErr: true},
		{name: "missing consent", change: func(a *service.Account, _ *service.Group) { a.Extra = nil }},
		{name: "explicit refusal", change: func(a *service.Account, _ *service.Group) { a.Extra[service.SharedPoolDispatchConsentKey] = false }},
		{name: "api key", change: func(a *service.Account, _ *service.Group) { a.Type = service.AccountTypeAPIKey }},
		{name: "unknown tier", change: func(a *service.Account, _ *service.Group) { a.Credentials["plan_type"] = "unknown" }},
		{name: "wrong group platform", change: func(_ *service.Account, g *service.Group) { g.Platform = service.PlatformGemini }},
		{name: "inactive", change: func(_ *service.Account, g *service.Group) { g.Status = service.StatusDisabled }},
		{name: "subscription billing", change: func(_ *service.Account, g *service.Group) { g.SubscriptionType = service.SubscriptionTypeSubscription }},
		{name: "composite", change: func(a *service.Account, g *service.Group) {
			a.Platform, g.Platform = service.PlatformComposite, service.PlatformComposite
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			a := &service.Account{Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
				Credentials: map[string]any{"plan_type": "prolite"}, Extra: map[string]any{service.SharedPoolDispatchConsentKey: true}}
			g := &service.Group{ID: 11, Platform: service.PlatformOpenAI, Status: service.StatusActive,
				SubscriptionType: service.SubscriptionTypeStandard, IsExclusive: true, RequireOAuthOnly: true}
			if tc.change != nil {
				tc.change(a, g)
			}
			if tc.queryErr {
				mock.ExpectQuery("SELECT subscription_group_ids FROM shared_pool_settings").WillReturnError(errors.New("fixture query failure"))
			} else if tc.rules != "" {
				mock.ExpectQuery("SELECT subscription_group_ids FROM shared_pool_settings").WillReturnRows(
					sqlmock.NewRows([]string{"subscription_group_ids"}).AddRow([]byte(tc.rules)))
			}
			allowed, err := sharedAccountTierGroupAllowed(context.Background(), db, a, g)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.allowed, allowed)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// 只替代查询依赖及隐私出站边界；创建、套餐校验和所有事务写入使用实际 repository。
type sharedImportServiceRepository struct {
	*sharedPoolRepository
	created *service.Account
}

func (r *sharedImportServiceRepository) CreateSharedAccount(ctx context.Context, account *service.Account, ownerID int64, fingerprint string) error {
	r.created = account
	return r.sharedPoolRepository.CreateSharedAccount(ctx, account, ownerID, fingerprint)
}

type sharedImportServiceAccounts struct {
	service.AccountRepository
	repo *sharedImportServiceRepository
}

func (a sharedImportServiceAccounts) GetByID(context.Context, int64) (*service.Account, error) {
	return a.repo.created, nil
}

type sharedImportServiceGroups struct {
	service.GroupRepository
	group *service.Group
}

func (g sharedImportServiceGroups) GetByID(_ context.Context, id int64) (*service.Group, error) {
	if id != g.group.ID {
		return nil, service.ErrGroupNotFound
	}
	return g.group, nil
}

type sharedImportOfflineAdmin struct {
	service.AdminService
	calls int
}

func (a *sharedImportOfflineAdmin) EnsureOpenAIPrivacy(context.Context, *service.Account) string {
	a.calls++
	return "fixture-offline"
}

type sharedImportServiceEarnings struct {
	service.SharedPoolEarningsRepository
}

func (sharedImportServiceEarnings) AccountTotals(context.Context, int64, []int64) (map[int64]service.SharedPoolAccountEarnings, error) {
	return map[int64]service.SharedPoolAccountEarnings{}, nil
}

func expectSharedImportSettings(mock sqlmock.Sqlmock, rules string) {
	mock.ExpectQuery("SELECT platform_rate_bps,proxy_rate_bps,max_concurrency,default_group_ids").
		WillReturnRows(sqlmock.NewRows([]string{"platform_rate_bps", "proxy_rate_bps", "max_concurrency", "default_group_ids", "subscription_group_ids", "settlement_multiplier", "subscription_settlement_multipliers", "default_priority"}).
			AddRow(1000, 0, 5, []byte(`{"openai":[11]}`), []byte(rules), 1, []byte("{}"), 50))
}

func expectSharedImportView(mock sqlmock.Sqlmock, rules string) {
	mock.ExpectQuery("SELECT s.account_id,s.owner_user_id").WithArgs(int64(41), int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "owner_user_id", "email", "enabled", "admin_disabled", "assigned"}).
			AddRow(41, 7, "owner@example.test", true, false, true))
	expectSharedImportSettings(mock, rules)
	mock.ExpectQuery("SELECT r.user_id,u.email,r.platform_rate_bps").WillReturnRows(sqlmock.NewRows([]string{"user_id", "email", "platform_rate_bps", "proxy_rate_bps", "settlement_multiplier"}))
}

func TestSharedPoolImportServicePersistsProliteThroughRepository(t *testing.T) {
	for _, tc := range []struct {
		name, failure string
		exclusive     bool
	}{
		{name: "prolite exclusive tier", exclusive: true},
		{name: "nonexclusive default"},
		{name: "outbox rolls back cross-layer creation", exclusive: true, failure: "outbox"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSharedImportFixture(t)
			f.group.IsExclusive = tc.exclusive
			rules := "{}"
			if tc.exclusive {
				rules = `{"openai":{"prolite":[11]}}`
			}
			expectSharedImportSettings(f.mock, rules)
			f.mock.ExpectBegin()
			expectSharedImportGroup(f.mock, f.group)
			if tc.exclusive {
				expectSharedImportRules(f.mock, &rules)
			}
			wantErr := expectSharedImportWrites(f.mock, tc.failure)
			if wantErr != nil {
				f.mock.ExpectRollback()
			} else {
				expectSharedImportView(f.mock, rules)
			}
			repo, admin := &sharedImportServiceRepository{sharedPoolRepository: f.repo}, &sharedImportOfflineAdmin{}
			svc := service.NewSharedPoolService(repo, sharedImportServiceAccounts{repo: repo}, sharedImportServiceGroups{group: f.group}, admin, nil, sharedImportServiceEarnings{}, nil)
			view, err := svc.Create(context.Background(), 7, service.SharedPoolAccountInput{
				Name: f.account.Name, Platform: f.account.Platform, Type: f.account.Type, Credentials: f.account.Credentials,
				Concurrency: 1, Enabled: true, DispatchConsent: true,
			})
			if wantErr != nil {
				require.ErrorIs(t, err, wantErr)
				require.Nil(t, view)
			} else {
				require.NoError(t, err)
				require.Equal(t, int64(41), view.ID)
				require.Equal(t, "prolite", view.SubscriptionTier)
				require.True(t, view.Enabled)
				require.True(t, view.DispatchConsent)
			}
			require.NotNil(t, repo.created)
			require.Equal(t, []int64{11}, repo.created.GroupIDs)
			require.Equal(t, "prolite", repo.created.Credentials["plan_type"])
			require.Zero(t, admin.calls, "默认空代理池 reject，测试不得触发隐私出站")
		})
	}
}

func TestSharedPoolImportServiceRejectsUnconsentedEnableBeforePersistence(t *testing.T) {
	f := newSharedImportFixture(t)
	svc := service.NewSharedPoolService(f.repo, nil, nil, nil, nil, nil, nil)
	view, err := svc.Create(context.Background(), 7, service.SharedPoolAccountInput{Enabled: true})
	require.Nil(t, view)
	require.Equal(t, "SHARED_DISPATCH_CONSENT_REQUIRED", infraerrors.Reason(err))
}
