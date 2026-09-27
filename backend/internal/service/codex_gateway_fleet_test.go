package service

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func encodeTestOAILBJWT(t *testing.T, payload string) string {
	t.Helper()
	return base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." +
		base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".c2ln"
}

func TestCodexGatewayFleetTableMatchesEnumeration(t *testing.T) {
	holes := map[int]bool{10: true, 14: true, 28: true, 33: true, 34: true, 38: true, 70: true,
		90: true, 104: true, 105: true, 106: true, 170: true, 171: true, 172: true}
	require.Len(t, codexGatewayFleetNodes, 201)
	require.Len(t, codexGatewayFleetRegions, 38)
	countries := map[string]int{}
	for number := 1; number <= codexGatewayFleetMaxNode; number++ {
		node, ok := codexGatewayFleetNodes[number]
		require.Equal(t, !holes[number], ok, "unified-%d", number)
		if ok {
			countries[node.Country]++
			require.NotEmpty(t, node.MacroRegion(), "unified-%d 所在国家必须有大区", number)
		}
	}
	require.Len(t, countries, 22)
	require.Equal(t, 87, countries["US"])
	require.Equal(t, 14, countries["BR"])
}

func TestParseCodexGatewayNodeHost(t *testing.T) {
	node, ok := parseCodexGatewayNodeHost("chat.gateway.unified-39.api.openai.com")
	require.True(t, ok)
	require.Equal(t, codexGatewayNode{Number: 39, Country: "ES", Region: "Madrid"}, node)
	require.Equal(t, "unified-39", node.Name())
	require.Equal(t, CodexMacroRegionEurope, node.MacroRegion())

	node, ok = parseCodexGatewayNodeHost(" CHAT.GATEWAY.UNIFIED-121.API.OPENAI.COM. ")
	require.True(t, ok)
	require.Equal(t, CodexMacroRegionNorthAmerica, node.MacroRegion())

	// 空洞编号仍是合法节点身份，但没有位置，不能推断大区。
	node, ok = parseCodexGatewayNodeHost("chat.gateway.unified-38.api.openai.com")
	require.True(t, ok)
	require.Equal(t, codexGatewayNode{Number: 38}, node)
	require.Empty(t, node.MacroRegion())

	for _, host := range []string{"", "chatgpt.com", "chat.gateway.unified-0.api.openai.com",
		"chat.gateway.unified-039.api.openai.com", "chat.gateway.unified-216.api.openai.com",
		"chat.gateway.unified-x.api.openai.com", "chat.gateway.unified-39.api.openai.com.evil.test"} {
		_, ok := parseCodexGatewayNodeHost(host)
		require.False(t, ok, host)
	}
}

func TestCodexGatewayRouteCrossRegionOnlyAcrossMacroRegions(t *testing.T) {
	madrid, _ := parseCodexGatewayNodeHost("chat.gateway.unified-39.api.openai.com")
	osaka, _ := parseCodexGatewayNodeHost("chat.gateway.unified-96.api.openai.com")
	sydney, _ := parseCodexGatewayNodeHost("chat.gateway.unified-23.api.openai.com")
	hole, _ := parseCodexGatewayNodeHost("chat.gateway.unified-38.api.openai.com")
	// 德国/英国出口落到西班牙节点：同大区跨国，属正常分配。
	require.False(t, codexGatewayRouteCrossRegion(madrid, "DE"))
	require.False(t, codexGatewayRouteCrossRegion(madrid, "gb"))
	require.False(t, codexGatewayRouteCrossRegion(osaka, "JP"))
	require.False(t, codexGatewayRouteCrossRegion(sydney, "AU"))
	require.True(t, codexGatewayRouteCrossRegion(madrid, "US"))
	require.True(t, codexGatewayRouteCrossRegion(osaka, "DE"))
	// 任一侧大区未知都不判异常。
	require.False(t, codexGatewayRouteCrossRegion(madrid, ""))
	require.False(t, codexGatewayRouteCrossRegion(madrid, "ZA"))
	require.False(t, codexGatewayRouteCrossRegion(hole, "US"))
}

func TestDecodeCodexOAILBCookieAcceptsJWT(t *testing.T) {
	exp := time.Now().Add(4 * time.Minute).Unix()
	value := encodeTestOAILBJWT(t, `{"host":"chat.gateway.unified-39.api.openai.com","exp":`+jsonInt(exp)+`}`)
	claims, expires, err := decodeCodexOAILBCookie(value)
	require.NoError(t, err)
	require.Equal(t, "chat.gateway.unified-39.api.openai.com", claims["host"])
	require.Equal(t, time.Unix(exp, 0).UTC(), expires)
	node, ok := codexOAILBNode(value)
	require.True(t, ok)
	require.Equal(t, 39, node.Number)

	_, ok = codexOAILBNode(encodeTestOAILBJWT(t, `{"host":"chatgpt.com"}`))
	require.False(t, ok)
	_, _, err = decodeCodexOAILBCookie("a.!!!.c")
	require.Error(t, err)
}

