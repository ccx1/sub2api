package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openaiauth"
)

// resolveAccountProxyURL 解析账号绑定的代理地址（本站授权时用于「一个账号一条 IP」）。
// 若账号未绑定固定代理，返回空串，由上层决定是否直连。
func (s *AccountTokenGuardService) resolveAccountProxyURL(ctx context.Context, account *Account) string {
	if account == nil || account.ProxyID == nil {
		return ""
	}
	if account.Proxy != nil {
		return account.Proxy.URL()
	}
	if s != nil && s.proxyRepo != nil {
		if proxy, err := s.proxyRepo.GetByID(ctx, *account.ProxyID); err == nil && proxy != nil {
			account.Proxy = proxy
			return proxy.URL()
		}
	}
	return ""
}

// accountCredentialStrings 把账号凭据展开成 openaiauth 需要的 string map。
func accountCredentialStrings(account *Account) map[string]string {
	out := make(map[string]string, 8)
	if account == nil || account.Credentials == nil {
		return out
	}
	for _, key := range []string{"access_token", "refresh_token", "id_token", "account_id", "workspace_id", "expires_at"} {
		if v := account.GetCredential(key); v != "" {
			out[key] = v
		}
	}
	return out
}

// probeBuiltin 使用进程内 OAuth 令牌直接测活（本站授权），走账号绑定的代理 IP。
func (s *AccountTokenGuardService) probeBuiltin(ctx context.Context, cfg AccountTokenGuardConfig, account *Account) AccountTokenGuardProbeResult {
	_ = cfg
	token := strings.TrimSpace(account.GetCredential("access_token"))
	if token == "" {
		return AccountTokenGuardProbeResult{State: AccountTokenGuardProbeAuth, Detail: "账号没有 access_token"}
	}
	proxyURL := s.resolveAccountProxyURL(ctx, account)
	started := time.Now()
	result := openaiauth.Probe(accountCredentialStrings(account), proxyURL)
	latency := int(time.Since(started).Milliseconds())
	switch {
	case result.Active:
		return AccountTokenGuardProbeResult{State: AccountTokenGuardProbeOK, Detail: firstNonEmptyGuard(result.Detail, "active") + " " + strconv.Itoa(latency) + "ms", LatencyMS: latency}
	case result.AuthFail:
		return AccountTokenGuardProbeResult{State: AccountTokenGuardProbeAuth, Detail: truncateGuardText(firstNonEmptyGuard(result.Detail, "令牌失效"), 160), LatencyMS: latency}
	default:
		return AccountTokenGuardProbeResult{State: AccountTokenGuardProbeTransient, Detail: truncateGuardText(firstNonEmptyGuard(result.Detail, "临时异常"), 160), LatencyMS: latency}
	}
}

// reloginBuiltin 使用进程内 OAuth 完成重登（本站授权），返回新的凭据集合（键与外站重登一致）。
func (s *AccountTokenGuardService) reloginBuiltin(ctx context.Context, cfg AccountTokenGuardConfig, entry AccountTokenGuardReloginAccount, account *Account) (map[string]any, error) {
	_ = cfg
	if entry.Password == "" {
		return nil, errors.New("缺少密码，无法本站授权")
	}
	proxyURL := s.resolveAccountProxyURL(ctx, account)
	existing := accountCredentialStrings(account)
	workspaceID := ""
	if account != nil {
		workspaceID = strings.TrimSpace(account.GetCredential("workspace_id"))
	}
	creds, err := openaiauth.Login(openaiauth.LoginInput{
		Email:         entry.Email,
		Password:      entry.Password,
		TOTPSecret:    entry.MFASecret,
		WorkspaceID:   workspaceID,
		ExistingCreds: existing,
		ProxyURL:      proxyURL,
	})
	if err != nil {
		return nil, err
	}
	if creds == nil || creds.AccessToken == "" || creds.RefreshToken == "" {
		return nil, errors.New("本站授权返回的凭据不完整")
	}
	out := map[string]any{
		"access_token":  creds.AccessToken,
		"refresh_token": creds.RefreshToken,
	}
	if creds.IDToken != "" {
		out["id_token"] = creds.IDToken
	}
	if creds.ExpiresAt > 0 {
		out["expires_at"] = creds.ExpiresAt
	}
	return out, nil
}
