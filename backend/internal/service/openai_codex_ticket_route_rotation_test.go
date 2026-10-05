package service

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func historicalRouteTicket(t *testing.T, marker, host string, age time.Duration) *openAICodexTicket {
	t.Helper()
	now := time.Now()
	ticket := usageTestTicket(marker, age, now)
	ticket.CredentialMode = config.CodexTicketCredentialCookieState
	ticket.SessionID = "session-" + marker
	ticket.Cookies = []*http.Cookie{{Name: "__oailb", Value: encodeTestOAILB(t, `{"host":"`+host+`"}`),
		Domain: "chatgpt.com", Path: "/backend-api", Secure: true, Expires: now.Add(time.Hour)}}
	return ticket
}

func TestHistoricalRouteRotationSkipsSameFreshHostAndCoolsTicket(t *testing.T) {
	account := ticketTestAccount(41)
	account.Extra = make(map[string]any)
	cfg := historicalTestConfig(4)
	cfg.CredentialMode = config.CodexTicketCredentialCookieState
	cfg.SkipSameRouteHost, cfg.SameRouteCooldownHours = true, 3
	a := historicalRouteTicket(t, "A", "old.example", 7*24*time.Hour+3*time.Hour)
	b := historicalRouteTicket(t, "B", "stored-b.example", 7*24*time.Hour+2*time.Hour)
	c := historicalRouteTicket(t, "C", "stored-c.example", 7*24*time.Hour+time.Hour)
	d := historicalRouteTicket(t, "D", "stored-d.example", 7*24*time.Hour+30*time.Minute)
	calls := map[string]int{}
	upstream := &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		state := req.Header.Get(openAICodexTurnStateHeader)
		marker, host := strings.TrimPrefix(req.Header.Get("session_id"), "session-"), "old.example"
		for _, item := range []struct {
			ticket      *openAICodexTicket
			name, route string
		}{{a, "A", "old.example"}, {b, "B", "old.example"}, {c, "C", "old.example"}, {d, "D", "new.example"}} {
			if marker == item.name {
				host = item.route
				break
			}
		}
		require.NotEmpty(t, marker)
		calls[marker]++
		require.Empty(t, state)
		require.Empty(t, req.Header.Get("Cookie"), "换票预检必须清掉旧 Cookie")
		response := codexTicketCompletedResponse(usageTestModel, state)
		response.Header.Add("Set-Cookie", "__oailb="+encodeTestOAILB(t, `{"host":"`+host+`"}`)+"; Path=/backend-api; Max-Age=60; Secure")
		return response, nil
	}}
	svc := usageTestService(t, cfg, a, b, c, d)
	svc.httpUpstream = upstream
	first, err := svc.selectHistoricalRouteTicket(context.Background(), account, usageTestModel, cfg)
	require.NoError(t, err)
	require.Equal(t, a.State, first.State)
	require.Zero(t, calls["A"], "首次选票只记录基线")

	key := openAICodexTicketKey(account.ID, usageTestModel)
	stored, _ := svc.openaiCodexTickets.Load(key)
	inventory := cloneCodexTicketInventory(stored.(*openAICodexTicket))
	inventory.Revoked = true
	svc.openaiCodexTickets.Store(key, inventory)
	account.Extra[openAICodexTicketExtraKey(usageTestModel)] = inventory
	selected, err := svc.selectHistoricalRouteTicket(context.Background(), account, usageTestModel, cfg)
	require.NoError(t, err)
	require.Equal(t, d.State, selected.State)
	require.Equal(t, "new.example", codexTicketRouteHost(selected))
	require.Equal(t, 1, calls["B"])
	require.Equal(t, 1, calls["C"])
	require.Equal(t, 1, calls["D"])
	stored, _ = svc.openaiCodexTickets.Load(key)
	inventory = stored.(*openAICodexTicket)
	require.WithinDuration(t, time.Now().Add(3*time.Hour), codexTicketRouteRotationCooldownUntil(inventory, b, time.Now()), time.Minute)
	require.WithinDuration(t, time.Now().Add(3*time.Hour), codexTicketRouteRotationCooldownUntil(inventory, c, time.Now()), time.Minute)
	require.Equal(t, usageTestStates(d), usageTestStates(codexTicketUsageCandidates(inventory, account, cfg, time.Now())...),
		"被跳过的票仍在库存，但冷却期间不能发放")

	selected, err = svc.selectHistoricalRouteTicket(context.Background(), account, usageTestModel, cfg)
	require.NoError(t, err)
	require.Equal(t, d.State, selected.State)
	require.Equal(t, 1, calls["D"], "同一张活跃票不重复预检")
	request, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
	require.NoError(t, err)
	require.NoError(t, svc.applyOpenAICodexTicketRequest(account, usageTestModel, request))
	require.Contains(t, request.Header.Get("Cookie"), "__oailb="+encodeTestOAILB(t, `{"host":"new.example"}`))
	require.NoError(t, svc.validateOpenAICodexTicketSend(request, account))

	svc.cfg.Gateway.OpenAICodexTicket.SkipSameRouteHost = false
	request, err = http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
	require.NoError(t, err)
	require.NoError(t, svc.applyOpenAICodexTicketRequest(account, usageTestModel, request))
	receipt := request.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
	require.Equal(t, b.State, receipt.ticket.State, "关闭开关后冷却记录不改变原有选票顺序")
}

