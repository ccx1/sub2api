package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProxyRegionPartialWritesValidateLatestLockedConfiguration(t *testing.T) {
	for _, bulk := range []bool{false, true} {
		t.Run(map[bool]string{false: "extra", true: "bulk"}[bulk], func(t *testing.T) {
			repo, mock := newCodexTicketCASRepo(t)
			updates := map[string]any{service.ProxyRegionModeExtraKey: "manual"}
			ctx := service.WithAccountProxyRegionWrite(context.Background(), updates)
			mock.ExpectBegin()
			// 管理员校验之后，另一个请求已清空旧国家；写入必须以锁内最新数据为准。
			mock.ExpectQuery("SELECT extra FROM accounts.*FOR NO KEY UPDATE").WithArgs(sqlmock.AnyArg()).
				WillReturnRows(sqlmock.NewRows([]string{"extra"}).AddRow([]byte(`{"proxy_region_mode":"off","proxy_region_country":null}`)))
			mock.ExpectRollback()
			var err error
			if bulk {
				_, err = repo.BulkUpdate(ctx, []int64{41}, service.AccountBulkUpdate{Extra: updates})
			} else {
				err = repo.UpdateExtra(ctx, 41, updates)
			}
			require.ErrorContains(t, err, "两位国家代码")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestProxyRegionFullWriteValidatesPreservedCountry(t *testing.T) {
	repo, mock := newCodexTicketCASRepo(t)
	account := codexTicketCASAccount()
	account.Extra = map[string]any{service.ProxyRegionModeExtraKey: "manual", service.ProxyRegionCountryExtraKey: "PH"}
	ctx := service.WithAccountProxyRegionWrite(context.Background(), map[string]any{service.ProxyRegionModeExtraKey: "manual"})
	mock.ExpectQuery("SELECT extra FROM accounts WHERE id").WithArgs(account.ID).
		WillReturnRows(sqlmock.NewRows([]string{"extra"}).AddRow([]byte(`{"proxy_region_mode":"off","proxy_region_country":null}`)))
	require.ErrorContains(t, preserveLockedAccountProtection(ctx, repo.client, account), "两位国家代码")
	require.NoError(t, mock.ExpectationsWereMet())
}
