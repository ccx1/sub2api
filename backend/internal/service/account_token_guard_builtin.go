package service

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openaiauth"
)

// 代理选择只修改请求副本；不能把随机出口写成账号的固定代理。
func (s *AccountTokenGuardService) resolveAccountProxyURL(ctx context.Context, account *Account) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if account == nil {
		return "", errors.New("凭证守护账号不存在")
	}
	requestAccount := *account
	requestAccount.Extra, requestAccount.Credentials = maps.Clone(account.Extra), maps.Clone(account.Credentials)
	if err := ResolveRandomProxyFromSource(ctx, &requestAccount, s.accounts); err != nil {
		if disableErr := DisableRandomProxyAccountOnUnavailable(ctx, &requestAccount, s.accounts, err); disableErr != nil {
			return "", fmt.Errorf("%w; disable random proxy account: %v", err, disableErr)
		}
		return "", err
	}
	if requestAccount.ProxyID == nil {
		return "", nil
	}
	if requestAccount.Proxy == nil || requestAccount.Proxy.ID != *requestAccount.ProxyID {
		if s.proxyRepo == nil {
			return "", errors.New("凭证守护代理仓储不可用")
		}
		proxy, err := s.proxyRepo.GetByID(ctx, *requestAccount.ProxyID)
		if err != nil {
			return "", err
		}
		requestAccount.Proxy = proxy
	}
	proxy := requestAccount.Proxy
	if proxy == nil || proxy.ID != *requestAccount.ProxyID || !proxy.IsActive() || proxy.IsExpired(time.Now()) {
		return "", ErrRandomProxyUnavailable
	}
	return proxy.URL(), nil
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
	probeCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.ProbeTimeoutSeconds)*time.Second)
	defer cancel()
	token := strings.TrimSpace(account.GetCredential("access_token"))
	if token == "" {
		return AccountTokenGuardProbeResult{State: AccountTokenGuardProbeAuth, Detail: "账号没有 access_token"}
	}
	proxyURL, err := s.resolveAccountProxyURL(probeCtx, account)
	if err != nil {
		diagnostic := AccountTokenGuardDiagnostic{Code: "proxy_unavailable"}
		if probeCtx.Err() != nil {
			diagnostic.Code = guardHTTPErrorCode(probeCtx, err)
		}
		return AccountTokenGuardProbeResult{State: AccountTokenGuardProbeTransient, Detail: formatGuardDiagnostic(diagnostic), Diagnostic: diagnostic}
	}
	started := time.Now()
	result := openaiauth.ProbeContext(probeCtx, accountCredentialStrings(account), proxyURL)
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
	loginCtx, cancel := context.WithTimeout(ctx, 25*time.Minute)
	defer cancel()
	if entry.Password == "" {
		return nil, errors.New("缺少密码，无法本站授权")
	}
	proxyURL, err := s.resolveAccountProxyURL(loginCtx, account)
	if err != nil {
		return nil, err
	}
	existing := accountCredentialStrings(account)
	workspaceID := ""
	if account != nil {
		workspaceID = strings.TrimSpace(account.GetCredential("workspace_id"))
	}
	creds, err := openaiauth.LoginContext(loginCtx, openaiauth.LoginInput{
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
