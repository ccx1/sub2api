//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type sharedAdmissionHandlerAccountRepo struct {
	service.AccountRepository
	selected service.Account
	latest   service.Account
	group    *service.Group
	terms    service.SharedPoolSettlementTerms
	reads    int
}

func (r *sharedAdmissionHandlerAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	if platform != r.selected.Platform {
		return nil, nil
	}
	return []service.Account{r.selected}, nil
}

func (r *sharedAdmissionHandlerAccountRepo) ListSchedulableByGroupIDAndPlatform(ctx context.Context, _ int64, platform string) ([]service.Account, error) {
	return r.ListSchedulableByPlatform(ctx, platform)
}

func (r *sharedAdmissionHandlerAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	r.reads++
	if id != r.latest.ID {
		return nil, service.ErrAccountNotFound
	}
	return &r.latest, nil
}

func (r *sharedAdmissionHandlerAccountRepo) SharedPoolSettlementTerms(_ context.Context, ownerID int64) (*service.SharedPoolSettlementTerms, error) {
	if ownerID != 71 {
		return nil, service.ErrSharedPoolBillingInvalid
	}
	return &r.terms, nil
}

func (r *sharedAdmissionHandlerAccountRepo) SharedPoolDispatchGroup(_ context.Context, id int64) (*service.Group, error) {
	if id != r.group.ID {
		return nil, service.ErrGroupNotFound
	}
	return r.group, nil
}

type sharedAdmissionHandlerBillingRepo struct {
	service.UsageBillingRepository
	commands []*service.UsageBillingCommand
}

func (r *sharedAdmissionHandlerBillingRepo) Apply(_ context.Context, cmd *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	copy := *cmd
	r.commands = append(r.commands, &copy)
	// 截取真实扣费命令，模拟已处理的幂等结果，避免触发无关缓存与通知。
	return &service.UsageBillingApplyResult{}, nil
}

type sharedAdmissionHandlerUsageRepo struct {
	service.UsageLogRepository
	logs []*service.UsageLog
}

func (r *sharedAdmissionHandlerUsageRepo) Create(_ context.Context, log *service.UsageLog) (bool, error) {
	r.logs = append(r.logs, log)
	return true, nil
}

type sharedAdmissionHandlerUpstream struct {
	service.HTTPUpstream
	repo          *sharedAdmissionHandlerAccountRepo
	authorization []string
}

func (u *sharedAdmissionHandlerUpstream) Do(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.authorization = append(u.authorization, request.Header.Get("Authorization"))
	// 请求已准入后更改数据库替身和分组对象，后续扣费必须沿用本次冻结的条款。
	u.repo.group.RateMultiplier = 9
	u.repo.terms.Multiplier, u.repo.terms.PlatformRateBPS, u.repo.terms.ProxyRateBPS = 0.9, 900, 150
	body := `{"id":"resp_snapshot","object":"response","model":"gpt-5.2","status":"completed","output":[],"usage":{"input_tokens":1000,"output_tokens":100}}`
	if strings.HasSuffix(request.URL.Path, "/embeddings") {
		body = `{"object":"list","model":"gpt-5.2","data":[{"object":"embedding","index":0,"embedding":[0.1]}],"usage":{"prompt_tokens":1000,"total_tokens":1000}}`
	}
	return &http.Response{StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"shared-snapshot-request"}},
		Body:   io.NopCloser(strings.NewReader(body))}, nil
}

type sharedAdmissionHandlerFixture struct {
	handler  *OpenAIGatewayHandler
	accounts *sharedAdmissionHandlerAccountRepo
	billing  *sharedAdmissionHandlerBillingRepo
	usage    *sharedAdmissionHandlerUsageRepo
	upstream *sharedAdmissionHandlerUpstream
	key      *service.APIKey
}

