package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestTempUnschedCacheCASKeepsNew403(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := &tempUnschedCache{rdb: client}
	ctx := context.Background()
	old := &service.TempUnschedState{UntilUnix: time.Now().Add(time.Minute).Unix(), StatusCode: 401, ErrorMessage: "expired"}
	require.NoError(t, cache.SetTempUnsched(ctx, 41, old))
	newer := &service.TempUnschedState{UntilUnix: old.UntilUnix + 1, StatusCode: 403, ErrorMessage: "subscription required"}
	require.NoError(t, cache.SetTempUnsched(ctx, 41, newer))
	cleared, err := cache.DeleteTempUnschedIfUnchanged(ctx, 41, old)
	require.NoError(t, err)
	require.False(t, cleared)
	current, err := cache.GetTempUnsched(ctx, 41)
	require.NoError(t, err)
	require.Equal(t, newer, current)
	cleared, err = cache.DeleteTempUnschedIfUnchanged(ctx, 41, newer)
	require.NoError(t, err)
	require.True(t, cleared)
	current, err = cache.GetTempUnsched(ctx, 41)
	require.NoError(t, err)
	require.Nil(t, current)
}
