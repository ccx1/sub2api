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

type sharedImportFixture struct {
	repo    *sharedPoolRepository
	mock    sqlmock.Sqlmock
	account *service.Account
	group   *service.Group
}

func newSharedImportFixture(t *testing.T) sharedImportFixture {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() {
		require.NoError(t, mock.ExpectationsWereMet())
		_ = client.Close()
	})
	return sharedImportFixture{
		repo: &sharedPoolRepository{client: client, db: db}, mock: mock,
		account: &service.Account{Name: "prolite-import@example.test", Platform: service.PlatformOpenAI,
			Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": "fixture-token", "plan_type": "prolite"},
			GroupIDs: []int64{11}, Concurrency: 1, Priority: 50, Status: service.StatusActive, Schedulable: true,
			Extra: map[string]any{service.SharedPoolDispatchConsentKey: true, service.SharedPoolEnabledKey: true, service.SharedPoolOwnerKey: int64(7)}},
		group: &service.Group{ID: 11, Platform: service.PlatformOpenAI, Status: service.StatusActive,
			SubscriptionType: service.SubscriptionTypeStandard, IsExclusive: true, RequireOAuthOnly: true},
	}
}

func expectSharedImportGroup(mock sqlmock.Sqlmock, group *service.Group) {
	rows := sqlmock.NewRows([]string{"id", "platform", "status", "subscription_type", "is_exclusive", "is_shared_pool", "require_oauth_only"})
	if group != nil {
		rows.AddRow(group.ID, group.Platform, group.Status, group.SubscriptionType, group.IsExclusive, group.IsSharedPool, group.RequireOAuthOnly)
	}
	// 同时约束软删除过滤与行锁，缺失和已删除分组不能进入持久化。
	mock.ExpectQuery(`SELECT .* FROM "groups".*"deleted_at" IS NULL.*FOR SHARE`).WithArgs(int64(11)).WillReturnRows(rows)
}

func expectSharedImportRules(mock sqlmock.Sqlmock, rules *string) {
	rows := sqlmock.NewRows([]string{"subscription_group_ids"})
	if rules != nil {
		rows.AddRow([]byte(*rules))
	}
	mock.ExpectQuery("SELECT subscription_group_ids FROM shared_pool_settings WHERE id=1 FOR SHARE").
		WillReturnRows(rows).RowsWillBeClosed()
}

func expectSharedImportWrites(mock sqlmock.Sqlmock, failure string) error {
	failureErr := errors.New("fixture " + failure + " failure")
	account := mock.ExpectQuery(`INSERT INTO "accounts"`)
	if failure == "account" {
		account.WillReturnError(failureErr)
		return failureErr
	}
	account.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(41))
	owner := mock.ExpectExec("INSERT INTO shared_pool_accounts").
		WithArgs(int64(41), int64(7), sqlmock.AnyArg(), true, true)
	if failure == "ownership" {
		owner.WillReturnError(failureErr)
		return failureErr
	}
	owner.WillReturnResult(sqlmock.NewResult(1, 1))
	link := mock.ExpectExec(`INSERT INTO "account_groups"`).
		WithArgs(50, sqlmock.AnyArg(), int64(41), int64(11))
	if failure == "association" {
		link.WillReturnError(failureErr)
		return failureErr
	}
	link.WillReturnResult(sqlmock.NewResult(1, 1))
	outbox := mock.ExpectExec("INSERT INTO scheduler_outbox").
		WithArgs(service.SchedulerOutboxEventAccountChanged, int64(41), nil, []byte(`{"group_ids":[11]}`), sqlmock.AnyArg())
	if failure == "outbox" {
		outbox.WillReturnError(failureErr)
		return failureErr
	}
	outbox.WillReturnResult(sqlmock.NewResult(1, 1))
	if failure == "commit" {
		mock.ExpectCommit().WillReturnError(failureErr)
		return failureErr
	}
	mock.ExpectCommit()
	return nil
}

