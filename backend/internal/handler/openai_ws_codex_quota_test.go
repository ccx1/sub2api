package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type openAIWSQuotaAccountRepo struct {
	*openAIWSUsageHandlerAccountRepoStub
	latest atomic.Pointer[service.Account]
}

func (r *openAIWSQuotaAccountRepo) GetByID(ctx context.Context, id int64) (*service.Account, error) {
	if latest := r.latest.Load(); latest != nil && latest.ID == id {
		copy := *latest
		return &copy, nil
	}
	return r.openAIWSUsageHandlerAccountRepoStub.GetByID(ctx, id)
}

func newOpenAIWSQuotaClient(t *testing.T, upstreamURL string) (*coderws.Conn, *openAIWSQuotaAccountRepo, <-chan struct{}) {
	t.Helper()
	account := service.Account{
		ID: 9961, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": upstreamURL},
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
			"openai_apikey_responses_websockets_v2_mode":    service.OpenAIWSIngressModePassthrough,
		},
	}
	repo := &openAIWSQuotaAccountRepo{openAIWSUsageHandlerAccountRepoStub: &openAIWSUsageHandlerAccountRepoStub{account: account}}
	cfg := &config.Config{}
	cfg.RunMode = config.RunModeSimple
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 3
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	usageRepo := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 2)}
	gateway := service.NewOpenAIGatewayService(
		repo, usageRepo, nil, nil, nil, nil, nil, cfg, nil, nil,
		service.NewBillingService(cfg, nil), nil, billing, nil, &service.DeferredService{},
		nil, nil, nil, nil, nil, nil, nil,
	)
	t.Cleanup(gateway.CloseOpenAIWSPool)
	concurrency := &concurrencyCacheMock{
		acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
	}
	h := &OpenAIGatewayHandler{
		cfg: cfg, gatewayService: gateway, billingCacheService: billing,
		apiKeyService:     &service.APIKeyService{},
		concurrencyHelper: NewConcurrencyHelper(service.NewConcurrencyService(concurrency), SSEPingFormatNone, time.Second),
	}
	groupID := int64(4361)
	apiKey := &service.APIKey{
		ID: 1861, GroupID: &groupID,
		User: &service.User{ID: 1761, Status: service.StatusActive},
	}
	done := make(chan struct{})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})
		c.Next()
	})
	router.GET("/openai/v1/responses", func(c *gin.Context) {
		defer close(done)
		h.ResponsesWebSocket(c)
	})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/openai/v1/responses", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.CloseNow() })
	return client, repo, done
}

func TestOpenAIResponsesWebSocketSecondTurnRechecksCodexQuota(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name    string
		window  string
		used    float64
		blocked bool
	}{
		{name: "5h exhausted", window: "5h", used: 100, blocked: true},
		{name: "7d exhausted", window: "7d", used: 100, blocked: true},
		{name: "below exhaustion", window: "5h", used: 99},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var upstreamRequests atomic.Int32
			upstreamDone := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(upstreamDone)
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					t.Errorf("accept upstream: %v", err)
					return
				}
				defer conn.CloseNow()
				ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
				defer cancel()
				for {
					if _, _, err := conn.Read(ctx); err != nil {
						return
					}
					upstreamRequests.Add(1)
					event := []byte(`{"type":"response.completed","response":{"id":"resp_quota","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)
					if err := conn.Write(ctx, coderws.MessageText, event); err != nil {
						return
					}
				}
			}))
			t.Cleanup(upstream.Close)
			client, repo, handlerDone := newOpenAIWSQuotaClient(t, upstream.URL)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			payload := []byte(`{"type":"response.create","model":"gpt-5.1","input":"first"}`)
			require.NoError(t, client.Write(ctx, coderws.MessageText, payload))
			_, first, err := client.Read(ctx)
			require.NoError(t, err)
			require.Equal(t, "response.completed", gjson.GetBytes(first, "type").String())

			// 长连接仍持有首轮账号副本，仓库中的新用量必须在第二轮准入时生效。
			latest := repo.account
			latest.Extra = make(map[string]any, len(repo.account.Extra)+3)
			for key, value := range repo.account.Extra {
				latest.Extra[key] = value
			}
			latest.Extra["codex_"+tt.window+"_used_percent"] = tt.used
			latest.Extra["codex_"+tt.window+"_reset_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
			latest.Extra["codex_usage_updated_at"] = time.Now().UTC().Format(time.RFC3339)
			repo.latest.Store(&latest)
			require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":"follow-up"}`)))
			_, second, err := client.Read(ctx)
			if tt.blocked {
				var closeErr coderws.CloseError
				require.ErrorAs(t, err, &closeErr)
				require.Equal(t, coderws.StatusTryAgainLater, closeErr.Code)
				require.Equal(t, "account is no longer schedulable, please reconnect", closeErr.Reason)
			} else {
				require.NoError(t, err)
				require.Equal(t, "response.completed", gjson.GetBytes(second, "type").String())
				require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
			}
			select {
			case <-handlerDone:
			case <-ctx.Done():
				t.Fatal("websocket handler did not exit")
			}
			select {
			case <-upstreamDone:
			case <-ctx.Done():
				t.Fatal("upstream websocket did not exit")
			}
			wantRequests := int32(2)
			if tt.blocked {
				wantRequests = 1
			}
			require.Equal(t, wantRequests, upstreamRequests.Load())
		})
	}
}
