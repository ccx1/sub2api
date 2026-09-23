package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func codexQualityFixture(t *testing.T, mode string) (*OpenAIGatewayService, *codexScheduleRepo) {
	t.Helper()
	svc, repo := codexScheduledFixture(t, true)
	svc.cfg.Gateway.OpenAICodexTicket.BusinessVerificationRounds = 3
	svc.cfg.Gateway.OpenAICodexTicket.CredentialMode = mode
	svc.cfg.Gateway.OpenAICodexTicket.CookieTTLSeconds = 60
	svc.cfg.Gateway.OpenAICodexTicket.HarvestProxyURL = "http://quality.example:8080"
	repo.proxy = &Proxy{ID: 8, Status: StatusActive, Protocol: "http", Host: "quality.example", Port: 8080}
	repo.account.ProxyID, repo.account.Proxy = &repo.proxy.ID, repo.proxy
	return svc, repo
}

func codexQualityResponse(call int, model string) *http.Response {
	state := fakeCodexTicketState(292)
	if call > 1 {
		state = "gAAAAA" + strings.Repeat("C", 286)
	}
	response := codexTicketCompletedResponse(model, state)
	for _, name := range []string{"__cflb", "__oailb"} {
		response.Header.Add("Set-Cookie", fmt.Sprintf("%s=mint-%d; Path=/; Secure; Max-Age=60", name, call))
	}
	return response
}

func assertCodexQualityCredentials(t *testing.T, requests []*http.Request, mode string) {
	t.Helper()
	require.NotEmpty(t, requests)
	session := requests[0].Header.Get("session_id")
	require.NotEmpty(t, session)
	for index, req := range requests {
		require.Equal(t, session, req.Header.Get("session_id"))
		if index == 0 {
			require.Empty(t, req.Header.Get("Cookie"))
			require.Empty(t, req.Header.Get(openAICodexTurnStateHeader))
			continue
		}
		cookies := map[string]string{}
		for _, cookie := range req.Cookies() {
			cookies[cookie.Name] = cookie.Value
		}
		require.Equal(t, map[string]string{"__cflb": "mint-1", "__oailb": "mint-1"}, cookies)
		if mode == config.CodexTicketCredentialCookie {
			require.Empty(t, req.Header.Get(openAICodexTurnStateHeader))
		} else {
			require.Equal(t, fakeCodexTicketState(292), req.Header.Get(openAICodexTurnStateHeader))
		}
	}
}

func TestCodexTicketQualityFreezesMintCredentialsThroughBusinessVerification(t *testing.T) {
	for _, mode := range []string{"state", "cookie", "cookie_state"} {
		for _, separateBusiness := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/separate_%t", mode, separateBusiness), func(t *testing.T) {
				svc, repo := codexQualityFixture(t, mode)
				if separateBusiness {
					business := &Proxy{ID: 9, Status: StatusActive, Protocol: "http", Host: "business.example", Port: 8080}
					repo.account.ProxyID, repo.account.Proxy = &business.ID, business
				}
				upstream := &codexTicketVerificationUpstream{respond: func(call int) *http.Response {
					return codexQualityResponse(call, "gpt-6-astra")
				}}
				svc.httpUpstream = upstream
				svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
				wantCalls := 4
				if separateBusiness {
					wantCalls++
				}
				require.Len(t, upstream.requests, wantCalls)
				assertCodexQualityCredentials(t, upstream.requests, mode)
				for _, proxy := range upstream.proxies[:4] {
					require.Equal(t, "http://quality.example:8080", proxy)
				}
				require.Equal(t, repo.account.Proxy.URL(), upstream.proxies[wantCalls-1])
				published := svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra")
				require.NotNil(t, published)
				require.Equal(t, openAICodexTicketEgress(repo.account.Proxy.URL()), published.Egress)
				require.Equal(t, upstream.requests[0].Header.Get("session_id"), published.SessionID)
				for _, cookie := range published.Cookies {
					require.Equal(t, "mint-1", cookie.Value)
				}
				require.Len(t, repo.history, 1)
				require.True(t, repo.history[0].Success)
				require.Equal(t, 3, repo.history[0].BusinessVerificationPassed)
			})
		}
	}
}