func TestOpenAIWSRouteFingerprintUsesComputeNode(t *testing.T) {
	now := time.Now()
	first := encodeTestOAILBJWT(t, `{"host":"chat.gateway.unified-39.api.openai.com","exp":`+jsonInt(now.Add(time.Minute).Unix())+`}`)
	refreshed := encodeTestOAILBJWT(t, `{"host":"chat.gateway.unified-39.api.openai.com","exp":`+jsonInt(now.Add(5*time.Minute).Unix())+`}`)
	other := encodeTestOAILBJWT(t, `{"host":"chat.gateway.unified-60.api.openai.com","exp":`+jsonInt(now.Add(time.Minute).Unix())+`}`)

	a := openAIWSRouteFingerprint(http.Header{"Set-Cookie": {"__oailb=" + first + "; Path=/", "__cflb=edge-a; Path=/"}})
	b := openAIWSRouteFingerprint(http.Header{"Set-Cookie": {"__oailb=" + refreshed + "; Path=/", "__cflb=edge-b; Path=/"}})
	c := openAIWSRouteFingerprint(http.Header{"Set-Cookie": {"__oailb=" + other + "; Path=/"}})
	require.Equal(t, "node:unified-39", a)
	require.Equal(t, a, b, "exp 刷新或边缘 Cookie 变化不代表换节点")
	require.NotEqual(t, a, c, "同为 Madrid 的不同节点仍是不同路由")
	require.NotContains(t, a, first)

	// 请求侧同名 Cookie 指向同一节点时视为一致，指向不同节点时无法确认路由。
	req := &http.Request{Header: http.Header{}}
	req.AddCookie(&http.Cookie{Name: "__oailb", Value: first})
	req.AddCookie(&http.Cookie{Name: "__oailb", Value: refreshed})
	require.Equal(t, "node:unified-39", openAIWSRequestRouteFingerprint(req.Header))
	req.AddCookie(&http.Cookie{Name: "__oailb", Value: other})
	require.Empty(t, openAIWSRequestRouteFingerprint(req.Header))
}

