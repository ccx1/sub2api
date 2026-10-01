package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func codexTicketNodeTestOAILB(t *testing.T, node int) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"host": "chat.gateway.unified-" + strconv.Itoa(node) + ".api.openai.com", "exp": time.Now().Add(time.Hour).Unix()})
	require.NoError(t, err)
	return "eyJhbGciOiJIUzI1NiJ9." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func codexTicketNodeTestResponse(setCookie string) *http.Response {
	resp := codexTicketCompletedResponse("gpt-6-astra", "")
	resp.Body = io.NopCloser(strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"pong\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"model\":\"gpt-6-astra\"}}\n\n"))
	if setCookie != "" {
		resp.Header.Add("Set-Cookie", setCookie)
	}
	return resp
}

func codexTicketNodeTestService(t *testing.T, do func(*http.Request) (*http.Response, error)) (*OpenAIGatewayService, *ticketPreviewReadOnlyRepo) {
	t.Helper()
	account := ticketTestAccount(41)
	repo := &ticketPreviewReadOnlyRepo{account: account}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, Models: []string{"gpt-6-astra"},
		CredentialMode: config.CodexTicketCredentialCookie}, &codexTicketFuncUpstream{do: do})
	svc.accountRepo = repo
	return svc, repo
}

func TestCodexTicketNodeProbeStickyJarIsEphemeralAndRaw(t *testing.T) {
	first := codexTicketNodeTestOAILB(t, 7)
	var mu sync.Mutex
	var requests []*http.Request
	svc, repo := codexTicketNodeTestService(t, func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, req)
		if len(requests) == 1 {
			return codexTicketNodeTestResponse(codexOAILBCookieName + "=" + first + "; Path=/; Secure; HttpOnly"), nil
		}
		return codexTicketNodeTestResponse(""), nil
	})
	before, err := json.Marshal(repo.account)
	require.NoError(t, err)

	result, err := svc.ProbeOpenAICodexTicketNodes(context.Background(), 41, CodexTicketNodeProbeInput{Model: "gpt-6-astra", Source: CodexTicketNodeSourceStickyJar, Count: 2})
	require.NoError(t, err)
	require.Len(t, result.Rounds, 2)
	require.Len(t, requests, 2)
	require.Equal(t, requests[0].Header.Get("session_id"), requests[1].Header.Get("session_id"))
	require.Empty(t, requests[0].Header.Get("Cookie"))
	require.Contains(t, requests[1].Header.Get("Cookie"), first)

	r1, r2 := result.Rounds[0], result.Rounds[1]
	require.Equal(t, "ok", r1.Status, r1.Reason)
	require.Equal(t, "assigned", r1.NodeOutcome)
	require.Equal(t, "unified-7", r1.EffectiveNode.Name)
	require.Equal(t, first, r1.ReceivedCookies[0].Value)
	require.NotNil(t, r1.FirstDeltaMs)
	require.Equal(t, "resp_1", r1.ResponseID)
	require.Equal(t, "kept", r2.NodeOutcome)
	require.Equal(t, "unified-7", r2.EffectiveNode.Name)
	require.Equal(t, map[string]int{"unified-7": 2}, result.NodeCounts)
	// 测试阶段原始报文不打码。
	require.Equal(t, "raw", r2.Exchange.CaptureMode)
	require.Contains(t, http.Header(r2.Exchange.Request.Headers).Get("Cookie"), first)
	require.Equal(t, "Bearer tok", http.Header(r2.Exchange.Request.Headers).Get("Authorization"))
	require.Contains(t, r2.Exchange.Response.Body, "response.completed")

	after, err := json.Marshal(repo.account)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))
	require.Nil(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
}