func TestCodexTicketQualitySecondMismatchStopsAndRecordsFailedModel(t *testing.T) {
	svc, repo := codexQualityFixture(t, config.CodexTicketCredentialCookieState)
	upstream := &codexTicketVerificationUpstream{respond: func(call int) *http.Response {
		model := "gpt-6-astra"
		if call == 3 {
			model = "gpt-5.6-luna"
		}
		return codexQualityResponse(call, model)
	}}
	svc.httpUpstream = upstream
	svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.Len(t, upstream.requests, 3)
	assertCodexQualityCredentials(t, upstream.requests, config.CodexTicketCredentialCookieState)
	require.Nil(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
	require.Len(t, repo.history, 1)
	require.False(t, repo.history[0].Success)
	require.Equal(t, 1, repo.history[0].BusinessVerificationPassed)
	require.Contains(t, repo.history[0].BusinessVerificationModels, "gpt-6-astra")
	require.Contains(t, repo.history[0].BusinessVerificationModels, "gpt-5.6-luna")
	require.Len(t, repo.finishes, 1)
	require.True(t, repo.finishes[0].QualityProxyFailed)
	require.False(t, repo.finishes[0].HarvestProxyFailed)
	require.False(t, repo.finishes[0].BusinessProxyFailed)
	require.False(t, repo.finishes[0].BusinessProxySucceeded)
	require.Zero(t, repo.businessFailures)
}

func TestCodexTicketQualityAuthAndRateLimitDoNotPunishProxy(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			svc, repo := codexQualityFixture(t, config.CodexTicketCredentialCookieState)
			upstream := &codexTicketVerificationUpstream{respond: func(call int) *http.Response {
				response := codexQualityResponse(call, "gpt-6-astra")
				if call == 3 {
					response.StatusCode = status
					response.Header.Set("Retry-After", "90")
				}
				return response
			}}
			svc.httpUpstream = upstream
			svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
			require.Len(t, upstream.requests, 3)
			require.Nil(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
			require.Len(t, repo.finishes, 1)
			finish := repo.finishes[0]
			require.False(t, finish.QualityProxyFailed)
			require.False(t, finish.HarvestProxyFailed)
			require.False(t, finish.BusinessProxyFailed)
			require.False(t, finish.BusinessProxySucceeded)
			require.False(t, finish.Silence)
			require.NotEmpty(t, finish.RetryScope)
			require.Zero(t, repo.businessFailures)
		})
	}
}

type codexQualityLeaseRepo struct {
	*codexScheduleRepo
	lost bool
}

func (r *codexQualityLeaseRepo) ValidateCodexTicket(context.Context, *CodexTicketReservation) error {
	r.validates++
	if r.lost {
		return errors.New("quality test lease lost")
	}
	return nil
}

