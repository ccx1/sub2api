package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodexModelQualityBoundProxyHonorsIPGuardWithoutWrites(t *testing.T) {
	for _, state := range []string{"cooling", "disabled", "expired", "other_ip", "corrupt"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			candidate := ipGuardCandidate(1, "203.0.113.10")
			a, server := newProxyPoolAllocatorTest(t, 0, candidate)
			repo, mock := newCodexTicketCASRepo(t)
			repo.proxyPool = a
			_, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
			require.NoError(t, err)
			ip, cooldown := candidate.proxy.Host, time.Minute
			if state == "other_ip" {
				ip = "203.0.113.11"
			} else if state == "expired" {
				cooldown = -time.Second
			}
			key := seedProxyIPGuard(t, a, ip, state == "disabled", cooldown)
			if state == "corrupt" {
				require.NoError(t, server.Set(key, "invalid"))
			}
			proxy := candidate.proxy
			mock.ExpectQuery(`SELECT .* FROM "proxies".*"deleted_at" IS NULL`).WithArgs(proxy.ID).
				WillReturnRows(sqlmock.NewRows([]string{"id", "protocol", "host", "port", "status"}).
					AddRow(proxy.ID, proxy.Protocol, proxy.Host, proxy.Port, proxy.Status))
			before := server.Dump()
			actual, err := repo.GetCodexModelQualityBoundProxy(ctx, 7)
			if state == "corrupt" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if state == "expired" || state == "other_ip" {
				require.NotNil(t, actual)
			} else {
				require.Nil(t, actual)
			}
			require.Equal(t, before, server.Dump())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestCodexModelQualityIPGuardRedisFailureFailsClosed(t *testing.T) {
	repo, mock := newCodexTicketCASRepo(t)
	a, _ := newProxyPoolAllocatorTest(t, 0, ipGuardCandidate(1, "203.0.113.10"))
	repo.proxyPool = a
	require.NoError(t, a.rdb.Close())
	proxy, err := repo.GetCodexModelQualityBoundProxy(context.Background(), 7)
	require.Error(t, err)
	require.Nil(t, proxy)
	require.NoError(t, mock.ExpectationsWereMet())
}
