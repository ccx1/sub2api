package openaiauth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

// OAuth 公共参数（对齐 customer_recovery.py 顶部常量）。
// 这些是官方公开 client 参数，可能随上游变化，集中于此便于维护。
const (
	authOrigin    = "https://auth.openai.com"
	oauthClient   = "app_EMoamEEZ73f0CkXaXp7hrann"
	oauthCallback = "http://localhost:1455/auth/callback"
)

// LoginError 携带稳定的错误码，便于上层区分「凭据错误 / 需人工介入 / 临时失败」。
type LoginError struct{ Code string }

func (e *LoginError) Error() string { return e.Code }

func loginErr(code string) *LoginError { return &LoginError{Code: code} }

// pkceChallenge 由 verifier 生成 S256 challenge。
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// authorizeURL 复刻 auth_url：始终携带原始 workspace。
func authorizeURL(workspaceID, state, verifier string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", oauthClient)
	q.Set("redirect_uri", oauthCallback)
	q.Set("scope", "openid profile email offline_access")
	q.Set("code_challenge", pkceChallenge(verifier))
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	q.Set("prompt", "login")
	q.Set("id_token_add_organizations", "true")
	q.Set("codex_cli_simplified_flow", "true")
	q.Set("allowed_workspace_id", workspaceID)
	return authOrigin + "/oauth/authorize?" + q.Encode()
}

// jwtClaims 解出 JWT 的 payload（不校验签名，仅用于读取 workspace / email）。
func jwtClaims(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, errors.New("invalid token document")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		// 兼容带 padding 的情况
		raw, err = base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, errors.New("invalid token document")
		}
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		return nil, errors.New("invalid token document")
	}
	return claims, nil
}

// workspaceFromCredentials 复刻 customer_recovery.workspace：
// 优先取 chatgpt_account_id / account_id，回退到 access_token 的 auth claim。
func workspaceFromCredentials(creds map[string]string) (string, error) {
	if v := strings.TrimSpace(creds["chatgpt_account_id"]); v != "" {
		return v, nil
	}
	if v := strings.TrimSpace(creds["account_id"]); v != "" {
		return v, nil
	}
	claims, err := jwtClaims(creds["access_token"])
	if err != nil {
		return "", errors.New("original workspace is missing")
	}
	if auth, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
		if v, ok := auth["chatgpt_account_id"].(string); ok && v != "" {
			return v, nil
		}
	}
	return "", errors.New("original workspace is missing")
}
