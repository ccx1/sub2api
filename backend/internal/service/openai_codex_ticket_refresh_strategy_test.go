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

func codexRefreshStrategyFixture(t *testing.T, mode string) (*OpenAIGatewayService, *Account, *openAICodexTicket) {
	t.Helper()
	svc, account, old := codexRevalidationFixture(t, mode)
	cfg := &svc.cfg.Gateway.OpenAICodexTicket
	cfg.RefreshStrategy = config.CodexTicketRefreshReplace
	cfg.RefreshBeforeSeconds = 60
	refreshBefore := 60
	cfg.CookieRefreshBeforeSeconds = &refreshBefore
	old.RevalidateAt = time.Now().Add(30 * time.Second)
	if old.usesCookies() {
		old.ExpiresAt = old.Cookies[0].Expires
	}
	old.Binding = svc.codexTicketBinding(account)
	saveCodexRefreshInventory(svc, account, old)
	return svc, account, old
}

func saveCodexRefreshInventory(svc *OpenAIGatewayService, account *Account, inventory *openAICodexTicket) {
	svc.openaiCodexTickets.Store(openAICodexTicketKey(account.ID, inventory.Model), cloneCodexTicketInventory(inventory))
	account.Extra[openAICodexTicketExtraKey(inventory.Model)] = cloneCodexTicketInventory(inventory)
}

func TestCodexTicketRefreshReplaceMintsPairAtRefreshWindow(t *testing.T) {
	for _, mode := range []string{"", config.CodexTicketCredentialCookieState, config.CodexTicketCredentialCookie} {
		t.Run(mode, func(t *testing.T) {
			svc, account, old := codexRefreshStrategyFixture(t, mode)
			fresh := codexTicketStateForExpiryTest(time.Now().Add(-20*time.Minute), 10)
			var states, cookies []string
			svc.httpUpstream = &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
				states = append(states, req.Header.Get(openAICodexTurnStateHeader))
				cookies = append(cookies, req.Header.Get("Cookie"))
				response := codexTicketCompletedResponse(old.Model, fresh)
				if len(states) == 1 && mode != "" {
					response.Header.Set("Set-Cookie", "session=fresh; Max-Age=600; Path=/; Secure")
				}
				return response, nil
			}}
			svc.harvestVerifiedOpenAICodexTicket(context.Background(), account, old.Model)
			require.Len(t, states, 2, "到刷新窗口直接新采，然后验证新凭据")
			require.Empty(t, states[0])
			require.Empty(t, cookies[0], "新采不能继承旧票或旧 Cookie")
			if mode == config.CodexTicketCredentialCookie {
				require.Empty(t, states[1])
			} else {
				require.Equal(t, fresh, states[1])
			}
			if mode != "" {
				require.Equal(t, "session=fresh", cookies[1])
			}
			next := svc.lookupOpenAICodexTicket(account, old.Model)
			require.NotNil(t, next)
			require.False(t, sameCodexTicket(old, next))
			require.True(t, next.ExpiresAt.Before(old.hardExpiresAt()), "新票硬期限更短也必须发布")
			require.True(t, next.RevalidateAt.After(time.Now().Add(time.Minute)))
			svc.harvestVerifiedOpenAICodexTicket(context.Background(), account, old.Model)
			require.Len(t, states, 2, "新票发布后不能继续无限新采")
		})
	}
}

func TestCodexTicketRefreshReplacePendingCookieDoesNotAdvanceWindow(t *testing.T) {
	svc, account, old := codexRefreshStrategyFixture(t, config.CodexTicketCredentialCookieState)
	old.RevalidateAt = time.Now().Add(3 * time.Minute)
	saveCodexRefreshInventory(svc, account, old)
	req, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
	require.NoError(t, err)
	require.NoError(t, svc.applyOpenAICodexTicketRequest(account, old.Model, req))
	svc.observeOpenAICodexTicketResponse(req, &http.Response{StatusCode: 429,
		Header: http.Header{"Set-Cookie": {"session=pending; Max-Age=600; Path=/; Secure"}}})
	require.NotNil(t, svc.pendingCodexTicketCookies(account.ID, old.Model))
	calls := 0
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		calls++
		return codexTicketCompletedResponse(old.Model, ""), nil
	}}
	svc.harvestVerifiedOpenAICodexTicket(context.Background(), account, old.Model)
	require.Zero(t, calls)
	require.True(t, sameCodexTicket(old, svc.lookupOpenAICodexTicket(account, old.Model)))
}

func TestCodexTicketRefreshReplaceFailureKeepsOldTicket(t *testing.T) {
	for _, failure := range []string{"mint", "verification", "persistence", "cas"} {
		t.Run(failure, func(t *testing.T) {
			svc, account, old := codexRefreshStrategyFixture(t, "")
			fresh := codexTicketStateForExpiryTest(time.Now().Add(-20*time.Minute), 10)
			calls := 0
			svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
				calls++
				if failure == "mint" || failure == "verification" && calls == 2 {
					return nil, errors.New("unexpected EOF")
				}
				return codexTicketCompletedResponse(old.Model, fresh), nil
			}}
			if failure == "persistence" {
				svc.accountRepo = &codexTicketLifecycleRepo{persist: func(context.Context) error { return errors.New("write failed") }}
			} else if failure == "cas" {
				svc.accountRepo = &codexTicketCASStub{update: func(*Account, string, any) (bool, error) { return false, nil }}
			}
			svc.harvestVerifiedOpenAICodexTicket(context.Background(), account, old.Model)
			require.True(t, sameCodexTicket(old, svc.lookupOpenAICodexTicket(account, old.Model)))
			require.False(t, svc.codexTicketRevoked(openAICodexTicketKey(account.ID, old.Model), old))
			req, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
			require.NoError(t, err)
			require.NoError(t, svc.applyOpenAICodexTicketRequest(account, old.Model, req))
			require.Equal(t, old.State, req.Header.Get(openAICodexTurnStateHeader))
		})
	}
}

func TestCodexTicketRefreshStrategyManualAlwaysMints(t *testing.T) {
	for _, strategy := range []string{config.CodexTicketRefreshRevalidate, config.CodexTicketRefreshReplace} {
		t.Run(strategy, func(t *testing.T) {
			svc, account, old := codexRefreshStrategyFixture(t, "")
			svc.cfg.Gateway.OpenAICodexTicket.RefreshStrategy = strategy
			old.RevalidateAt = time.Now().Add(3 * time.Minute)
			saveCodexRefreshInventory(svc, account, old)
			fresh := codexTicketStateForExpiryTest(time.Now(), 10)
			var states []string
			svc.httpUpstream = &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
				states = append(states, req.Header.Get(openAICodexTurnStateHeader))
				return codexTicketCompletedResponse(old.Model, fresh), nil
			}}
			svc.harvestVerifiedOpenAICodexTicket(withCodexTicketManualRetry(context.Background()), account, old.Model)
			require.Equal(t, []string{"", fresh}, states)
		})
	}
}
