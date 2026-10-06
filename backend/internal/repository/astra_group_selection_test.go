package repository

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func newAstraGroupSelectionRepo(t *testing.T) (*settingRepository, *sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(func(expected, actual string) error {
		if strings.Contains(actual, "astra_group_accounts") && strings.Contains(actual, "schedulable") {
			return errors.New("group selection must retain Astra-paused accounts")
		}
		return sqlmock.QueryMatcherRegexp.Match(expected, actual)
	})))
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	return &settingRepository{client: client}, db, mock
}

func astraGroupSelectionSettings() config.AstraRoutingSettings {
	return config.AstraRoutingSettings{Revision: "groups-v1", CookiePool: config.CodexGatewayPinConfig{
		Enabled: true, SourceSelection: "groups", SourceGroupIDs: []int64{11},
		SourceAccountIDs: []int64{999}, TargetAccountIDs: []int64{202},
	}}
}

func expectAstraSelectionGroups(mock sqlmock.Sqlmock, ids ...int64) *sqlmock.ExpectedQuery {
	return mock.ExpectQuery(`(?s)astra_selection_groups.*deleted_at IS NULL.*status='active'.*platform='openai'`).WithArgs(pq.Array(ids))
}

func expectAstraGroupAccounts(mock sqlmock.Sqlmock, groups, accounts []int64) {
	groupRows := sqlmock.NewRows([]string{"id"})
	for _, id := range groups {
		groupRows.AddRow(id)
	}
	expectAstraSelectionGroups(mock, groups...).WillReturnRows(groupRows)
	accountRows := sqlmock.NewRows([]string{"id"})
	for _, id := range accounts {
		accountRows.AddRow(id)
	}
	// 匹配实际资格约束；停调账号必须仍被解析，才能在验证恢复后重新启用。
	mock.ExpectQuery(`(?s)astra_group_accounts.*SELECT DISTINCT a.id.*g.deleted_at IS NULL.*g.status='active'.*g.platform='openai'.*a.deleted_at IS NULL.*a.status='active'.*a.platform='openai'.*a.type='oauth'.*a.parent_account_id IS NULL.*a.expires_at>NOW\(\).*ORDER BY a.id`).
		WithArgs(pq.Array(groups)).WillReturnRows(accountRows)
}

