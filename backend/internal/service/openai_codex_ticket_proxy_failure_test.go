package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type ticketFailureProxyRepo struct {
	balancedAccountProxyStub
	randomProxyFailureStub
}

type ticketResultProxyRepo struct {
	ticketFailureProxyRepo
	randomProxySuccessStub
}

func TestCodexTicketProxyResultUsesActualPoolExitAndIgnoresFixedMode(t *testing.T) {
	repo := &ticketResultProxyRepo{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	proxy := openAICodexTicketProxy{accountID: 42, proxyID: 7}
	svc.reportOpenAICodexTicketProxyResult(context.Background(), proxy, false)
	svc.reportOpenAICodexTicketProxyResult(context.Background(), proxy, true)
	require.Equal(t, []int64{42}, repo.randomProxyFailureStub.accounts)
	require.Equal(t, []int64{7}, repo.randomProxyFailureStub.proxies)
	require.Equal(t, []int64{7}, repo.randomProxySuccessStub.proxies)
	fixed := openAICodexTicketProxy{url: "http://fixed.example:8080"}
	svc.reportOpenAICodexTicketProxyResult(context.Background(), fixed, false)
	svc.reportOpenAICodexTicketProxyResult(context.Background(), fixed, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc.reportOpenAICodexTicketProxyResult(ctx, proxy, false)
	require.Len(t, repo.randomProxyFailureStub.proxies, 1)
	require.Len(t, repo.randomProxySuccessStub.proxies, 1)
}

func TestCodexTicketPoolTransportFailureReportsActualEgress(t *testing.T) {
	account := ticketTestAccount(42)
	businessProxyID := int64(99)
	account.ProxyID = &businessProxyID
	account.Proxy = &Proxy{ID: 99, Protocol: "http", Host: "business.example", Port: 8080, Status: StatusActive}
	upstream := &ticketPoolUpstream{failure: errors.New("connection reset by peer")}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, upstream)
	repo := &ticketFailureProxyRepo{}
	repo.proxy = &Proxy{ID: 7, Status: StatusActive, Protocol: "http", Host: "pool.example", Port: 8080}
	svc.accountRepo = repo
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	require.Equal(t, []int64{42}, repo.randomProxyFailureStub.accounts)
	require.Equal(t, []int64{7}, repo.randomProxyFailureStub.proxies)
	require.EqualValues(t, 99, *account.ProxyID, "故障上报不得改写业务出口")
	require.False(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra").valid(time.Now(), 292))

	repo.proxy = &Proxy{ID: 8, Status: StatusActive, Protocol: "http", Host: "next.example", Port: 8080}
	upstream.failure = nil
	expireCodexTicketBackoff(t, svc, account, "gpt-6-astra")
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	require.Equal(t, []string{"http://pool.example:8080", "http://next.example:8080", "http://business.example:8080"}, upstream.proxies)
	require.Equal(t, []int64{7}, repo.randomProxyFailureStub.proxies)
	require.EqualValues(t, 99, *account.ProxyID)
	require.Equal(t, "http://business.example:8080", account.Proxy.URL())
	require.True(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra").valid(time.Now(), 292))
}

func TestCodexTicketPoolDoesNotEvictOnBusinessResponseOrCancellation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failure error
		status  int
	}{
		{name: "auth", status: http.StatusUnauthorized},
		{name: "rate limit", status: http.StatusTooManyRequests},
		{name: "cancel", failure: context.Canceled},
		{name: "deadline", failure: context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &ticketPoolUpstream{failure: tc.failure, status: tc.status}
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, upstream)
			repo := &ticketFailureProxyRepo{}
			repo.proxy = &Proxy{ID: 7, Status: StatusActive, Protocol: "http", Host: "pool.example", Port: 8080}
			svc.accountRepo = repo
			svc.probeOnceOpenAICodexTicket(context.Background(), ticketTestAccount(42), "gpt-6-astra")
			require.Len(t, upstream.proxies, 1)
			require.Empty(t, repo.randomProxyFailureStub.proxies)
		})
	}
}

