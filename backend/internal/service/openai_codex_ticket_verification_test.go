package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func codexTicketCompletedResponse(model, state string) *http.Response {
	h := http.Header{}
	h.Set(openAICodexTurnStateHeader, state)
	body := fmt.Sprintf("data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"model\":%q}}\n\n", model)
	return &http.Response{StatusCode: http.StatusOK, Header: h, Body: io.NopCloser(strings.NewReader(body))}
}

func TestCodexTicketProbeRequiresCompletedMatchingResponse(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"mismatched_model", `{"object":"response","status":"completed","model":"gpt-other"}`},
		{"failed", `{"object":"response","status":"failed","model":"gpt-6-astra"}`},
		{"truncated", "data: {\"type\":\"response.created\"}\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
				response := codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(292))
				response.Body = io.NopCloser(strings.NewReader(tc.body))
				return response, nil
			}}
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{}, u)
			_, _, err := svc.fireOpenAICodexTicketProbe(context.Background(), ticketTestAccount(41), "test", "gpt-6-astra", "", time.Second)
			require.Error(t, err)
		})
	}
}

type codexTicketVerificationUpstream struct {
	HTTPUpstream
	requests []*http.Request
	proxies  []string
	respond  func(int) *http.Response
}

func (u *codexTicketVerificationUpstream) Do(req *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	u.requests = append(u.requests, req)
	u.proxies = append(u.proxies, proxy)
	return u.respond(len(u.requests)), nil
}

func TestCodexTicketHarvestVerifiesBusinessEgressBeforePublishing(t *testing.T) {
	for _, matched := range []bool{true, false} {
		t.Run(fmt.Sprint(matched), func(t *testing.T) {
			state := fakeCodexTicketState(292)
			u := &codexTicketVerificationUpstream{respond: func(call int) *http.Response {
				model := "gpt-6-astra"
				if call == 2 && !matched {
					model = "gpt-other"
				}
				return codexTicketCompletedResponse(model, state)
			}}
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://harvest.example:8080"}, u)
			account := ticketTestAccount(41)
			proxyID := int64(7)
			account.ProxyID = &proxyID
			account.Proxy = &Proxy{ID: 7, Protocol: "http", Host: "business.example", Port: 8080}
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			require.Equal(t, []string{"http://harvest.example:8080", "http://business.example:8080"}, u.proxies)
			require.Empty(t, u.requests[0].Header.Get(openAICodexTurnStateHeader))
			require.Equal(t, state, u.requests[1].Header.Get(openAICodexTurnStateHeader))
			require.Equal(t, matched, svc.lookupOpenAICodexTicket(account, "gpt-6-astra").valid(time.Now(), 292))
			require.EqualValues(t, 7, *account.ProxyID)
		})
	}
}

func TestCodexTicketRenewalPersistenceFailureKeepsPreviousTicket(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	account := ticketTestAccount(41)
	previous := &openAICodexTicket{Model: "gpt-6-astra", State: fakeCodexTicketState(292), Length: 292, CapturedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour)}
	svc.storeOpenAICodexTicket(context.Background(), account, previous)
	svc.accountRepo = &codexTicketLifecycleRepo{persist: func(context.Context) error { return errors.New("write failed") }}
	next := *previous
	next.State = "gAAAAA" + strings.Repeat("C", 286)
	next.CapturedAt = time.Now()
	svc.storeOpenAICodexTicket(context.Background(), account, &next)
	require.Equal(t, previous.State, svc.lookupOpenAICodexTicket(account, previous.Model).State)
}

func TestCodexTicketHarvestCooldownAppliesAcrossModelsAndKeepsOldTicket(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests} {
		for _, rejectedPhase := range []int{1, 2} {
			t.Run(fmt.Sprintf("%d_phase_%d", status, rejectedPhase), func(t *testing.T) {
				u := &codexTicketVerificationUpstream{respond: func(call int) *http.Response {
					response := codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(292))
					if call == rejectedPhase {
						response.StatusCode = status
						response.Header.Set("Retry-After", "900")
					}
					return response
				}}
				svc, account, previous, _ := ticketWatchdogFixture(t)
				previous.ExpiresAt = time.Now().Add(time.Minute)
				require.True(t, svc.storeOpenAICodexTicket(context.Background(), account, previous))
				svc.httpUpstream = u
				svc.cfg.Gateway.OpenAICodexTicket.HarvestProxyURL = "http://harvest.example:8080"
				svc.probeOnceOpenAICodexTicket(context.Background(), account, previous.Model)
				svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-5.6-sol")
				require.Len(t, u.requests, rejectedPhase)
				require.True(t, svc.openAICodexTicketCooling(account, "tok"))
				require.Equal(t, previous.CapturedAt, svc.lookupOpenAICodexTicket(account, previous.Model).CapturedAt)
				// 更换账号凭据后允许重新采集，不继承旧凭据的冷却。
				account.Credentials["access_token"] = "rotated-token"
				svc.probeOnceOpenAICodexTicket(context.Background(), account, previous.Model)
				require.Len(t, u.requests, rejectedPhase+2)
			})
		}
	}
}
