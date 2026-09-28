package service

import (
	"errors"
	"strings"
)

// 外站 code 可能夹带凭据；只保留固定协议码，未知内容不能进入状态、日志或通知。
func newGuardReloginProtocolError(value any) error {
	code, ok := value.(string)
	if !ok || len(code) > 64 {
		return errors.New("relogin_rejected")
	}
	code = strings.ToLower(strings.TrimSpace(code))
	switch code {
	case "auth_failed", "invalid_token", "token_invalid", "requires_relogin",
		"invalid_grant", "revoked", "relogin_rejected":
		return errors.New(code)
	default:
		return errors.New("relogin_rejected")
	}
}
