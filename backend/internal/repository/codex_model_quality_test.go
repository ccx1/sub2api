package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newCodexModelQualityRepository(t *testing.T) (*accountRepository, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return &accountRepository{proxyPool: &ProxyPoolAllocator{rdb: client}}, server
}

func codexModelQualityTestRecord(t *testing.T, scope string) *service.CodexModelQualityRecord {
	t.Helper()
	var record service.CodexModelQualityRecord
	require.NoError(t, json.Unmarshal([]byte(`{"status":{"model":"astra","status":"passed","reason":"checks_passed"},"policy":"policy-1"}`), &record))
	record.Scope = scope
	return &record
}

func TestCodexModelQualityLeaseIsSharedAndIsolatedByModel(t *testing.T) {
	repo, server := newCodexModelQualityRepository(t)
	peerClient := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = peerClient.Close() })
	peer := &accountRepository{proxyPool: &ProxyPoolAllocator{rdb: peerClient}}
	ctx := context.Background()
	acquired, err := repo.AcquireCodexModelQuality(ctx, 7, "astra", "first", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
	acquired, err = peer.AcquireCodexModelQuality(ctx, 7, " astra ", "second", time.Minute)
	require.NoError(t, err)
	require.False(t, acquired)
	for _, scope := range []struct {
		account int64
		model   string
	}{{7, "sol"}, {8, "astra"}} {
		acquired, err = peer.AcquireCodexModelQuality(ctx, scope.account, scope.model, "second", time.Minute)
		require.NoError(t, err)
		require.True(t, acquired)
	}
	require.NoError(t, peer.ReleaseCodexModelQuality(ctx, 7, "astra", "second"))
	acquired, err = peer.AcquireCodexModelQuality(ctx, 7, "astra", "second", time.Minute)
	require.NoError(t, err)
	require.False(t, acquired, "another owner cannot release the lease")
	require.NoError(t, repo.ReleaseCodexModelQuality(ctx, 7, "astra", "first"))
	acquired, err = peer.AcquireCodexModelQuality(ctx, 7, "astra", "second", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
}

func TestCodexModelQualitySaveRequiresLiveOwner(t *testing.T) {
	repo, server := newCodexModelQualityRepository(t)
	ctx := context.Background()
	record := codexModelQualityTestRecord(t, "scope-old")
	saved, err := repo.SaveCodexModelQuality(ctx, 7, "astra", "old", record, 24*time.Hour)
	require.NoError(t, err)
	require.False(t, saved, "saving without a lease must fail")
	acquired, err := repo.AcquireCodexModelQuality(ctx, 7, "astra", "old", time.Second)
	require.NoError(t, err)
	require.True(t, acquired)
	saved, err = repo.SaveCodexModelQuality(ctx, 7, "astra", "outsider", record, 24*time.Hour)
	require.NoError(t, err)
	require.False(t, saved)
	server.FastForward(time.Second)
	saved, err = repo.SaveCodexModelQuality(ctx, 7, "astra", "old", record, 24*time.Hour)
	require.NoError(t, err)
	require.False(t, saved, "an expired lease cannot publish a result")
	acquired, err = repo.AcquireCodexModelQuality(ctx, 7, "astra", "new", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
	newRecord := codexModelQualityTestRecord(t, "scope-new")
	pausedUntil := time.Now().UTC().Add(15 * time.Minute)
	newRecord.ConsecutiveLowQuality, newRecord.LastLowQualityTicket = 3, "failed-ticket-hash"
	newRecord.QualityPausedUntil = &pausedUntil
	saved, err = repo.SaveCodexModelQuality(ctx, 7, "astra", "new", newRecord, 24*time.Hour)
	require.NoError(t, err)
	require.True(t, saved)
	saved, err = repo.SaveCodexModelQuality(ctx, 7, "astra", "old", record, 24*time.Hour)
	require.NoError(t, err)
	require.False(t, saved, "a late result cannot replace the new owner's result")
	require.NoError(t, repo.ReleaseCodexModelQuality(ctx, 7, "astra", "old"))
	_, keys, err := repo.codexModelQualityRedis(7, "astra")
	require.NoError(t, err)
	owner, err := server.Get(keys[0])
	require.NoError(t, err)
	require.Equal(t, "new", owner)
	loaded, err := repo.LoadCodexModelQuality(ctx, 7, "astra")
	require.NoError(t, err)
	require.Equal(t, newRecord, loaded)
	for _, scope := range []struct {
		account int64
		model   string
	}{{8, "astra"}, {7, "sol"}} {
		other, err := repo.LoadCodexModelQuality(ctx, scope.account, scope.model)
		require.NoError(t, err)
		require.Nil(t, other, "quality cooldown is isolated by account and model")
	}
}

func TestCodexModelQualityRecordRoundTripAndExpiry(t *testing.T) {
	repo, server := newCodexModelQualityRepository(t)
	ctx := context.Background()
	loaded, err := repo.LoadCodexModelQuality(ctx, 7, "astra")
	require.NoError(t, err)
	require.Nil(t, loaded)
	record := codexModelQualityTestRecord(t, "scope-1")
	acquired, err := repo.AcquireCodexModelQuality(ctx, 7, "astra", "owner", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
	saved, err := repo.SaveCodexModelQuality(ctx, 7, "astra", "owner", record, 24*time.Hour)
	require.NoError(t, err)
	require.True(t, saved)
	require.NoError(t, repo.ReleaseCodexModelQuality(ctx, 7, "astra", "owner"))
	loaded, err = repo.LoadCodexModelQuality(ctx, 7, "astra")
	require.NoError(t, err)
	require.Equal(t, record, loaded)
	_, keys, err := repo.codexModelQualityRedis(7, "astra")
	require.NoError(t, err)
	require.Equal(t, 24*time.Hour, server.TTL(keys[1]))
	payload, err := server.Get(keys[1])
	require.NoError(t, err)
	encoded, err := json.Marshal(record)
	require.NoError(t, err)
	require.JSONEq(t, string(encoded), payload)
	require.NotContains(t, payload, "owner")
	server.FastForward(24 * time.Hour)
	loaded, err = repo.LoadCodexModelQuality(ctx, 7, "astra")
	require.NoError(t, err)
	require.Nil(t, loaded)
}

func TestCodexModelQualityUnavailableStorageFailsClosed(t *testing.T) {
	ctx := context.Background()
	for _, repo := range []*accountRepository{nil, {}, {proxyPool: &ProxyPoolAllocator{}}} {
		_, err := repo.LoadCodexModelQuality(ctx, 7, "astra")
		require.Error(t, err)
		_, err = repo.AcquireCodexModelQuality(ctx, 7, "astra", "owner", time.Minute)
		require.Error(t, err)
		_, err = repo.SaveCodexModelQuality(ctx, 7, "astra", "owner", codexModelQualityTestRecord(t, "scope"), time.Hour)
		require.Error(t, err)
		require.Error(t, repo.ReleaseCodexModelQuality(ctx, 7, "astra", "owner"))
	}
	repo, _ := newCodexModelQualityRepository(t)
	require.NoError(t, repo.proxyPool.rdb.Close())
	_, err := repo.AcquireCodexModelQuality(ctx, 7, "astra", "owner", time.Minute)
	require.Error(t, err)
	_, err = repo.LoadCodexModelQuality(ctx, 7, "astra")
	require.Error(t, err)
}

func TestCodexModelQualityRejectsInvalidInputsAndCorruptRecord(t *testing.T) {
	repo, server := newCodexModelQualityRepository(t)
	ctx := context.Background()
	for _, scope := range []struct {
		account int64
		model   string
	}{{0, "astra"}, {7, " "}} {
		_, err := repo.LoadCodexModelQuality(ctx, scope.account, scope.model)
		require.Error(t, err)
	}
	_, err := repo.AcquireCodexModelQuality(ctx, 7, "astra", " ", time.Minute)
	require.Error(t, err)
	_, err = repo.AcquireCodexModelQuality(ctx, 7, "astra", "owner", time.Nanosecond)
	require.Error(t, err)
	_, err = repo.SaveCodexModelQuality(ctx, 7, "astra", "owner", nil, time.Hour)
	require.Error(t, err)
	_, err = repo.SaveCodexModelQuality(ctx, 7, "astra", "owner", codexModelQualityTestRecord(t, "scope"), 0)
	require.Error(t, err)
	require.Error(t, repo.ReleaseCodexModelQuality(ctx, 7, "astra", ""))
	_, keys, err := repo.codexModelQualityRedis(7, "astra")
	require.NoError(t, err)
	for _, payload := range []string{"{invalid", "null"} {
		require.NoError(t, server.Set(keys[1], payload))
		_, err := repo.LoadCodexModelQuality(ctx, 7, "astra")
		require.Error(t, err)
	}
}

func TestCodexModelQualityReadsCurrentBoundProxyWithoutMutatingPool(t *testing.T) {
	for _, valid := range []bool{true, false} {
		repo, mock := newCodexTicketCASRepo(t)
		allocator, server := newProxyPoolAllocatorTest(t, 0, poolCandidate(1))
		repo.proxyPool = allocator
		ctx := context.Background()
		proxy, err := allocator.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
		require.NoError(t, err)
		before := server.Dump()
		host := proxy.Host
		if !valid {
			host = "different.test"
		}
		mock.ExpectQuery(`SELECT .* FROM "proxies".*"deleted_at" IS NULL`).WithArgs(proxy.ID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "protocol", "host", "port", "status"}).AddRow(proxy.ID, proxy.Protocol, host, proxy.Port, proxy.Status))
		actual, err := repo.GetCodexModelQualityBoundProxy(ctx, 7)
		require.NoError(t, err)
		if valid {
			require.NotNil(t, actual)
			require.Equal(t, proxy.URL(), actual.URL())
		} else {
			require.Nil(t, actual)
		}
		require.Equal(t, before, server.Dump(), "quality lookup must not renew leases, rebind, or clear failures")
		require.NoError(t, mock.ExpectationsWereMet())
	}
}

func TestCodexModelQualityMissingAndCoolingProxyNeverAllocates(t *testing.T) {
	repo, mock := newCodexTicketCASRepo(t)
	allocator, server := newProxyPoolAllocatorTest(t, 0, poolCandidate(1))
	repo.proxyPool = allocator
	ctx := context.Background()
	proxy, err := repo.GetCodexModelQualityBoundProxy(ctx, 7)
	require.NoError(t, err)
	require.Nil(t, proxy)
	selected, err := allocator.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	for _, transport := range []bool{false, true} {
		key, member := proxyPoolFailureKey("7"), "1"
		score := float64(time.Now().Add(time.Hour).Unix())
		if transport {
			key, member = proxyTransportCooldownKey, proxyTransportCooldownMember(selected)
			score *= 1000
		}
		require.NoError(t, allocator.rdb.ZAdd(ctx, key, redis.Z{Member: member, Score: score}).Err())
		before := server.Dump()
		proxy, err = repo.GetCodexModelQualityBoundProxy(ctx, 7)
		require.NoError(t, err)
		require.Nil(t, proxy)
		require.Equal(t, before, server.Dump())
		require.NoError(t, allocator.rdb.Del(ctx, key).Err())
	}
	require.NoError(t, mock.ExpectationsWereMet(), "missing or cooling bindings must not even query proxy rows")
}

func TestCodexModelQualityChangedAffinityVersionIsRejectedWithoutWrites(t *testing.T) {
	repo, mock := newCodexTicketCASRepo(t)
	allocator, server := newProxyPoolAllocatorTest(t, 0, poolCandidate(1))
	repo.proxyPool = allocator
	ctx := context.Background()
	proxy, err := allocator.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	member := "7"
	key := proxyPoolAffinityKey(member)
	require.NoError(t, allocator.rdb.HSet(ctx, key, "version", "stale-version").Err())
	before := server.Dump()
	mock.ExpectQuery(`SELECT .* FROM "proxies".*"deleted_at" IS NULL`).WithArgs(proxy.ID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "protocol", "host", "port", "status"}).AddRow(proxy.ID, proxy.Protocol, proxy.Host, proxy.Port, proxy.Status))
	actual, err := repo.GetCodexModelQualityBoundProxy(ctx, 7)
	require.NoError(t, err)
	require.Nil(t, actual)
	require.Equal(t, before, server.Dump(), "quality lookup must not repair stale affinity")
	require.NoError(t, mock.ExpectationsWereMet())
}
