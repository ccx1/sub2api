package service

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// 只解析协议元数据；旧格式解析失败时仍使用原先保存的期限。
type codexTicketStateMetadata struct {
	Blocks   int
	IssuedAt time.Time
}

const (
	codexTicketStateHeaderLength = 57
	codexTicketStateBlockLength  = 16
	codexTicketStateMaxLength    = 8192
)

// 时间戳来自上游返回的 Fernet 格式头，不解密 STATE 内容。
func parseOpenAICodexTicketStateMetadata(state string) (codexTicketStateMetadata, error) {
	state = strings.TrimSpace(state)
	if state == "" || len(state) > codexTicketStateMaxLength {
		return codexTicketStateMetadata{}, errors.New("invalid codex turn-state length")
	}

	padding := 0
	for i := len(state) - 1; i >= 0 && state[i] == '='; i-- {
		padding++
	}
	if padding > 2 {
		return codexTicketStateMetadata{}, errors.New("invalid codex turn-state padding")
	}
	for i := 0; i < len(state)-padding; i++ {
		b := state[i]
		if b == '=' || b <= ' ' || !(b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '-' || b == '_') {
			return codexTicketStateMetadata{}, fmt.Errorf("invalid codex turn-state character at %d", i)
		}
	}
	encoding := base64.RawURLEncoding.Strict()
	if padding > 0 {
		encoding = base64.URLEncoding.Strict()
	}
	raw, err := encoding.DecodeString(state)
	if err != nil {
		return codexTicketStateMetadata{}, fmt.Errorf("decode codex turn-state: %w", err)
	}
	if len(raw) < codexTicketStateHeaderLength+codexTicketStateBlockLength ||
		(len(raw)-codexTicketStateHeaderLength)%codexTicketStateBlockLength != 0 || raw[0] != 0x80 {
		return codexTicketStateMetadata{}, errors.New("invalid codex turn-state payload")
	}
	issuedUnix := binary.BigEndian.Uint64(raw[1:9])
	// 拒绝明显损坏的时间戳，同时保留历史票据的可解析性。
	if issuedUnix < 1577836800 || issuedUnix > 4102444800 {
		return codexTicketStateMetadata{}, errors.New("invalid codex turn-state issue time")
	}
	return codexTicketStateMetadata{
		Blocks:   (len(raw) - codexTicketStateHeaderLength) / codexTicketStateBlockLength,
		IssuedAt: time.Unix(int64(issuedUnix), 0).UTC(),
	}, nil
}

func codexTicketStateExpiresAt(metadata codexTicketStateMetadata) time.Time {
	if metadata.IssuedAt.IsZero() {
		return time.Time{}
	}
	// 按参考实现的一小时上限估算，预留请求跨越过期边界的安全窗口。
	return metadata.IssuedAt.Add(time.Hour - 30*time.Second)
}

func hydrateCodexTicketStateExpiry(ticket *openAICodexTicket) {
	if ticket == nil {
		return
	}
	hydrateCodexTicketLeafStateExpiry(ticket)
	hydrateCodexTicketStateExpiry(ticket.Standby)
	for _, reserve := range ticket.Reserve {
		hydrateCodexTicketStateExpiry(reserve)
	}
}

func hydrateCodexTicketLeafStateExpiry(ticket *openAICodexTicket) {
	ticket.IssuedAt, ticket.StateExpiresAt = time.Time{}, time.Time{}
	if ticket.CredentialMode == config.CodexTicketCredentialCookie {
		return
	}
	if metadata, err := parseOpenAICodexTicketStateMetadata(ticket.State); err == nil {
		// 只有协议期限会覆盖旧 ExpiresAt 时，才需保留旧配置期限。
		// 解析失败的旧票仍由 needsRefresh 和调度器按需计算软复验时间。
		if ticket.RevalidateAt.IsZero() {
			ticket.RevalidateAt = ticket.ExpiresAt
		}
		ticket.IssuedAt = metadata.IssuedAt
		ticket.StateExpiresAt = codexTicketStateExpiresAt(metadata)
		if ticket.usesCookies() {
			ticket.ExpiresAt = earlierCodexTicketExpiry(ticket.StateExpiresAt, codexTicketCookieExpiry(ticket))
		} else {
			ticket.ExpiresAt = ticket.StateExpiresAt
		}
	}
}

// 发布期限与实际 Cookie 期限都约束凭据包；Cookie 更新不能延长 STATE。
func codexTicketHardExpiry(ticket *openAICodexTicket) time.Time {
	if ticket == nil {
		return time.Time{}
	}
	if !ticket.usesCookies() {
		if !ticket.StateExpiresAt.IsZero() {
			return ticket.StateExpiresAt
		}
		return ticket.ExpiresAt
	}
	expires := earlierCodexTicketExpiry(ticket.ExpiresAt, codexTicketCookieExpiry(ticket))
	if ticket.CredentialMode == config.CodexTicketCredentialCookieState {
		expires = earlierCodexTicketExpiry(expires, ticket.StateExpiresAt)
	}
	return expires
}

func codexTicketCookieExpiry(ticket *openAICodexTicket) time.Time {
	var expires time.Time
	for _, cookie := range ticket.Cookies {
		if cookie == nil {
			continue
		}
		deadline := cookie.Expires
		// 旧快照里的会话 Cookie 没有实际期限，保守使用原先的配置期限。
		if deadline.IsZero() {
			deadline = ticket.ExpiresAt
		}
		expires = earlierCodexTicketExpiry(expires, deadline)
	}
	if expires.IsZero() {
		return ticket.ExpiresAt
	}
	return expires
}

func earlierCodexTicketExpiry(first, second time.Time) time.Time {
	if first.IsZero() || !second.IsZero() && second.Before(first) {
		return second
	}
	return first
}