func TestCodexTicketNodeProbeReplaysSelectedTicketAndSnapshotShowsNode(t *testing.T) {
	cookie := codexTicketNodeTestOAILB(t, 12)
	changed := codexTicketNodeTestOAILB(t, 3)
	var sent *http.Request
	svc, repo := codexTicketNodeTestService(t, func(req *http.Request) (*http.Response, error) {
		sent = req
		return codexTicketNodeTestResponse(codexOAILBCookieName + "=" + changed + "; Path=/; Secure"), nil
	})
	now := time.Now()
	ticket := &openAICodexTicket{AccountID: 41, Model: "gpt-6-astra", CredentialMode: config.CodexTicketCredentialCookie, SessionID: "sess-ticket",
		Egress: openAICodexTicketEgress(""), Verified: true, CapturedAt: now, ExpiresAt: now.Add(time.Hour),
		Cookies: []*http.Cookie{{Name: codexOAILBCookieName, Value: cookie, Domain: "chatgpt.com", Path: "/", Secure: true, Expires: now.Add(time.Hour)}}}
	raw, err := json.Marshal(ticket)
	require.NoError(t, err)
	var extra any
	require.NoError(t, json.Unmarshal(raw, &extra))
	repo.account.Extra = map[string]any{openAICodexTicketExtraKey("gpt-6-astra"): extra}

	snapshot, err := svc.GetOpenAICodexTicketNodes(context.Background(), 41)
	require.NoError(t, err)
	require.Len(t, snapshot.Models, 1)
	require.Len(t, snapshot.Models[0].Slots, 1)
	slot := snapshot.Models[0].Slots[0]
	require.Equal(t, "primary", slot.Label)
	require.Equal(t, "unified-12", slot.RouteNode.Name)
	require.Equal(t, "unified-12", slot.SentNode.Name)
	require.Equal(t, cookie, slot.Cookies[0].Value)
	require.Equal(t, "sess-ticket", slot.SessionID)
	require.Equal(t, CodexCookiePreserve, snapshot.CookieMode)
	require.True(t, snapshot.ProxyAvailable)
	require.Equal(t, "direct", snapshot.Proxy.Address)

	result, err := svc.ProbeOpenAICodexTicketNodes(context.Background(), 41, CodexTicketNodeProbeInput{Model: "gpt-6-astra", Source: CodexTicketNodeSourceTicket, Slot: "primary", Count: 1})
	require.NoError(t, err)
	require.NotNil(t, sent)
	require.Equal(t, "sess-ticket", sent.Header.Get("session_id"))
	require.Contains(t, sent.Header.Get("Cookie"), cookie)
	require.Equal(t, "primary", result.Slot)
	require.Equal(t, "unified-12", result.SlotNode.Name)
	require.Equal(t, "changed", result.Rounds[0].NodeOutcome)
	require.Equal(t, "unified-12", result.Rounds[0].SentNode.Name)
	require.Equal(t, "unified-3", result.Rounds[0].EffectiveNode.Name)

	// 探测收到的新 Cookie 不回写库存。
	stored := svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra")
	require.NotNil(t, stored)
	require.Equal(t, cookie, stored.Cookies[0].Value)
}

func TestCodexTicketNodeProbeRejectsInvalidInput(t *testing.T) {
	svc, _ := codexTicketNodeTestService(t, func(*http.Request) (*http.Response, error) { panic("must not send") })
	for _, input := range []CodexTicketNodeProbeInput{
		{Model: "gpt-6-astra", Source: "other"},
		{Model: "gpt-6-astra", Source: CodexTicketNodeSourceEmptyJar, Count: 6},
		{Model: "unknown", Source: CodexTicketNodeSourceEmptyJar},
		{Model: "gpt-6-astra", Source: CodexTicketNodeSourceTicket},
	} {
		_, err := svc.ProbeOpenAICodexTicketNodes(context.Background(), 41, input)
		var probeErr *CodexTicketNodeProbeError
		require.ErrorAs(t, err, &probeErr, input.Source)
	}
}

func TestCodexTicketNodeOutcome(t *testing.T) {
	node := func(n string) *CodexTicketNodeInfo { return &CodexTicketNodeInfo{Host: n, Name: n} }
	lb := func(n string) CodexTicketNodeCookie {
		return CodexTicketNodeCookie{Name: codexOAILBCookieName, Value: "v", Node: node(n)}
	}
	now := time.Now()
	for _, tc := range []struct {
		sent, received []CodexTicketNodeCookie
		want           string
	}{
		{nil, nil, "no_route"},
		{nil, []CodexTicketNodeCookie{lb("a")}, "assigned"},
		{[]CodexTicketNodeCookie{lb("a")}, nil, "kept"},
		{[]CodexTicketNodeCookie{lb("a")}, []CodexTicketNodeCookie{lb("a")}, "refreshed"},
		{[]CodexTicketNodeCookie{lb("a")}, []CodexTicketNodeCookie{lb("b")}, "changed"},
		{[]CodexTicketNodeCookie{lb("a")}, []CodexTicketNodeCookie{{Name: codexOAILBCookieName, MaxAge: -1}}, "cleared"},
		{nil, []CodexTicketNodeCookie{{Name: codexOAILBCookieName, Value: "garbage"}}, "unparsed"},
	} {
		_, _, got := codexTicketNodeOutcome(tc.sent, tc.received, now)
		require.Equal(t, tc.want, got)
	}
}
