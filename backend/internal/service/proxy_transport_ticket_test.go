package service

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type transportTicketRepo struct {
	accountEgressTicketRepo
	fallback *Proxy
	saved    []*Account
}

func (r *transportTicketRepo) GetCodexTicketAccountSnapshot(context.Context, int64) (*Account, error) {
	account := cloneOpenAICodexTicketAccount(r.account)
	if account.Proxy != nil {
		proxy := *account.Proxy
		account.Proxy = &proxy
	}
	return account, nil
}

func (r *transportTicketRepo) GetCodexTicketProxy(_ context.Context, id int64) (*Proxy, error) {
	if proxy := r.proxies[id]; proxy != nil {
		copy := *proxy
		return &copy, nil
	}
	return nil, ErrProxyNotFound
}

func (r *transportTicketRepo) ResolveFixedProxyFailover(_ context.Context, account *Account) (*Proxy, error) {
	configured := account.ConfiguredProxySnapshot()
	if configured.ProxyID != nil && *configured.ProxyID == 70 {
		return r.fallback, nil
	}
	if configured.ProxyID == nil {
		return nil, nil
	}
	return r.proxies[*configured.ProxyID], nil
}

func (r *transportTicketRepo) CompareAndSwapCodexTicket(_ context.Context, account *Account, _ string, _ any) (bool, error) {
	configured := account.ConfiguredProxySnapshot()
	if configured.ProxyID == nil || r.account.ProxyID == nil || *configured.ProxyID != *r.account.ProxyID ||
		configured.Proxy == nil || r.account.Proxy == nil || configured.Proxy.URL() != r.account.Proxy.URL() {
		return false, nil
	}
	r.saved = append(r.saved, cloneOpenAICodexTicketAccount(account))
	return true, nil
}

type transportTicketUpstream struct {
	ticketPoolUpstream
	after func(int)
}

func (u *transportTicketUpstream) Do(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	response, err := u.ticketPoolUpstream.Do(req, proxy, id, concurrency)
	if u.after != nil {
		u.after(len(u.proxies))
	}
	return response, err
}

func transportTicketFixture(t *testing.T) (*OpenAIGatewayService, *transportTicketRepo, *transportTicketUpstream) {
	t.Helper()
	svc, base := accountEgressTicketFixture(t)
	original := &Proxy{ID: 70, Name: "configured", Status: StatusActive, Protocol: "http", Host: "original.invalid", Port: 1333}
	fallback := &Proxy{ID: 58, Name: "fallback", Status: StatusActive, Protocol: "http", Host: "fallback.invalid", Port: 8080}
	base.proxies = map[int64]*Proxy{70: original, 58: fallback}
	base.account.ProxyID, base.account.Proxy = &original.ID, original
	repo := &transportTicketRepo{accountEgressTicketRepo: *base, fallback: fallback}
	upstream := &transportTicketUpstream{}
	svc.accountRepo, svc.httpUpstream = repo, upstream
	return svc, repo, upstream
}

func TestProxyTransportTicketPublishesOnFixedFallbackWithoutChangingConfiguration(t *testing.T) {
	svc, repo, upstream := transportTicketFixture(t)
	svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.Equal(t, []string{repo.fallback.URL(), repo.fallback.URL()}, upstream.proxies)
	require.Len(t, repo.saved, 1, "备用出口完成采集和业务复验后应发布")
	runtime := repo.saved[0]
	require.EqualValues(t, 58, *runtime.ProxyID)
	require.EqualValues(t, 70, *runtime.ConfiguredProxySnapshot().ProxyID)
	require.EqualValues(t, 70, *repo.account.ProxyID, "运行时换出口不能写回固定配置")
	ticket := svc.lookupOpenAICodexTicket(runtime, "gpt-6-astra")
	require.NotNil(t, ticket)
	require.True(t, ticket.valid(time.Now(), 292))
	require.Equal(t, openAICodexTicketEgress(repo.fallback.URL()), ticket.Egress)
	require.True(t, ticket.accountCompatible(runtime))
	require.False(t, ticket.accountCompatible(repo.account), "备用出口票不能注入原故障出口")
}

func TestProxyTransportTicketStillRejectsChangedConfiguredIdentity(t *testing.T) {
	for _, change := range []string{"proxy_id", "proxy_address", "token", "mode"} {
		for _, stage := range []int{1, 2} {
			t.Run(fmt.Sprintf("%s_stage_%d", change, stage), func(t *testing.T) {
				svc, repo, upstream := transportTicketFixture(t)
				upstream.after = func(calls int) {
					if calls != stage {
						return
					}
					switch change {
					case "proxy_id":
						updated := &Proxy{ID: 66, Status: StatusActive, Protocol: "http", Host: "changed.invalid", Port: 8080}
						repo.proxies[66] = updated
						repo.account.ProxyID, repo.account.Proxy = &updated.ID, updated
					case "proxy_address":
						repo.account.Proxy.Host = "changed.invalid"
					case "token":
						repo.account.Credentials["access_token"] = "changed-test-token"
					case "mode":
						repo.account.Extra[CodexTicketProxyModeExtraKey] = CodexTicketProxyModeInherit
					}
				}
				svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
				require.Len(t, upstream.proxies, stage)
				require.Empty(t, repo.saved, "配置变化仍须阻止旧尝试发布")
			})
		}
	}
}