// legacyOpenAIWSRouteFingerprint 是节点识别之前的原策略：原始值排序后哈希。
func legacyOpenAIWSRouteFingerprint(pairs ...string) string {
	sort.Strings(pairs)
	sum := sha256.Sum256([]byte(strings.Join(pairs, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestOpenAIWSRouteFingerprintFallsBackWhenNodeUnavailable(t *testing.T) {
	exp := jsonInt(time.Now().Add(time.Minute).Unix())
	cases := map[string]string{
		"opaque":            "opaque-route-a",
		"jwt_bad_payload":   "eyJhbGciOiJIUzI1NiJ9.!!!not-base64!!!.c2ln",
		"jwt_not_object":    encodeTestOAILBJWT(t, `[1,2]`),
		"jwt_no_host":       encodeTestOAILBJWT(t, `{"exp":`+exp+`}`),
		"jwt_host_not_node": encodeTestOAILBJWT(t, `{"host":"chatgpt.com","exp":`+exp+`}`),
		"jwt_host_bad_num":  encodeTestOAILBJWT(t, `{"host":"chat.gateway.unified-999.api.openai.com"}`),
		"jwt_host_not_str":  encodeTestOAILBJWT(t, `{"host":39}`),
		"two_segments":      "abc.def",
		"four_segments":     "a.b.c.d",
		"looks_like_marker": "node:unified-39",
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			_, ok := codexOAILBNode(value)
			require.False(t, ok)
			got := openAIWSRouteFingerprint(http.Header{"Set-Cookie": {"__oailb=" + value + "; Path=/", "__cflb=edge-a; Path=/"}})
			require.Equal(t, legacyOpenAIWSRouteFingerprint("__oailb="+value, "__cflb=edge-a"), got, "解析不出节点时必须沿用原指纹")
			require.NotContains(t, got, "node:")
			require.NotContains(t, got, value)
			// 回退后 __cflb 仍参与比对，边缘变化照旧视为路由变化。
			require.NotEqual(t, got, openAIWSRouteFingerprint(http.Header{"Set-Cookie": {"__oailb=" + value + "; Path=/", "__cflb=edge-b; Path=/"}}))
		})
	}
}

func TestOpenAIWSRouteFingerprintNodeAndFallbackNeverMatch(t *testing.T) {
	node := encodeTestOAILBJWT(t, `{"host":"chat.gateway.unified-39.api.openai.com"}`)
	// 上游把节点票换成解析不了的值，应判为路由变化而不是沿用节点。
	nodeFP := openAIWSRouteFingerprint(http.Header{"Set-Cookie": {"__oailb=" + node}})
	opaqueFP := openAIWSRouteFingerprint(http.Header{"Set-Cookie": {"__oailb=opaque-route-a"}})
	require.NotEqual(t, nodeFP, opaqueFP)
	require.NotEqual(t, nodeFP, openAIWSRouteFingerprint(http.Header{"Set-Cookie": {"__oailb=node:unified-39"}}))

	// 请求侧同名 Cookie 一个能解析、一个不能，无法确认路由。
	req := &http.Request{Header: http.Header{}}
	req.AddCookie(&http.Cookie{Name: "__oailb", Value: node})
	req.AddCookie(&http.Cookie{Name: "__oailb", Value: "opaque-route-a"})
	require.Empty(t, openAIWSRequestRouteFingerprint(req.Header))
}

func TestCodexOAILBDecodeFailureKeepsLegacyBehaviour(t *testing.T) {
	// 旧格式整体 base64 JSON 仍可解出 exp 和节点。
	legacy := encodeTestOAILB(t, `{"host":"chat.gateway.unified-96.api.openai.com","exp":1900000000}`)
	_, expires, err := decodeCodexOAILBCookie(legacy)
	require.NoError(t, err)
	require.Equal(t, int64(1900000000), expires.Unix())
	node, ok := codexOAILBNode(legacy)
	require.True(t, ok)
	require.Equal(t, 96, node.Number)

	// 解析失败：无路由到期、无节点，状态与发布日志都不补节点字段，由 Cookie 自身期限决定。
	ticket := &openAICodexTicket{Model: "gpt-5.6-codex", CredentialMode: config.CodexTicketCredentialCookie,
		ExpiresAt: time.Now().Add(time.Hour), HarvestCountry: "US",
		Cookies: []*http.Cookie{{Name: "__oailb", Value: "eyJhbGciOiJIUzI1NiJ9.!!!.c2ln", Expires: time.Now().Add(time.Hour)}}}
	require.True(t, codexOAILBCookieExpiry(ticket.Cookies[0]).IsZero())
	require.True(t, codexTicketRouteExpiresAt(ticket).IsZero())
	_, ok = codexTicketRouteNode(ticket)
	require.False(t, ok)
	require.Nil(t, codexOAILBClaimsForLog(ticket))
	require.NotEmpty(t, codexTicketRouteFingerprint(ticket), "解析失败的票仍有原指纹，路由状态照旧为已知")

	account := &Account{ID: 8, Extra: map[string]any{openAICodexTicketExtraKey(ticket.Model): ticket}}
	status := OpenAICodexTicketStatus{Model: ticket.Model}
	applyCodexTicketRouteNodeStatus(&status, account)
	require.Empty(t, status.RouteNode)
	require.Empty(t, status.RouteMacroRegion)
	require.False(t, status.RouteCrossRegion, "没有节点时不判跨大区")
	route, _ := codexTicketRouteAffinityFromInventory(account, ticket.Model)
	require.Equal(t, "route_known", route)
}

func TestCodexTicketRouteNodeRespectsCookieProjection(t *testing.T) {
	value := encodeTestOAILBJWT(t, `{"host":"chat.gateway.unified-96.api.openai.com"}`)
	ticket := &openAICodexTicket{Cookies: []*http.Cookie{{Name: "__oailb", Value: value}}}
	node, ok := codexTicketRouteNode(ticket)
	require.True(t, ok)
	require.Equal(t, "JP", node.Country)
	ticket.CookieMode = CodexCookieStripRouting
	_, ok = codexTicketRouteNode(ticket)
	require.False(t, ok, "发送投影去掉路由 Cookie 后不再声明节点")
	_, ok = codexTicketRouteNode(nil)
	require.False(t, ok)
}

func TestApplyCodexTicketRouteNodeStatus(t *testing.T) {
	model := "gpt-5.6-codex"
	expires := time.Now().Add(time.Hour)
	ticketFor := func(host, country string) *openAICodexTicket {
		return &openAICodexTicket{Model: model, CredentialMode: config.CodexTicketCredentialCookie, ExpiresAt: expires,
			HarvestCountry: country, Cookies: []*http.Cookie{{Name: "__oailb", Value: encodeTestOAILBJWT(t, `{"host":"`+host+`"}`), Expires: expires}}}
	}
	account := &Account{ID: 7, Extra: map[string]any{openAICodexTicketExtraKey(model): ticketFor("chat.gateway.unified-39.api.openai.com", "DE")}}
	status := OpenAICodexTicketStatus{Model: model, RouteCrossRegion: true}
	applyCodexTicketRouteNodeStatus(&status, account)
	require.Equal(t, "unified-39", status.RouteNode)
	require.Equal(t, "ES", status.RouteNodeCountry)
	require.Equal(t, "Madrid", status.RouteNodeRegion)
	require.Equal(t, CodexMacroRegionEurope, status.RouteMacroRegion)
	require.Equal(t, "DE", status.RouteEgressCountry)
	require.False(t, status.RouteCrossRegion, "德国出口落到西班牙节点不是异常")

	account.Extra[openAICodexTicketExtraKey(model)] = ticketFor("chat.gateway.unified-121.api.openai.com", "JP")
	applyCodexTicketRouteNodeStatus(&status, account)
	require.Equal(t, "unified-121", status.RouteNode)
	require.True(t, status.RouteCrossRegion)

	delete(account.Extra, openAICodexTicketExtraKey(model))
	applyCodexTicketRouteNodeStatus(&status, account)
	require.Empty(t, status.RouteNode)
	require.False(t, status.RouteCrossRegion)
}
