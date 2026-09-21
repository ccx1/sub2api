package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func proxyGroupUpdateMatcher(expected, actual string) error {
	if strings.HasPrefix(expected, "membership=") {
		if !strings.HasPrefix(actual, `UPDATE "proxies" SET `) {
			return fmt.Errorf("expected proxy update: %s", actual)
		}
		writesGroup := strings.Contains(strings.Split(actual, " WHERE ")[0], `"group_id"`)
		if writesGroup != (expected != "membership=preserve") {
			return fmt.Errorf("unexpected membership write in %s", actual)
		}
		if expected == "membership=clear" && !strings.Contains(actual, `"group_id" = NULL`) {
			return fmt.Errorf("expected membership removal in %s", actual)
		}
		if expected == "membership=set" && !strings.Contains(actual, `"group_id" = $`) {
			return fmt.Errorf("expected membership assignment in %s", actual)
		}
		return nil
	}
	return sqlmock.QueryMatcherRegexp.Match(expected, actual)
}

func TestProxyGroupOrdinaryUpdatePreservesConcurrentAssignment(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(proxyGroupUpdateMatcher)))
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	oldGroupID := int64(1)
	stale := &service.Proxy{ID: 9, Name: "renamed", Host: "proxy.test", Protocol: "http", Port: 8080, Status: service.StatusActive, GroupID: &oldGroupID}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT protocol, host, port").WithArgs(int64(9)).WillReturnRows(sqlmock.NewRows([]string{"protocol", "host", "port", "username", "password", "status"}).AddRow("http", "proxy.test", 8080, "", "", service.StatusActive))
	mock.ExpectExec("membership=preserve").WillReturnResult(sqlmock.NewResult(0, 1))
	// 管理员读取旧组 1 后，另一事务已把代理划入组 2。
	mock.ExpectQuery(`SELECT .* FROM "proxies"`).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "group_id"}).AddRow(9, "renamed", 2))
	mock.ExpectQuery(`SELECT .* FROM "proxy_groups"`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(2, "new group"))
	mock.ExpectCommit()
	require.NoError(t, newProxyRepositoryWithSQL(client, db).Update(context.Background(), stale))
	require.NoError(t, mock.ExpectationsWereMet())
	require.EqualValues(t, 2, *stale.GroupID)
	require.Equal(t, "new group", stale.GroupName)
}

func TestProxyGroupExplicitUpdateWritesIDOrNull(t *testing.T) {
	for _, clear := range []bool{false, true} {
		t.Run(fmt.Sprintf("clear=%t", clear), func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(proxyGroupUpdateMatcher)))
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			groupID := int64(3)
			proxy := &service.Proxy{ID: 9, Name: "proxy", Host: "proxy.test", Protocol: "http", Port: 8080, Status: service.StatusActive, GroupID: &groupID, GroupIDSet: true}
			if clear {
				proxy.GroupID = nil
			}
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT protocol, host, port").WithArgs(int64(9)).WillReturnRows(sqlmock.NewRows([]string{"protocol", "host", "port", "username", "password", "status"}).AddRow("http", "proxy.test", 8080, "", "", service.StatusActive))
			mode := "membership=set"
			if clear {
				mode = "membership=clear"
			}
			mock.ExpectExec(mode).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery(`SELECT .* FROM "proxies"`).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "group_id"}).AddRow(9, "proxy", proxy.GroupID))
			if !clear {
				mock.ExpectQuery(`SELECT .* FROM "proxy_groups"`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(3, "target"))
			}
			mock.ExpectCommit()
			require.NoError(t, newProxyRepositoryWithSQL(client, db).Update(context.Background(), proxy))
			require.NoError(t, mock.ExpectationsWereMet())
			require.False(t, proxy.GroupIDSet)
			if clear {
				require.Nil(t, proxy.GroupID)
				require.Empty(t, proxy.GroupName)
			} else {
				require.EqualValues(t, 3, *proxy.GroupID)
				require.Equal(t, "target", proxy.GroupName)
			}
		})
	}
}