func TestSharedPoolImportCreatePersistsAndCommits(t *testing.T) {
	for _, consented := range []bool{true, false} {
		name := "authorized prolite exclusive tier"
		if !consented {
			name = "legacy shared nonexclusive default"
		}
		t.Run(name, func(t *testing.T) {
			f := newSharedImportFixture(t)
			f.account.Extra[service.SharedPoolDispatchConsentKey] = consented
			f.group.IsExclusive, f.group.IsSharedPool = consented, !consented
			f.mock.ExpectBegin()
			expectSharedImportGroup(f.mock, f.group)
			if consented {
				expectSharedImportRules(f.mock, new(`{"openai":{"prolite":[11]}}`))
			}
			expectSharedImportWrites(f.mock, "")
			require.NoError(t, f.repo.CreateSharedAccount(context.Background(), f.account, 7, "fixture-fingerprint"))
			require.Equal(t, int64(41), f.account.ID)
			require.Equal(t, []int64{11}, f.account.GroupIDs)
			require.False(t, f.account.CreatedAt.IsZero())
		})
	}
}

func TestSharedPoolImportCreateRollsBackWriteFailures(t *testing.T) {
	for _, failure := range []string{"account", "ownership", "association", "outbox", "commit"} {
		t.Run(failure, func(t *testing.T) {
			f := newSharedImportFixture(t)
			f.mock.ExpectBegin()
			expectSharedImportGroup(f.mock, f.group)
			expectSharedImportRules(f.mock, new(`{"openai":{"prolite":[11]}}`))
			wantErr := expectSharedImportWrites(f.mock, failure)
			if failure != "commit" {
				f.mock.ExpectRollback()
			}
			err := f.repo.CreateSharedAccount(context.Background(), f.account, 7, "fixture-fingerprint")
			require.ErrorIs(t, err, wantErr)
		})
	}
}

func TestSharedPoolImportCreateRejectsInvalidInitialGroups(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		change                   func(*service.Account, *service.Group)
		missingGroup, queryRules bool
		rules                    *string
	}{
		{name: "missing or soft-deleted group", missingGroup: true},
		{name: "missing settings row", queryRules: true},
		{name: "no tier configuration", queryRules: true, rules: new(`{}`)},
		{name: "different tier configuration", queryRules: true, rules: new(`{"openai":{"pro":[11]}}`)},
		{name: "different configured group", queryRules: true, rules: new(`{"openai":{"prolite":[12]}}`)},
		{name: "missing consent", change: func(a *service.Account, _ *service.Group) { delete(a.Extra, service.SharedPoolDispatchConsentKey) }},
		{name: "explicit refusal", change: func(a *service.Account, _ *service.Group) { a.Extra[service.SharedPoolDispatchConsentKey] = false }},
		{name: "api key", change: func(a *service.Account, _ *service.Group) { a.Type = service.AccountTypeAPIKey }},
		{name: "different platform", change: func(_ *service.Account, g *service.Group) { g.Platform = service.PlatformGemini }},
		{name: "subscription billing", change: func(_ *service.Account, g *service.Group) { g.SubscriptionType = service.SubscriptionTypeSubscription }},
		{name: "disabled group", change: func(_ *service.Account, g *service.Group) { g.Status = service.StatusDisabled }},
		{name: "unknown tier", change: func(a *service.Account, _ *service.Group) { a.Credentials["plan_type"] = "unknown" }},
		{name: "unconsented nonshared default", change: func(a *service.Account, g *service.Group) {
			a.Extra[service.SharedPoolDispatchConsentKey], g.IsExclusive = false, false
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSharedImportFixture(t)
			if tc.change != nil {
				tc.change(f.account, f.group)
			}
			f.mock.ExpectBegin()
			group := f.group
			if tc.missingGroup {
				group = nil
			}
			expectSharedImportGroup(f.mock, group)
			if tc.queryRules {
				expectSharedImportRules(f.mock, tc.rules)
			}
			f.mock.ExpectRollback()
			err := f.repo.CreateSharedAccount(context.Background(), f.account, 7, "fixture-fingerprint")
			require.Equal(t, "INVALID_SHARED_GROUP", infraerrors.Reason(err))
			require.Zero(t, f.account.ID)
		})
	}
}