func TestCodexTicketQualityStopsAfterConfigOrLeaseChanges(t *testing.T) {
	for _, cause := range []string{"config", "lease"} {
		t.Run(cause, func(t *testing.T) {
			svc, base := codexQualityFixture(t, config.CodexTicketCredentialCookieState)
			repo := &codexQualityLeaseRepo{codexScheduleRepo: base}
			svc.accountRepo = repo
			upstream := &codexTicketVerificationUpstream{respond: func(call int) *http.Response {
				if call == 2 {
					if cause == "config" {
						svc.cfg.Gateway.OpenAICodexTicket.BusinessVerificationRounds = 4
					} else {
						repo.lost = true
					}
				}
				return codexQualityResponse(call, "gpt-6-astra")
			}}
			svc.httpUpstream = upstream
			svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
			require.Len(t, upstream.requests, 2)
			require.Nil(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
			require.Len(t, repo.history, 1)
			require.Equal(t, "controls_changed", repo.history[0].Reason)
			require.Len(t, repo.finishes, 1)
			require.False(t, repo.finishes[0].QualityProxyFailed)
			require.False(t, repo.finishes[0].BusinessProxyFailed)
			if cause == "lease" {
				require.Positive(t, repo.validates)
			}
		})
	}
}

func TestCodexTicketQualityStopsAfterAccountChanges(t *testing.T) {
	for _, change := range []string{"disabled", "token"} {
		t.Run(change, func(t *testing.T) {
			svc, repo := codexQualityFixture(t, config.CodexTicketCredentialCookieState)
			upstream := &codexTicketVerificationUpstream{respond: func(call int) *http.Response {
				if call == 2 {
					if change == "disabled" {
						repo.account.Status = StatusDisabled
					} else {
						repo.account.Credentials["access_token"] = "rotated-test-token"
					}
				}
				return codexQualityResponse(call, "gpt-6-astra")
			}}
			svc.httpUpstream = upstream
			svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
			require.Len(t, upstream.requests, 2)
			require.Nil(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
			require.Len(t, repo.history, 1)
			require.Equal(t, "controls_changed", repo.history[0].Reason)
			require.Len(t, repo.finishes, 1)
			require.False(t, repo.finishes[0].QualityProxyFailed)
			require.False(t, repo.finishes[0].HarvestProxyFailed)
			require.False(t, repo.finishes[0].BusinessProxyFailed)
			require.False(t, repo.finishes[0].BusinessProxySucceeded)
			require.Zero(t, repo.businessFailures)
		})
	}
}

func TestCodexTicketQualityRoundsChangePreservesPublishedBinding(t *testing.T) {
	for _, mode := range []string{"state", "cookie", "cookie_state"} {
		t.Run(mode, func(t *testing.T) {
			svc, repo := codexQualityFixture(t, mode)
			upstream := &codexTicketVerificationUpstream{respond: func(call int) *http.Response {
				return codexQualityResponse(call, "gpt-6-astra")
			}}
			svc.httpUpstream = upstream
			svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
			published := svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra")
			require.NotNil(t, published)
			for _, rounds := range []int{0, 1, 5, 10} {
				svc.cfg.Gateway.OpenAICodexTicket.BusinessVerificationRounds = rounds
				require.Equal(t, published.Binding, svc.codexTicketBinding(repo.account))
				current := svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra")
				require.NotNil(t, current)
				require.Equal(t, published.credentialIdentity(), current.credentialIdentity())
				require.True(t, current.usable(time.Now(), repo.account, svc.openAICodexTicketConfig()))
			}
			require.Len(t, upstream.requests, 4)
		})
	}
}

func TestCodexTicketQualityDefaultSingleRoundRejectsReturnedStateLength(t *testing.T) {
	svc, repo := codexQualityFixture(t, config.CodexTicketCredentialState)
	svc.cfg.Gateway.OpenAICodexTicket.BusinessVerificationRounds = 0
	svc.cfg.Gateway.OpenAICodexTicket.LengthMode = config.CodexTicketLengthStrict
	upstream := &codexTicketVerificationUpstream{respond: func(call int) *http.Response {
		length := 292
		if call == 2 {
			length = 312
		}
		return codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(length))
	}}
	svc.httpUpstream = upstream
	svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.Equal(t, 1, svc.openAICodexTicketConfig().BusinessVerificationRounds)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, fakeCodexTicketState(292), upstream.requests[1].Header.Get(openAICodexTurnStateHeader))
	require.Nil(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
	require.Len(t, repo.history, 1)
	require.Equal(t, "business_ticket_rejected", repo.history[0].Reason)
	require.Len(t, repo.finishes, 1)
	require.False(t, repo.finishes[0].QualityProxyFailed)
	require.True(t, repo.finishes[0].BusinessProxyFailed)
}
