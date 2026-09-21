package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func sanitizeSharedCredentials(platform, kind string, in map[string]any) (map[string]any, error) {
	allowed := []string{"access_token", "refresh_token", "expires_at", "token_type", "scope", "id_token", "client_id",
		"chatgpt_account_id", "chatgpt_user_id", "account_id", "org_uuid", "account_uuid", "email", "email_address",
		"project_id", "oauth_type", "tier_id", "plan_type", "chatgpt_plan_type", "subscription_tier", "organization_id", "api_key"}
	result := map[string]any{}
	for _, key := range allowed {
		if value, ok := in[key]; ok {
			result[key] = value
		}
	}
	for _, key := range []string{"access_token", "refresh_token", "api_key"} {
		if value, ok := result[key]; ok {
			v, valid := value.(string)
			if !valid || len(v) > 32768 {
				return nil, infraerrors.BadRequest("INVALID_CREDENTIALS", "认证凭证格式无效")
			}
			result[key] = strings.TrimSpace(v)
		}
	}
	tokenKey := "access_token"
	if kind == AccountTypeAPIKey {
		tokenKey = "api_key"
	}
	value, _ := result[tokenKey].(string)
	if value == "" {
		return nil, infraerrors.BadRequest("CREDENTIALS_REQUIRED", "请完成账号授权或导入完整凭证")
	}
	if platform == PlatformAntigravity && kind != AccountTypeOAuth {
		return nil, infraerrors.BadRequest("OAUTH_REQUIRED", "Antigravity 共享账号需要 OAuth 授权")
	}
	return result, nil
}

func sharedCredentialFingerprint(platform, kind string, credentials map[string]any) (string, error) {
	keys := []string{"refresh_token", "access_token"}
	if kind == AccountTypeAPIKey {
		keys = []string{"api_key"}
	}
	for _, key := range keys {
		if v, ok := credentials[key].(string); ok && v != "" {
			sum := sha256.Sum256([]byte(platform + "\x00" + kind + "\x00" + v))
			return hex.EncodeToString(sum[:]), nil
		}
	}
	return "", infraerrors.BadRequest("CREDENTIALS_REQUIRED", "缺少有效凭证")
}

func (s *SharedPoolService) applyProxy(ctx context.Context, userID int64, raw *string, extra map[string]any) (*int64, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		extra[ProxyModeExtraKey] = "random"
		extra[RandomProxyEmptyPoolPolicyExtraKey] = RandomProxyEmptyPoolPolicyReject
		return nil, nil
	}
	proxy, err := parseSharedProxy(ctx, *raw)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(proxy.URL()))
	proxy, err = s.repo.CreateSharedProxy(ctx, userID, proxy, hex.EncodeToString(sum[:]))
	if err != nil {
		return nil, err
	}
	delete(extra, ProxyModeExtraKey)
	delete(extra, RandomProxyEmptyPoolPolicyExtraKey)
	return &proxy.ID, nil
}

func parseSharedProxy(ctx context.Context, raw string) (*Proxy, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	invalid := infraerrors.BadRequest("INVALID_PROXY", "请输入有效的 http 或 socks5 代理地址及端口")
	if err != nil || u.Hostname() == "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, invalid
	}
	switch u.Scheme {
	case "http", "socks5", "socks5h":
	default:
		return nil, invalid
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return nil, invalid
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", u.Hostname())
	if err != nil || len(ips) == 0 {
		return nil, infraerrors.BadRequest("INVALID_PROXY_HOST", "代理地址解析失败")
	}
	// 固定解析后的公网地址，避免校验后 DNS 重绑定访问本机或内网。
	for _, ip := range ips {
		if !sharedProxyPublicIP(ip) {
			return nil, infraerrors.BadRequest("PRIVATE_PROXY_FORBIDDEN", "用户代理必须使用公网地址")
		}
	}
	p := &Proxy{Name: "用户共享代理", Protocol: u.Scheme, Host: ips[0].String(), Port: port, Status: StatusActive}
	if u.User != nil {
		p.Username = u.User.Username()
		p.Password, _ = u.User.Password()
	}
	if len(p.Username) > 100 || len(p.Password) > 100 {
		return nil, invalid
	}
	return p, nil
}

func sharedProxyPublicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range []string{"100.64.0.0/10", "0.0.0.0/8", "192.0.0.0/24", "198.18.0.0/15"} {
		if netip.MustParsePrefix(cidr).Contains(ip) {
			return false
		}
	}
	return true
}

func (s *SharedPoolService) OAuthProxy(ctx context.Context, userID int64, raw *string) (*int64, error) {
	extra := map[string]any{}
	id, err := s.applyProxy(ctx, userID, raw, extra)
	if err != nil || id != nil {
		return id, err
	}
	account := &Account{Extra: extra}
	if err = ResolveRandomProxyFromSource(ctx, account, s.accounts); err != nil {
		return nil, fmt.Errorf("平台随机代理不可用: %w", err)
	}
	return account.ProxyID, nil
}

func (s *SharedPoolService) AccountOAuthProxy(ctx context.Context, userID, accountID int64, platform string, raw *string) (*int64, error) {
	if accountID <= 0 {
		return s.OAuthProxy(ctx, userID, raw)
	}
	_, account, err := s.OwnedAccount(ctx, userID, accountID)
	if err != nil {
		return nil, err
	}
	if account.Platform != platform {
		return nil, infraerrors.BadRequest("INVALID_PLATFORM", "授权平台与账号不一致")
	}
	if raw != nil {
		return s.OAuthProxy(ctx, userID, raw)
	}
	if account.IsRandomProxy() {
		if err = ResolveRandomProxyFromSource(ctx, account, s.accounts); err != nil {
			return nil, err
		}
	}
	return account.ProxyID, nil
}
