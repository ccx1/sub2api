package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const codexOAILBCookieName = "__oailb"

// __oailb 是 base64url 编码的 JSON 路由元数据（非加密值），可能带有 exp。
// 只读取期限与声明字段，调用方不得记录原始 Cookie 值；无法解析时保持原有行为。
func decodeCodexOAILBCookie(value string) (map[string]any, time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 4096 {
		return nil, time.Time{}, errors.New("invalid __oailb length")
	}
	var decoded []byte
	var err error
	for _, encoding := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		if decoded, err = encoding.DecodeString(value); err == nil {
			break
		}
	}
	if err != nil {
		return nil, time.Time{}, errors.New("__oailb is not base64")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.UseNumber()
	var claims map[string]any
	if err := decoder.Decode(&claims); err != nil || claims == nil {
		return nil, time.Time{}, errors.New("__oailb is not a JSON object")
	}
	return claims, codexOAILBClaimExpiry(claims["exp"]), nil
}

func codexOAILBClaimExpiry(value any) time.Time {
	var seconds int64
	switch current := value.(type) {
	case json.Number:
		if parsed, err := current.Int64(); err == nil {
			seconds = parsed
		} else if parsed, err := current.Float64(); err == nil {
			seconds = int64(parsed)
		}
	case string:
		seconds, _ = strconv.ParseInt(strings.TrimSpace(current), 10, 64)
	default:
		return time.Time{}
	}
	// 与 STATE 时间戳相同的合理区间，拒绝毫秒值或损坏数据。
	if seconds < 1577836800 || seconds > 4102444800 {
		return time.Time{}
	}
	return time.Unix(seconds, 0).UTC()
}

// codexOAILBCookieExpiry 返回可解析的 __oailb exp；其它 Cookie 或解析失败返回零值。
func codexOAILBCookieExpiry(cookie *http.Cookie) time.Time {
	if cookie == nil || cookie.Name != codexOAILBCookieName {
		return time.Time{}
	}
	_, expires, err := decodeCodexOAILBCookie(cookie.Value)
	if err != nil {
		return time.Time{}
	}
	return expires
}

// codexTicketRouteExpiresAt 返回票据实际发送的 __oailb 中声明的路由到期时间。
func codexTicketRouteExpiresAt(ticket *openAICodexTicket) time.Time {
	if ticket == nil {
		return time.Time{}
	}
	var expires time.Time
	for _, cookie := range ticket.Cookies {
		if cookie != nil && codexCookieExcluded(normalizeCodexCookieMode(ticket.CookieMode), cookie.Name) {
			continue
		}
		expires = earlierCodexTicketExpiry(expires, codexOAILBCookieExpiry(cookie))
	}
	return expires
}

// codexOAILBClaimsForLog 只返回解码后的声明，用于诊断；不包含原始 Cookie。
func codexOAILBClaimsForLog(ticket *openAICodexTicket) map[string]any {
	if ticket == nil {
		return nil
	}
	for _, cookie := range ticket.Cookies {
		if cookie == nil || cookie.Name != codexOAILBCookieName {
			continue
		}
		if claims, _, err := decodeCodexOAILBCookie(cookie.Value); err == nil {
			return claims
		}
	}
	return nil
}
