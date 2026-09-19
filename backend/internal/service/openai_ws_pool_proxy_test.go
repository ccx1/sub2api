package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOpenAIWSConnPoolDoesNotReuseDifferentProxy(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	pool := newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(&openAIWSFakeDialer{})
	t.Cleanup(pool.Close)
	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1}
	req := openAIWSAcquireRequest{Account: account, WSURL: "wss://example.com/responses", Headers: http.Header{}, ProxyURL: "http://proxy-a.example:8080"}
	a, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	idA := a.ConnID()
	a.Release()
	reused, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, idA, reused.ConnID())
	reused.Release()
	req.ProxyURL = "http://proxy-b.example:8080"
	b, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	require.NotEqual(t, idA, b.ConnID())
	idB := b.ConnID()
	b.Release()
	req.ProxyURL = ""
	direct, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	require.NotEqual(t, idB, direct.ConnID())
	direct.Release()
	req.ProxyURL = "http://proxy-b.example:8080"
	recovered, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	require.NotEqual(t, direct.ConnID(), recovered.ConnID())
	recovered.Release()
}
