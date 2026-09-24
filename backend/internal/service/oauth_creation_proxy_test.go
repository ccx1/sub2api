package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

type oauthCreationProxyRepo struct {
	ProxyRepository
	calls []int64
}

func (r *oauthCreationProxyRepo) GetByID(_ context.Context, id int64) (*Proxy, error) {
	r.calls = append(r.calls, id)
	return &Proxy{ID: id, Protocol: "http", Host: "proxy.example", Port: 8080}, nil
}

type oauthCreationOpenAIClient struct {
	OpenAIOAuthClient
	proxyURL string
	calls    int
}

func (c *oauthCreationOpenAIClient) ExchangeCode(_ context.Context, _, _, _, proxyURL, _ string) (*openai.TokenResponse, error) {
	c.proxyURL = proxyURL
	c.calls++
	return &openai.TokenResponse{AccessToken: "access", ExpiresIn: 3600}, nil
}

type oauthCreationGrokClient struct {
	GrokOAuthClient
	proxyURL string
	calls    int
}

func (c *oauthCreationGrokClient) ExchangeCode(_ context.Context, _, _, _, proxyURL, _ string) (*xai.TokenResponse, error) {
	c.proxyURL = proxyURL
	c.calls++
	return &xai.TokenResponse{AccessToken: "access", ExpiresIn: 3600}, nil
}

func TestOAuthCreationProxySelection(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok} {
		for _, tc := range []struct {
			name string
			id   *int64
			want string
		}{
			{"session", nil, "http://session.example:8080"},
			{"direct", new(int64(0)), ""},
			{"fixed", new(int64(3)), "http://proxy.example:8080"},
			{"negative", new(int64(-1)), ""},
		} {
			t.Run(platform+"/"+tc.name, func(t *testing.T) {
				repo := &oauthCreationProxyRepo{}
				proxyURL, calls, err := exchangeOAuthCreationProxy(t, platform, repo, tc.id)
				if tc.name == "negative" {
					require.ErrorContains(t, err, "OAUTH_PROXY_INVALID")
					require.Zero(t, calls)
				} else {
					require.NoError(t, err)
					require.Equal(t, 1, calls)
					require.Equal(t, tc.want, proxyURL)
				}
				if tc.name == "fixed" {
					require.Equal(t, []int64{3}, repo.calls)
				} else {
					require.Empty(t, repo.calls)
				}
			})
		}
	}
}

func exchangeOAuthCreationProxy(t *testing.T, platform string, repo ProxyRepository, id *int64) (string, int, error) {
	t.Helper()
	if platform == PlatformOpenAI {
		client := &oauthCreationOpenAIClient{}
		svc := NewOpenAIOAuthService(repo, client)
		t.Cleanup(svc.Stop)
		svc.sessionStore.Set("session", &openai.OAuthSession{
			State: "state", ProxyURL: "http://session.example:8080", CreatedAt: time.Now(),
		})
		_, err := svc.ExchangeCode(context.Background(), &OpenAIExchangeCodeInput{
			SessionID: "session", State: "state", Code: "code", ProxyID: id,
		})
		return client.proxyURL, client.calls, err
	}
	client := &oauthCreationGrokClient{}
	svc := NewGrokOAuthService(repo, client)
	t.Cleanup(svc.Stop)
	svc.sessionStore.Set("session", &xai.OAuthSession{
		State: "state", ProxyURL: "http://session.example:8080", CreatedAt: time.Now(),
	})
	_, err := svc.ExchangeCode(context.Background(), &GrokExchangeCodeInput{
		SessionID: "session", State: "state", Code: "code", ProxyID: id,
	})
	return client.proxyURL, client.calls, err
}