func TestAstraGroupSelectionUsesLiveDistinctMembers(t *testing.T) {
	repo, _, mock := newAstraGroupSelectionRepo(t)
	value := astraGroupSelectionSettings()
	value.CookiePool.SourceGroupIDs = []int64{11, 12}
	expectAstraGroupAccounts(mock, []int64{11, 12}, []int64{103, 101, 103})
	resolved, err := repo.ResolveAstraRoutingAccounts(t.Context(), value)
	require.NoError(t, err)
	require.Equal(t, []int64{101, 103}, resolved.CookiePool.SourceAccountIDs)
	require.Equal(t, []int64{202}, resolved.CookiePool.TargetAccountIDs)
	require.Equal(t, []int64{999}, value.CookiePool.SourceAccountIDs, "caller preview must not be mutated")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAstraGroupSelectionValidatesEverySelectedGroup(t *testing.T) {
	repo, _, mock := newAstraGroupSelectionRepo(t)
	value := astraGroupSelectionSettings()
	value.CookiePool.SourceGroupIDs = []int64{11, 12}
	expectAstraSelectionGroups(mock, 11, 12).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11))
	resolved, err := repo.ResolveAstraRoutingAccounts(t.Context(), value)
	require.ErrorContains(t, err, "astra_group_unavailable")
	require.Empty(t, resolved.CookiePool.SourceAccountIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAstraGroupSelectionEnforcesResolvedBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name string
		ids  []int64
		want string
	}{{"empty", nil, "astra_group_accounts_empty"}, {"all targets are sources", []int64{202}, "astra_target_required"}, {"too many", astraSequentialIDs(65), "astra_group_accounts_limit"}} {
		t.Run(tc.name, func(t *testing.T) {
			repo, _, mock := newAstraGroupSelectionRepo(t)
			expectAstraGroupAccounts(mock, []int64{11}, tc.ids)
			resolved, err := repo.ResolveAstraRoutingAccounts(t.Context(), astraGroupSelectionSettings())
			require.ErrorContains(t, err, tc.want)
			require.Empty(t, resolved.CookiePool.SourceAccountIDs)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestAstraGroupSelectionSubtractsSourcesFromTargets(t *testing.T) {
	for _, mode := range []string{"groups", "accounts"} {
		t.Run(mode, func(t *testing.T) {
			repo, _, mock := newAstraGroupSelectionRepo(t)
			value := astraGroupSelectionSettings()
			value.CookiePool.TargetSelection = mode
			value.CookiePool.TargetAccountIDs = []int64{202, 303}
			expectAstraGroupAccounts(mock, []int64{11}, []int64{101, 202})
			if mode == "groups" {
				value.CookiePool.TargetGroupIDs = []int64{12}
				expectAstraGroupAccounts(mock, []int64{12}, []int64{202, 303})
			}
			resolved, err := repo.ResolveAstraRoutingAccounts(t.Context(), value)
			require.NoError(t, err)
			require.Equal(t, []int64{303}, resolved.CookiePool.TargetAccountIDs)
			require.Equal(t, []int64{101, 202}, resolved.CookiePool.SourceAccountIDs)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestAstraGroupSelectionAppliesTargetLimitAfterExclusion(t *testing.T) {
	repo, _, mock := newAstraGroupSelectionRepo(t)
	value := astraGroupSelectionSettings()
	value.CookiePool.TargetSelection = "groups"
	value.CookiePool.TargetGroupIDs = []int64{12}
	expectAstraGroupAccounts(mock, []int64{11}, []int64{1})
	expectAstraGroupAccounts(mock, []int64{12}, astraSequentialIDs(65))
	resolved, err := repo.ResolveAstraRoutingAccounts(t.Context(), value)
	require.NoError(t, err)
	require.Len(t, resolved.CookiePool.TargetAccountIDs, 64)
	require.NotContains(t, resolved.CookiePool.TargetAccountIDs, int64(1))
	require.NoError(t, mock.ExpectationsWereMet())
}

func astraSequentialIDs(count int) []int64 {
	ids := make([]int64, count)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	return ids
}

func TestAstraGroupSelectionRejectsWSOutsideTargets(t *testing.T) {
	repo, _, mock := newAstraGroupSelectionRepo(t)
	value := astraGroupSelectionSettings()
	value.CookiePool.TargetSelection = "groups"
	value.CookiePool.TargetGroupIDs = []int64{12}
	value.WSSession = config.CodexWSAnchorConfig{Enabled: true, AccountIDs: []int64{303}}
	expectAstraGroupAccounts(mock, []int64{11}, []int64{101})
	expectAstraGroupAccounts(mock, []int64{12}, []int64{202})
	_, err := repo.ResolveAstraRoutingAccounts(t.Context(), value)
	require.ErrorContains(t, err, "astra_ws_outside_targets")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAstraGroupSelectionResolvesSourcesBeforeWSExpansion(t *testing.T) {
	repo, _, mock := newAstraGroupSelectionRepo(t)
	value := astraGroupSelectionSettings()
	value.WSSession = config.CodexWSAnchorConfig{Enabled: true, AccountIDs: []int64{101, 303}}
	expectAstraGroupAccounts(mock, []int64{11}, []int64{101})
	resolved, err := repo.ResolveAstraRoutingAccounts(t.Context(), value)
	require.NoError(t, err)
	require.Equal(t, []int64{202, 303}, resolved.CookiePool.TargetAccountIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAstraGroupSelectionDoesNotReusePreviewOnDatabaseFailure(t *testing.T) {
	for _, stage := range []string{"groups", "members", "rows"} {
		t.Run(stage, func(t *testing.T) {
			repo, _, mock := newAstraGroupSelectionRepo(t)
			failure := errors.New("database read failed")
			groups := expectAstraSelectionGroups(mock, 11)
			if stage == "groups" {
				groups.WillReturnError(failure)
			} else {
				groups.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11))
				members := mock.ExpectQuery(`astra_group_accounts`).WithArgs(pq.Array([]int64{11}))
				if stage == "members" {
					members.WillReturnError(failure)
				} else {
					members.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(101).RowError(0, failure))
				}
			}
			resolved, err := repo.ResolveAstraRoutingAccounts(t.Context(), astraGroupSelectionSettings())
			require.ErrorIs(t, err, failure)
			require.Empty(t, resolved.CookiePool.SourceAccountIDs)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestAstraGroupSelectionCanDisableUnavailableGroups(t *testing.T) {
	repo, _, mock := newAstraGroupSelectionRepo(t)
	value := astraGroupSelectionSettings()
	value.CookiePool.Enabled = false
	resolved, err := repo.ResolveAstraRoutingAccounts(t.Context(), value)
	require.NoError(t, err)
	require.Empty(t, resolved.CookiePool.SourceAccountIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}