func TestHistoricalRouteHostReadsAnyDecodedHost(t *testing.T) {
	ticket := &openAICodexTicket{Cookies: []*http.Cookie{{Name: codexOAILBCookieName,
		Value: encodeTestOAILB(t, `{"host":"EXAMPLE.com."}`)}}}
	require.Equal(t, "example.com", codexTicketRouteHost(ticket))
	ticket.Cookies[0].Value = strings.Repeat("!", 10)
	require.Empty(t, codexTicketRouteHost(ticket))
}

func TestHistoricalRouteRotationAlsoAppliesToConsumeAfterUse(t *testing.T) {
	account := ticketTestAccount(41)
	account.Extra = make(map[string]any)
	cfg := historicalTestConfig(3)
	cfg.CredentialMode = config.CodexTicketCredentialCookieState
	cfg.SkipSameRouteHost, cfg.ConsumeAfterUse = true, true
	a := historicalRouteTicket(t, "A", "old.example", 7*24*time.Hour+3*time.Hour)
	b := historicalRouteTicket(t, "B", "stored-b.example", 7*24*time.Hour+2*time.Hour)
	c := historicalRouteTicket(t, "C", "stored-c.example", 7*24*time.Hour+time.Hour)
	calls := map[string]int{}
	upstream := &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		marker := strings.TrimPrefix(req.Header.Get("session_id"), "session-")
		calls[marker]++
		host := "old.example"
		if marker == "C" {
			host = "new.example"
		}
		require.Empty(t, req.Header.Get("Cookie"))
		response := codexTicketCompletedResponse(usageTestModel, "")
		response.Header.Add("Set-Cookie", "__oailb="+encodeTestOAILB(t, `{"host":"`+host+`"}`)+"; Path=/backend-api; Max-Age=60; Secure")
		return response, nil
	}}
	svc := usageTestService(t, cfg, a, b, c)
	svc.httpUpstream = upstream
	first, _, err := usageTestApply(context.Background(), svc, account)
	require.NoError(t, err)
	require.Equal(t, a.State, first.ticket.State)
	require.NotNil(t, first.claimed)

	next, _, err := usageTestApply(context.Background(), svc, account)
	require.NoError(t, err)
	require.Equal(t, c.State, next.ticket.State)
	require.NotNil(t, next.claimed)
	require.Equal(t, 1, calls["B"])
	require.Equal(t, 1, calls["C"])
	stored, _ := svc.openaiCodexTickets.Load(openAICodexTicketKey(account.ID, usageTestModel))
	require.False(t, codexTicketRouteRotationCooldownUntil(stored.(*openAICodexTicket), b, time.Now()).IsZero())
}
