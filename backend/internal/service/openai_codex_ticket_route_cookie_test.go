package service

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func encodeTestOAILB(t *testing.T, payload string) string {
	t.Helper()
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func TestDecodeCodexOAILBCookie(t *testing.T) {
	exp := time.Now().Add(4 * time.Minute).Unix()
	claims, expires, err := decodeCodexOAILBCookie(encodeTestOAILB(t, `{"cluster":"a1","exp":`+jsonInt(exp)+`}`))
	require.NoError(t, err)
	require.Equal(t, "a1", claims["cluster"])
	require.Equal(t, time.Unix(exp, 0).UTC(), expires)

	// 字符串形式的 exp 同样可用；缺少或非法 exp 时仅返回声明。
	_, expires, err = decodeCodexOAILBCookie(encodeTestOAILB(t, `{"exp":"`+jsonInt(exp)+`"}`))
	require.NoError(t, err)
	require.Equal(t, time.Unix(exp, 0).UTC(), expires)
	_, expires, err = decodeCodexOAILBCookie(encodeTestOAILB(t, `{"exp":1700000000000}`))
	require.NoError(t, err)
	require.True(t, expires.IsZero())

	for _, value := range []string{"", "opaque-random-value!", encodeTestOAILB(t, `[1,2]`)} {
		_, expires, err = decodeCodexOAILBCookie(value)
		require.Error(t, err)
		require.True(t, expires.IsZero())
	}
}

func TestNormalizeCodexTicketCookieBoundsRouteCookieByExp(t *testing.T) {
	now := time.Now()
	u, _ := url.Parse(chatgptCodexURL)
	exp := now.Add(2 * time.Minute).Truncate(time.Second)
	value := encodeTestOAILB(t, `{"exp":`+jsonInt(exp.Unix())+`}`)

	cookie, ok := normalizeCodexTicketCookie(http.Cookie{Name: "__oailb", Value: value, Path: "/", MaxAge: 3600}, u, now)
	require.True(t, ok)
	require.True(t, cookie.Expires.Equal(exp))

	// 会话 Cookie 保持会话语义，不被改写为持久 Cookie。
	cookie, ok = normalizeCodexTicketCookie(http.Cookie{Name: "__oailb", Value: value, Path: "/"}, u, now)
	require.True(t, ok)
	require.True(t, cookie.Expires.IsZero())

	// exp 已过：按过期 Cookie 处理。
	expired := encodeTestOAILB(t, `{"exp":`+jsonInt(now.Add(-time.Minute).Unix())+`}`)
	cookie, ok = normalizeCodexTicketCookie(http.Cookie{Name: "__oailb", Value: expired, Path: "/"}, u, now)
	require.True(t, ok)
	require.False(t, cookie.Expires.After(now))

	// 解析不了的值保持现状。
	cookie, ok = normalizeCodexTicketCookie(http.Cookie{Name: "__oailb", Value: "opaque", Path: "/", MaxAge: 3600}, u, now)
	require.True(t, ok)
	require.WithinDuration(t, now.Add(time.Hour), cookie.Expires, time.Second)
}

func TestCodexTicketCookieSnapshotUsesRouteExpForSessionCookie(t *testing.T) {
	now := time.Now()
	exp := now.Add(90 * time.Second).Truncate(time.Second)
	jar := newOpenAICodexTicketCookieJar()
	u, _ := url.Parse(chatgptCodexURL)
	jar.SetCookies(u, []*http.Cookie{
		{Name: "__oailb", Value: encodeTestOAILB(t, `{"exp":`+jsonInt(exp.Unix())+`}`), Path: "/"},
		{Name: "other", Value: "v", Path: "/"},
	})
	cookies, expires := snapshotOpenAICodexTicketCookies(jar, time.Hour)
	require.Len(t, cookies, 2)
	require.True(t, expires.Equal(exp))
	ticket := &openAICodexTicket{Cookies: cookies}
	require.True(t, codexTicketRouteExpiresAt(ticket).Equal(exp))
	require.NotNil(t, codexOAILBClaimsForLog(ticket))

	// 无法解析时沿用 Cookie TTL 兜底。
	jar = newOpenAICodexTicketCookieJar()
	jar.SetCookies(u, []*http.Cookie{{Name: "__oailb", Value: "opaque", Path: "/"}})
	_, expires = snapshotOpenAICodexTicketCookies(jar, time.Hour)
	require.WithinDuration(t, now.Add(time.Hour), expires, 2*time.Second)
}

func TestCodexModelQualityRouteDeadline(t *testing.T) {
	now := time.Now()
	until := now.Add(30 * time.Second)
	exp := now.Add(10 * time.Second).Truncate(time.Second)
	ticket := &openAICodexTicket{Cookies: []*http.Cookie{{Name: "__oailb", Value: encodeTestOAILB(t, `{"exp":`+jsonInt(exp.Unix())+`}`), Expires: exp}}}
	require.True(t, codexModelQualityRouteDeadline(ticket, until).Equal(exp.Add(-time.Second)))
	opaque := &openAICodexTicket{Cookies: []*http.Cookie{{Name: "__oailb", Value: "opaque", Expires: exp}}}
	require.True(t, codexModelQualityRouteDeadline(opaque, until).Equal(until))
	// strip_routing 模式不发送 __oailb，不受路由租约约束。
	ticket.CookieMode = CodexCookieStripRouting
	require.True(t, codexModelQualityRouteDeadline(ticket, until).Equal(until))
}

func jsonInt(value int64) string {
	return strconv.FormatInt(value, 10)
}