func TestCodexTicketFixedFailureDoesNotEvictAccountProxy(t *testing.T) {
	upstream := &ticketPoolUpstream{failure: errors.New("connection reset by peer")}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://fixed.example:8080"}, upstream)
	repo := &ticketFailureProxyRepo{}
	svc.accountRepo = repo
	account := ticketTestAccount(42)
	account.Extra = map[string]any{ProxyModeExtraKey: ProxyModeRandom}
	id := int64(99)
	account.ProxyID = &id
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	require.Equal(t, []string{"http://fixed.example:8080"}, upstream.proxies)
	require.Empty(t, repo.randomProxyFailureStub.proxies)
}

func TestCodexTicketProxyFailureIgnoresErrorsBeforeTransport(t *testing.T) {
	repo := &ticketFailureProxyRepo{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	proxy := openAICodexTicketProxy{accountID: 42, proxyID: 7}
	svc.reportOpenAICodexTicketProxyFailure(context.Background(), proxy, errors.New("connection reset by peer"))
	require.Empty(t, repo.randomProxyFailureStub.proxies)
}

func TestCodexTicketFixedHarvestRespectsBusinessEmptyPoolPolicy(t *testing.T) {
	for _, policy := range []string{RandomProxyEmptyPoolPolicyReject, RandomProxyEmptyPoolPolicyDisable, RandomProxyEmptyPoolPolicyDirect} {
		t.Run(policy, func(t *testing.T) {
			account := pluginDirectoryAccount(policy)
			upstream := &ticketPoolUpstream{}
			repo := &ticketFailureProxyRepo{}
			repo.account = account
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://harvest.example:8080"}, upstream)
			svc.accountRepo = repo
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			if policy == RandomProxyEmptyPoolPolicyDirect {
				require.Equal(t, []string{"http://harvest.example:8080", ""}, upstream.proxies)
				require.True(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra").valid(time.Now(), 292))
			} else {
				require.Equal(t, []string{"http://harvest.example:8080"}, upstream.proxies)
				require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
			}
			if policy == RandomProxyEmptyPoolPolicyDisable {
				require.Equal(t, []int64{account.ID}, repo.disabledIDs)
				require.Equal(t, StatusDisabled, account.Status)
				require.False(t, account.Schedulable)
			} else {
				require.Empty(t, repo.disabledIDs)
				require.Equal(t, StatusActive, account.Status)
			}
			require.Empty(t, repo.randomProxyFailureStub.proxies)
			require.Nil(t, account.ProxyID, "业务复验不得将随机出口持久化为固定代理")
		})
	}
}

type codexTicketBusinessProxyRepo struct {
	ticketFailureProxyRepo
	choices []*Proxy
}

func (r *codexTicketBusinessProxyRepo) SelectBalancedProxy(_ context.Context, selection ProxyPoolSelection) (*Proxy, error) {
	r.selections = append(r.selections, selection)
	proxy := r.choices[0]
	r.choices = r.choices[1:]
	return proxy, nil
}

func TestCodexTicketPoolBusinessModelMismatchCountsOnlyActualEgress(t *testing.T) {
	account := pluginDirectoryAccount(RandomProxyEmptyPoolPolicyReject)
	repo := &codexTicketBusinessProxyRepo{choices: []*Proxy{
		{ID: 7, Status: StatusActive, Protocol: "http", Host: "harvest.example", Port: 8080},
		{ID: 99, Status: StatusActive, Protocol: "http", Host: "business.example", Port: 8080},
	}}
	repo.account = account
	u := &codexTicketVerificationUpstream{respond: func(call int) *http.Response {
		model := "gpt-6-astra"
		if call == 2 {
			model = "gpt-other"
		}
		return codexTicketCompletedResponse(model, fakeCodexTicketState(292))
	}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, u)
	svc.accountRepo = repo
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	require.Equal(t, []string{"http://harvest.example:8080", "http://business.example:8080"}, u.proxies)
	require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
	require.Equal(t, []int64{99}, repo.randomProxyFailureStub.proxies, "业务模型不符只累计一次实际业务出口失败，仓储连续失败才切换")
	require.Nil(t, account.ProxyID)
	require.Empty(t, repo.disabledIDs)
}