func newSharedAdmissionHandlerFixture(t *testing.T) sharedAdmissionHandlerFixture {
	t.Helper()
	group := &service.Group{ID: 73, Platform: service.PlatformOpenAI, Status: service.StatusActive,
		SubscriptionType: service.SubscriptionTypeStandard, Hydrated: true, RateMultiplier: 2}
	selected := service.Account{ID: 75, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 2, GroupIDs: []int64{group.ID},
		Credentials: map[string]any{"api_key": "fixture-stale", "base_url": "https://api.openai.com"},
		Extra: map[string]any{"openai_passthrough": true, service.SharedPoolOwnerKey: int64(71),
			service.SharedPoolEnabledKey: true, service.SharedPoolDispatchConsentKey: true}}
	latest := selected
	latest.Credentials = map[string]any{"api_key": "fixture-latest", "base_url": "https://api.openai.com"}
	repo := &sharedAdmissionHandlerAccountRepo{selected: selected, latest: latest, group: group,
		terms: service.SharedPoolSettlementTerms{Multiplier: 0.5, PlatformRateBPS: 500, ProxyRateBPS: 100}}
	usage, billing := &sharedAdmissionHandlerUsageRepo{}, &sharedAdmissionHandlerBillingRepo{}
	upstream := &sharedAdmissionHandlerUpstream{repo: repo}
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	// 只跳过余额预检查；实际转发服务使用正常模式并生成真实 UsageBillingCommand。
	eligibility := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{RunMode: config.RunModeSimple}, nil)
	t.Cleanup(eligibility.Stop)
	gateway := service.NewOpenAIGatewayService(repo, usage, billing, nil, nil, nil, nil, cfg, nil, nil,
		service.NewBillingService(cfg, nil), nil, eligibility, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
	t.Cleanup(gateway.StopOpenAICodexTicketHarvester)
	slots := &concurrencyCacheMock{
		acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
	}
	h := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(slots), eligibility, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
	key := &service.APIKey{ID: 77, UserID: 79, GroupID: &group.ID, Group: group,
		User: &service.User{ID: 79, Status: service.StatusActive, Balance: 100}}
	return sharedAdmissionHandlerFixture{h, repo, billing, usage, upstream, key}
}

func (f sharedAdmissionHandlerFixture) request(path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	ctx := context.WithValue(context.Background(), ctxkey.Group, f.key.Group)
	ctx = context.WithValue(ctx, ctxkey.UserID, f.key.UserID)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware.ContextKeyAPIKey), f.key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: f.key.UserID, Concurrency: 2})
	return c, w
}

func TestSharedPoolAdmissionSnapshotReachesHandlerBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name string
		path string
		body string
		run  func(*OpenAIGatewayHandler, *gin.Context)
	}{
		{"responses", "/v1/responses", `{"model":"gpt-5.2","stream":false,"input":"hello"}`, (*OpenAIGatewayHandler).Responses},
		{"embeddings", "/v1/embeddings", `{"model":"gpt-5.2","input":"hello"}`, (*OpenAIGatewayHandler).Embeddings},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSharedAdmissionHandlerFixture(t)
			c, w := f.request(tc.path, tc.body)
			tc.run(f.handler, c)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Equal(t, []string{"Bearer fixture-latest"}, f.upstream.authorization, "转发必须使用准入重读的凭证")
			require.Positive(t, f.accounts.reads)
			require.Nil(t, f.accounts.selected.SharedPoolSettlement)
			require.Nil(t, f.accounts.latest.SharedPoolSettlement, "请求快照不得写回共享仓储对象")
			require.Len(t, f.billing.commands, 1, "真实 handler 必须把请求交给统一扣费链路")
			cmd := f.billing.commands[0]
			require.EqualValues(t, 71, cmd.SharedPoolOwnerID)
			require.EqualValues(t, 73, cmd.GroupID)
			require.False(t, cmd.SharedPoolGroup, "普通分组内的已授权共享账号也必须保留独立结算")
			require.Equal(t, new(0.5), cmd.SharedPoolSettlementMultiplier)
			require.Equal(t, new(500), cmd.SharedPoolPlatformRateBPS)
			require.Equal(t, new(100), cmd.SharedPoolProxyRateBPS)
			require.Positive(t, cmd.SharedPoolBaseCost)
			require.InDelta(t, cmd.SharedPoolBaseCost*2, cmd.BalanceCost, 0.00000001)
			require.Len(t, f.usage.logs, 1)
			require.Equal(t, 2.0, f.usage.logs[0].RateMultiplier, "请求期间分组改为9倍不能改写准入时的2倍售价")
			require.Equal(t, 9.0, f.accounts.group.RateMultiplier)
			require.Equal(t, 0.9, f.accounts.terms.Multiplier)
		})
	}
}
