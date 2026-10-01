package openaiauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// client.go 暴露给业务层的入口：站内自助 OAuth 登录 / 刷新 / 测活。
// 全部在进程内完成，不依赖 Python / Node / 外部重登服务。

// Credentials 是登录 / 刷新成功后回写账号的凭据集合。
type Credentials struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
}

// LoginInput 描述一次站内重登所需的全部信息。
type LoginInput struct {
	Email       string
	Password    string
	TOTPSecret  string
	WorkspaceID string // 目标空间；为空时从 ExistingCreds 推断
	// ExistingCreds 是账号当前凭据（用于推断 workspace）。
	ExistingCreds map[string]string
	// ProxyURL 为该账号分配的 IP 池代理（1 账号 1 IP）；为空则直连。
	ProxyURL string
}

// Login 执行站内 OAuth 登录，返回新凭据。
func Login(in LoginInput) (*Credentials, error) {
	return LoginContext(context.Background(), in)
}

// LoginContext 将巡检任务的取消与时间预算传递到每一步授权请求。
func LoginContext(ctx context.Context, in LoginInput) (*Credentials, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	workspaceID := strings.TrimSpace(in.WorkspaceID)
	if workspaceID == "" {
		var err error
		workspaceID, err = workspaceFromCredentials(in.ExistingCreds)
		if err != nil {
			return nil, err
		}
	}
	tr, err := newTransport(in.ProxyURL)
	if err != nil {
		return nil, err
	}
	tr.ctx = ctx
	defer tr.close()

	state := randToken(32)
	verifier := randToken(64)
	result, err := login(tr, strings.ToLower(strings.TrimSpace(in.Email)), in.Password, in.TOTPSecret, workspaceID, state, verifier)
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, contextErr
	}
	if err != nil {
		return nil, err
	}
	return credentialsFromLogin(result), nil
}

// Refresh 用 refresh_token 换取新凭据（不需要密码 / 2FA）。
func Refresh(existing map[string]string, proxyURL string) (*Credentials, error) {
	token := strings.TrimSpace(existing["refresh_token"])
	if token == "" {
		return nil, errors.New("no refresh token; use password login")
	}
	tr, err := newTransport(proxyURL)
	if err != nil {
		return nil, err
	}
	defer tr.close()

	clientID := strings.TrimSpace(existing["client_id"])
	if clientID == "" {
		clientID = oauthClient
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", clientID)
	form.Set("refresh_token", token)
	req, err := tr.newRequest(http.MethodPost, authOrigin+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := tr.do(req)
	if err != nil {
		return nil, err
	}
	tokens, err := responseData(resp)
	if err != nil {
		return nil, err
	}
	result := toLoginResult(tokens)
	if result.RefreshToken == "" {
		result.RefreshToken = token
	}
	if result.AccessToken == "" {
		return nil, errors.New("refresh returned no access token")
	}
	return credentialsFromLogin(result), nil
}

// ProbeResult 是站内测活结果：Active 表示令牌可用。
type ProbeResult struct {
	Active   bool
	AuthFail bool // true 表示令牌失效（需重登），否则为临时异常
	Detail   string
}

// Probe 站内测活：用 access_token 调 chatgpt.com usage 接口核验令牌与空间。
// 与 customer_recovery.verified_account 的 usage 核验一致，但仅判断可用性。
func Probe(existing map[string]string, proxyURL string) ProbeResult {
	return ProbeContext(context.Background(), existing, proxyURL)
}

// ProbeContext 保留令牌失效分类，取消与超时仅作为临时异常。
func ProbeContext(ctx context.Context, existing map[string]string, proxyURL string) ProbeResult {
	if err := ctx.Err(); err != nil {
		return ProbeResult{Detail: err.Error()}
	}
	access := strings.TrimSpace(existing["access_token"])
	if access == "" {
		return ProbeResult{AuthFail: true, Detail: "missing access_token"}
	}
	workspaceID, err := workspaceFromCredentials(existing)
	if err != nil {
		return ProbeResult{AuthFail: true, Detail: "missing workspace"}
	}
	// usage 接口在 chatgpt.com，不属于受限 auth 源，用独立客户端（带 TLS 指纹 + 代理）。
	tr, err := newUsageTransport(proxyURL)
	if err != nil {
		return ProbeResult{Detail: "transport: " + err.Error()}
	}
	defer tr.close()
	return tr.probeUsage(ctx, access, workspaceID)
}

func (tr *transport) probeUsage(ctx context.Context, access, workspaceID string) ProbeResult {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://chatgpt.com/backend-api/wham/usage", nil)
	if err != nil {
		return ProbeResult{Detail: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("ChatGPT-Account-Id", workspaceID)
	req.Header.Set("User-Agent", tr.userAgent)
	req.Header.Set("Accept", "application/json")
	ctx, cancel := reqContext(req, 30*time.Second)
	defer cancel()
	resp, err := tr.client.Do(req.WithContext(ctx))
	if err != nil {
		return ProbeResult{Detail: "usage request failed"}
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || ctx.Err() != nil {
		return ProbeResult{Detail: "usage response incomplete"}
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return ProbeResult{AuthFail: true, Detail: "usage " + strconv.Itoa(resp.StatusCode)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ProbeResult{Detail: "usage " + strconv.Itoa(resp.StatusCode)}
	}
	var body struct {
		AccountID string `json:"account_id"`
	}
	_ = json.Unmarshal(raw, &body)
	if body.AccountID != "" && body.AccountID != workspaceID {
		return ProbeResult{AuthFail: true, Detail: "account mismatch"}
	}
	return ProbeResult{Active: true, Detail: "active"}
}

func credentialsFromLogin(result *LoginResult) *Credentials {
	c := &Credentials{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		IDToken:      result.IDToken,
		ExpiresIn:    result.ExpiresIn,
	}
	// expires_at：优先取 access_token 的 exp，回退 now+expires_in。
	if claims, err := jwtClaims(result.AccessToken); err == nil {
		if exp, ok := claims["exp"].(float64); ok && exp > 0 {
			c.ExpiresAt = int64(exp)
		}
	}
	if c.ExpiresAt == 0 {
		expiresIn := result.ExpiresIn
		if expiresIn == 0 {
			expiresIn = 3600
		}
		c.ExpiresAt = time.Now().Unix() + expiresIn
	}
	return c
}

func randToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
