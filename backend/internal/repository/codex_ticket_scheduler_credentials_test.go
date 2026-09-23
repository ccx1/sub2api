package repository

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type cookieSchedulerAccountRepo struct {
	service.AccountRepository
	*ProxyPoolAllocator
	mu       sync.Mutex
	account  *service.Account
	saved    chan json.RawMessage
	finished chan service.CodexTicketFinishRequest
}

func (r *cookieSchedulerAccountRepo) ListByPlatform(context.Context, string) ([]service.Account, error) {
	return nil, nil
}

func (r *cookieSchedulerAccountRepo) GetByID(context.Context, int64) (*service.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account := *r.account
	account.Extra, account.Credentials = maps.Clone(r.account.Extra), maps.Clone(r.account.Credentials)
	return &account, nil
}

func (r *cookieSchedulerAccountRepo) GetCodexTicketAccountSnapshot(ctx context.Context, id int64) (*service.Account, error) {
	return r.GetByID(ctx, id)
}

func (r *cookieSchedulerAccountRepo) CompareAndSwapCodexTicket(_ context.Context, account *service.Account, model string, value any) (bool, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return false, err
	}
	r.mu.Lock()
	r.account.Extra["codex_turn_ticket:"+model] = json.RawMessage(payload)
	r.mu.Unlock()
	r.saved <- payload
	return true, nil
}

func (r *cookieSchedulerAccountRepo) RecordCodexTicketAttempt(context.Context, int64, service.CodexTicketAttempt) error {
	return nil
}

func (r *cookieSchedulerAccountRepo) FinishCodexTicket(ctx context.Context, request service.CodexTicketFinishRequest) error {
	err := r.ProxyPoolAllocator.FinishCodexTicket(ctx, request)
	r.finished <- request
	return err
}

type cookieSchedulerUpstream struct {
	requests chan http.Header
	state    string
}

func (u *cookieSchedulerUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.requests <- req.Header.Clone()
	header := http.Header{"Set-Cookie": {"session=private-cookie; Path=/backend-api; Secure; Max-Age=60"}}
	if u.state != "" {
		header.Set("X-Codex-Turn-State", u.state)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: header,
		Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-6-astra\",\"status\":\"completed\"}}\n\n"))}, nil
}

func (u *cookieSchedulerUpstream) DoWithTLS(req *http.Request, proxy string, account int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, account, concurrency)
}

func TestCodexSchedulerCookieCredentialsCompleteRealReservationAndPublish(t *testing.T) {
	for _, test := range []struct{ name, global, account string }{
		{"global_cookie", "cookie", ""}, {"global_cookie_state", "cookie_state", ""},
		{"account_cookie", "state", "cookie"}, {"account_cookie_state", "state", "cookie_state"},
	} {
		t.Run(test.name, func(t *testing.T) {
			allocator, _ := newProxyPoolAllocatorTest(t, 0)
			cfg := codexSchedulerConfig()
			cfg.CredentialMode, cfg.Models, cfg.FailClosed = test.global, []string{"gpt-6-astra"}, true
			allocator.settings = &codexSchedulerSettingsStub{cfg: cfg}
			account := &service.Account{ID: 41, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true,
				Credentials: map[string]any{"access_token": "test", "chatgpt_account_id": "test-account"},
				Extra:       map[string]any{service.CodexTicketProxyModeExtraKey: "account"}}
			mode := test.global
			if test.account != "" {
				mode = test.account
				account.Extra[service.CodexTicketCredentialPolicyExtraKey] = map[string]any{"mode": mode, "ttl_seconds": 12, "refresh_before_seconds": 2}
			}
			repo := &cookieSchedulerAccountRepo{ProxyPoolAllocator: allocator, account: account, saved: make(chan json.RawMessage, 1), finished: make(chan service.CodexTicketFinishRequest, 1)}
			upstream := &cookieSchedulerUpstream{requests: make(chan http.Header, 2)}
			if mode == "cookie_state" {
				upstream.state = "gAAAAA" + strings.Repeat("A", 286)
			}
			gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil,
				&config.Config{Gateway: config.GatewayConfig{OpenAICodexTicket: cfg}}, nil, nil, nil, nil, nil,
				upstream, nil, nil, nil, nil, nil, nil, nil, nil)
			t.Cleanup(gateway.StopOpenAICodexTicketHarvester)
			result, err := gateway.RetryOpenAICodexTicket(context.Background(), 41, "gpt-6-astra")
			require.NoError(t, err)
			require.Equal(t, 1, result.Scheduled)
			select {
			case payload := <-repo.saved:
				var ticket struct {
					CredentialMode, State               string
					CapturedAt, ExpiresAt, RevalidateAt time.Time
					Cookies                             []*http.Cookie
				}
				var raw map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(payload, &raw))
				require.NoError(t, json.Unmarshal(raw["credential_mode"], &ticket.CredentialMode))
				require.NoError(t, json.Unmarshal(raw["cookies"], &ticket.Cookies))
				require.NoError(t, json.Unmarshal(raw["captured_at"], &ticket.CapturedAt))
				require.NoError(t, json.Unmarshal(raw["expires_at"], &ticket.ExpiresAt))
				require.NoError(t, json.Unmarshal(raw["revalidate_at"], &ticket.RevalidateAt))
				require.Equal(t, mode, ticket.CredentialMode)
				require.Len(t, ticket.Cookies, 1)
				ttl := 20 * time.Second
				if test.account != "" {
					ttl = 12 * time.Second
				}
				// 配置 TTL 决定复验时间，上游 Max-Age 保持实际硬期限。
				require.Equal(t, ttl, ticket.RevalidateAt.Sub(ticket.CapturedAt))
				require.Equal(t, time.Minute, ticket.ExpiresAt.Sub(ticket.CapturedAt))
				require.Equal(t, ticket.Cookies[0].Expires, ticket.ExpiresAt)
			case <-time.After(5 * time.Second):
				t.Fatal("Cookie harvest did not publish through the shared scheduler")
			}
			harvest, business := <-upstream.requests, <-upstream.requests
			require.Empty(t, harvest.Get("Cookie"))
			require.Equal(t, "session=private-cookie", business.Get("Cookie"))
			require.Equal(t, upstream.state, business.Get("X-Codex-Turn-State"))
			select {
			case finished := <-repo.finished:
				require.Equal(t, "success", finished.Outcome)
				require.Equal(t, cfg, finished.Reservation.Config)
			case <-time.After(5 * time.Second):
				t.Fatal("Cookie reservation did not finish")
			}
		})
	}
}
