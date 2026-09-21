package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProxyGroupCreateIgnoresMetadataReadFailureAfterSave(t *testing.T) {
	for _, missing := range []bool{true, false} {
		t.Run(map[bool]string{true: "group_removed", false: "read_failure"}[missing], func(t *testing.T) {
			r, mock := newProxyGroupRepositoryTest(t)
			groupID := int64(1)
			mock.ExpectQuery(`INSERT INTO "proxies"`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(9))
			read := mock.ExpectQuery(`SELECT .* FROM "proxy_groups"`)
			if missing {
				read.WillReturnRows(sqlmock.NewRows([]string{"id", "name"}))
			} else {
				read.WillReturnError(errors.New("metadata unavailable"))
			}
			proxy := &service.Proxy{Name: "proxy", Protocol: "http", Host: "proxy.test", Port: 8080, Status: service.StatusActive, GroupID: &groupID}
			require.NoError(t, r.Create(context.Background(), proxy))
			require.EqualValues(t, 9, proxy.ID)
			require.Empty(t, proxy.GroupName)
			if missing {
				require.Nil(t, proxy.GroupID)
			} else {
				require.Equal(t, &groupID, proxy.GroupID)
			}
		})
	}
}
