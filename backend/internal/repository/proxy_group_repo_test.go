package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func newProxyGroupRepositoryTest(t *testing.T) (*proxyRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()); _ = client.Close() })
	return newProxyRepositoryWithSQL(client, db), mock
}

func TestProxyGroupCreateRenameAndConflicts(t *testing.T) {
	r, mock := newProxyGroupRepositoryTest(t)
	now := time.Now()
	mock.ExpectQuery("INSERT INTO proxy_groups").WithArgs("Asia").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at"}).AddRow(1, "Asia", now, now))
	group, err := r.CreateProxyGroup(context.Background(), "Asia")
	require.NoError(t, err)
	require.Equal(t, "Asia", group.Name)
	mock.ExpectQuery("UPDATE proxy_groups").WithArgs(int64(1), "Europe").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at"}).AddRow(1, "Europe", now, now))
	group, err = r.UpdateProxyGroup(context.Background(), 1, "Europe")
	require.NoError(t, err)
	require.Equal(t, "Europe", group.Name)
	mock.ExpectQuery("INSERT INTO proxy_groups").WithArgs("Europe").WillReturnError(&pq.Error{Code: "23505"})
	_, err = r.CreateProxyGroup(context.Background(), "Europe")
	require.ErrorIs(t, err, service.ErrProxyGroupDuplicate)
	mock.ExpectQuery("UPDATE proxy_groups").WithArgs(int64(9), "Absent").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at"}))
	_, err = r.UpdateProxyGroup(context.Background(), 9, "Absent")
	require.ErrorIs(t, err, service.ErrProxyGroupNotFound)
}

func TestProxyGroupListCounts(t *testing.T) {
	r, mock := newProxyGroupRepositoryTest(t)
	now := time.Now()
	mock.ExpectQuery("SELECT g.id, g.name").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "count", "active"}).AddRow(1, "Asia", now, now, 3, 2).AddRow(2, "Empty", now, now, 0, 0))
	groups, err := r.ListProxyGroups(context.Background())
	require.NoError(t, err)
	require.Len(t, groups, 2)
	require.EqualValues(t, 3, groups[0].ProxyCount)
	require.EqualValues(t, 2, groups[0].ActiveProxyCount)
	require.Zero(t, groups[1].ProxyCount)
}

func TestProxyGroupSingleProxyReturnsMembershipAndName(t *testing.T) {
	r, mock := newProxyGroupRepositoryTest(t)
	mock.ExpectQuery(`SELECT .* FROM "proxies"`).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "group_id"}).AddRow(9, "fixed proxy", 1))
	mock.ExpectQuery(`SELECT .* FROM "proxy_groups"`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(1, "Asia"))
	proxy, err := r.GetByID(context.Background(), 9)
	require.NoError(t, err)
	require.NotNil(t, proxy.GroupID)
	require.EqualValues(t, 1, *proxy.GroupID)
	require.Equal(t, "Asia", proxy.GroupName)
}

func TestProxyGroupCreateProxyPersistsMembership(t *testing.T) {
	r, mock := newProxyGroupRepositoryTest(t)
	groupID := int64(1)
	mock.ExpectQuery(`INSERT INTO "proxies".*"group_id"`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(9))
	mock.ExpectQuery(`SELECT .* FROM "proxy_groups"`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(1, "Asia"))
	proxy := &service.Proxy{Name: "proxy", Protocol: "http", Host: "example.test", Port: 8080, Status: service.StatusActive, GroupID: &groupID}
	require.NoError(t, r.Create(context.Background(), proxy))
	require.EqualValues(t, 9, proxy.ID)
	require.EqualValues(t, 1, *proxy.GroupID)
	require.Equal(t, "Asia", proxy.GroupName)
}

func TestProxyGroupDeletePreservesMembersAndRejectsAccountReferences(t *testing.T) {
	for _, inUse := range []bool{false, true} {
		t.Run(map[bool]string{false: "detach", true: "referenced"}[inUse], func(t *testing.T) {
			r, mock := newProxyGroupRepositoryTest(t)
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT id FROM proxy_groups .* FOR UPDATE").WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
			mock.ExpectQuery("(?s)SELECT EXISTS.*FROM accounts.*proxy_mode.*random.*random_proxy_pool_scope.*group").WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(inUse))
			if inUse {
				mock.ExpectRollback()
			} else {
				mock.ExpectExec("UPDATE proxies SET group_id=NULL").WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 2))
				mock.ExpectExec("DELETE FROM proxy_groups").WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			}
			err := r.DeleteProxyGroup(context.Background(), 1)
			if inUse {
				require.ErrorIs(t, err, service.ErrProxyGroupInUse)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestProxyGroupAssignIsAtomic(t *testing.T) {
	for _, scenario := range []string{"success", "missing", "write_failure", "clear"} {
		t.Run(scenario, func(t *testing.T) {
			r, mock := newProxyGroupRepositoryTest(t)
			group := int64(1)
			groupID := &group
			mock.ExpectBegin()
			if scenario == "clear" {
				groupID = nil
			} else {
				mock.ExpectQuery("SELECT id FROM proxy_groups .* FOR UPDATE").WithArgs(group).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(group))
			}
			rows := sqlmock.NewRows([]string{"id"}).AddRow(2)
			if scenario != "missing" {
				rows.AddRow(3)
			}
			mock.ExpectQuery("SELECT id FROM proxies .* FOR UPDATE").WithArgs("{2,3}").WillReturnRows(rows)
			if scenario == "missing" {
				mock.ExpectRollback()
			} else {
				update := mock.ExpectExec("UPDATE proxies SET group_id=\\$2").WithArgs("{2,3}", groupID)
				if scenario == "write_failure" {
					update.WillReturnError(errors.New("failed write"))
					mock.ExpectRollback()
				} else {
					update.WillReturnResult(sqlmock.NewResult(0, 2))
					mock.ExpectCommit()
				}
			}
			count, err := r.AssignProxyGroup(context.Background(), []int64{2, 3}, groupID)
			if scenario == "missing" {
				require.ErrorIs(t, err, service.ErrProxyNotFound)
			} else if scenario == "write_failure" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, 2, count)
			}
		})
	}
}

func TestProxyGroupFilterAppliesToCountAndPage(t *testing.T) {
	for _, groupID := range []int64{0, 2} {
		r, mock := newProxyGroupRepositoryTest(t)
		predicate := regexp.QuoteMeta(`"proxies"."group_id" IS NULL`)
		if groupID > 0 {
			predicate = regexp.QuoteMeta(`"proxies"."group_id" = $1`)
		}
		mock.ExpectQuery(`SELECT COUNT.*` + predicate).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(`SELECT .*` + predicate).WillReturnRows(sqlmock.NewRows([]string{"id"}))
		result, page, err := r.ListWithFilters(service.WithProxyGroupFilter(context.Background(), groupID), pagination.PaginationParams{Page: 1, PageSize: 20}, "", "", "")
		require.NoError(t, err)
		require.Empty(t, result)
		require.Zero(t, page.Total)
	}
}
