package openaiauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// login.go 移植 customer_auth.py 的 login 状态机：
// authorize/continue -> password/verify -> mfa issue+verify -> workspace/select -> oauth code -> token。
//
// 与 Python 版差异：sentinel token 由内置 SentinelSolver + turnstile VM 计算，无外部依赖。

var (
	mfaChallengeRe = regexp.MustCompile(`/mfa-challenge/([^/?#]+)`)
	factorIDRe     = regexp.MustCompile(`^[A-Za-z0-9_-]{1,180}$`)
	stateURLSafeRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

// LoginResult 是登录得到的原始 token 集合。
type LoginResult struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	ExpiresIn    int64
	// Raw 保留完整响应，便于写回额外字段。
	Raw map[string]any
}

// login 执行完整登录流程，返回 token 集合。
// email/password/totpSecret 为订单凭据；workspaceID 为目标空间；tr 为已建好的传输层。
func login(tr *transport, email, password, totpSecret, workspaceID, state, verifier string) (*LoginResult, error) {
	if password == "" || len(password) > 512 {
		return nil, loginErr("password_required")
	}
	if err := ValidateTOTPSecret(totpSecret); err != nil {
		return nil, loginErr("invalid_totp_secret")
	}

	var data map[string]any
	selected, passwordSent, mfaSent := false, false, false

	// device id 取 cookie oai-did，缺失则随机。
	deviceID := cookieValue(tr, "oai-did")
	if deviceID == "" {
		deviceID = randomUUID()
	}
	solver := newSentinelSolver(deviceID, tr.userAgent, sentinelGeo{TZ: "UTC", Lang: "en-US", Langs: "en-US,en"})

	current := authorizeURL(workspaceID, state, verifier)
	var err error
	current, err = followRedirects(tr, current)
	if err != nil {
		return nil, err
	}

	post := func(path string, body map[string]any, flow string) (map[string]any, error) {
		headers := map[string]string{
			"Accept":       "application/json",
			"Content-Type": "application/json",
			"Origin":       authOrigin,
			"User-Agent":   tr.userAgent,
		}
		if strings.HasPrefix(current, authOrigin+"/") {
			headers["Referer"] = current
		} else {
			headers["Referer"] = authOrigin + "/log-in"
		}
		if flow != "" {
			token, err := solver.BuildToken(tr.post, flow)
			if tr.ctx != nil && tr.ctx.Err() != nil {
				return nil, tr.ctx.Err()
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			if err != nil || token == "" {
				return nil, loginErr("security_verification_unavailable")
			}
			headers["openai-sentinel-token"] = token
		}
		payload, _ := json.Marshal(body)
		req, err := tr.newRequest(http.MethodPost, authOrigin+path, strings.NewReader(string(payload)))
		if err != nil {
			return nil, err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := tr.do(req)
		if err != nil {
			return nil, err
		}
		return responseData(resp)
	}

	for i := 0; i < 14; i++ {
		parsed, _ := url.Parse(current)
		if isCallback(parsed) {
			query := parsed.Query()
			if query.Get("state") != state {
				return nil, loginErr("oauth_state_mismatch")
			}
			code := query.Get("code")
			if code == "" {
				return nil, loginErr("oauth_code_missing")
			}
			return exchangeCode(tr, code, verifier)
		}
		if isMFA(data, current) {
			if mfaSent {
				return nil, loginErr("mfa_not_advanced")
			}
			factor := mfaFactor(data, current)
			if !factorIDRe.MatchString(factor) {
				return nil, loginErr("mfa_factor_missing")
			}
			if _, err := post("/api/accounts/mfa/issue_challenge",
				map[string]any{"id": factor, "type": "totp", "force_fresh_challenge": false}, ""); err != nil {
				return nil, err
			}
			code, err := TOTP(totpSecret)
			if err != nil {
				return nil, loginErr("invalid_totp_secret")
			}
			data, err = post("/api/accounts/mfa/verify",
				map[string]any{"id": factor, "type": "totp", "code": code}, "password_verify")
			if err != nil {
				return nil, err
			}
			mfaSent = true
			current = nextURL(data, current)
			continue
		}
		if strings.Contains(current, "log-in/password") {
			if passwordSent {
				return nil, loginErr("password_not_advanced")
			}
			data, err = post("/api/accounts/password/verify", map[string]any{"password": password}, "password_verify")
			if err != nil {
				return nil, err
			}
			passwordSent = true
			current = nextURL(data, current)
			continue
		}
		if strings.Contains(current, "email-verification") {
			return nil, loginErr("additional_email_verification_required")
		}
		if strings.Contains(current, "add-phone") || strings.Contains(current, "about-you") {
			return nil, loginErr("additional_verification_required")
		}
		if strings.Contains(current, "workspace") || strings.Contains(current, "consent") {
			if selected {
				return nil, loginErr("workspace_not_advanced")
			}
			data, err = post("/api/accounts/workspace/select", map[string]any{"workspace_id": workspaceID}, "")
			if err != nil {
				return nil, err
			}
			selected = true
			current, err = followRedirects(tr, joinURL(authOrigin, nextURL(data, "")))
			if err != nil {
				return nil, err
			}
			continue
		}
		if strings.Contains(current, "oauth2/auth") || strings.Contains(current, "oauth/authorize") {
			current, err = followRedirects(tr, joinURL(authOrigin, current))
			if err != nil {
				return nil, err
			}
			continue
		}
		if passwordSent || mfaSent {
			return nil, loginErr("unexpected_login_step")
		}
		data, err = post("/api/accounts/authorize/continue",
			map[string]any{"username": map[string]any{"value": email, "kind": "email"}}, "authorize_continue")
		if err != nil {
			return nil, err
		}
		current = nextURL(data, current)
	}
	return nil, loginErr("login_step_limit")
}

// exchangeCode 用授权码换取 token。
func exchangeCode(tr *transport, code, verifier string) (*LoginResult, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", oauthClient)
	form.Set("redirect_uri", oauthCallback)
	form.Set("code", code)
	form.Set("code_verifier", verifier)
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
	if result.AccessToken == "" || result.RefreshToken == "" {
		return nil, loginErr("incomplete_tokens")
	}
	return result, nil
}

// followRedirects 手工跟随最多 12 次重定向，遇到本地回调即返回。
func followRedirects(tr *transport, current string) (string, error) {
	for i := 0; i < 12; i++ {
		parsed, err := url.Parse(current)
		if err != nil {
			return "", loginErr("unexpected_auth_response")
		}
		if isCallback(parsed) {
			return current, nil
		}
		req, err := tr.newRequest(http.MethodGet, current, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Accept", "text/html")
		resp, err := tr.do(req)
		if err != nil {
			return "", err
		}
		status := resp.StatusCode
		location := resp.Header.Get("Location")
		finalURL := resp.Request.URL.String()
		_ = resp.Body.Close()
		if status == 301 || status == 302 || status == 303 || status == 307 || status == 308 {
			current = joinURL(current, location)
			continue
		}
		if status != 200 {
			// 复用 responseData 的错误映射（需要重新请求？此处直接映射状态码）
			return "", mapHTTPError(status, nil)
		}
		if finalURL != "" {
			return finalURL, nil
		}
		return current, nil
	}
	return "", loginErr("too_many_auth_redirects")
}

// isCallback 判断是否是本地 OAuth 回调地址。
func isCallback(u *url.URL) bool {
	if u == nil {
		return false
	}
	return u.Scheme == "http" && (u.Host == "localhost:1455" || u.Host == "127.0.0.1:1455") && u.Path == "/auth/callback"
}

// responseData 复刻 customer_auth.response_data：200 才返回 JSON，否则映射错误码。
func responseData(resp *http.Response) (map[string]any, error) {
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.Request != nil && resp.Request.Context().Err() != nil {
		return nil, resp.Request.Context().Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	if err != nil {
		return nil, errNetwork
	}
	if resp.StatusCode != 200 {
		return nil, mapHTTPError(resp.StatusCode, raw)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, loginErr("unexpected_auth_response")
	}
	return value, nil
}

// mapHTTPError 把非 200 响应映射为稳定错误码。
func mapHTTPError(status int, raw []byte) error {
	known := map[string]bool{
		"invalid_password": true, "invalid_otp": true, "invalid_totp": true,
		"account_deactivated": true, "account_disabled": true,
		"unsupported_country_region_territory": true,
	}
	if len(raw) > 0 {
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &body) == nil && known[body.Error.Code] {
			return loginErr(body.Error.Code)
		}
	}
	return loginErr("auth_http_" + itoa(status))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// nextURL 复刻 next_url：从多层 payload 里取 continue_url / url。
func nextURL(data map[string]any, fallback string) string {
	containers := []map[string]any{data}
	if page, ok := data["page"].(map[string]any); ok {
		containers = append(containers, page)
	}
	for _, c := range containers {
		var nested map[string]any
		if p, ok := c["payload"].(map[string]any); ok {
			nested = p
		}
		for _, key := range []string{"continue_url", "url"} {
			if v, ok := c[key].(string); ok && v != "" {
				return v
			}
			if nested != nil {
				if v, ok := nested[key].(string); ok && v != "" {
					return v
				}
			}
		}
	}
	return fallback
}

// isMFA 判断当前是否处于 MFA 挑战步骤。
func isMFA(data map[string]any, current string) bool {
	if strings.Contains(current, "mfa-challenge") || strings.Contains(current, "mfa_challenge") {
		return true
	}
	if page, ok := data["page"].(map[string]any); ok {
		kind := firstStr(page["type"], page["name"])
		if kind == "mfa_challenge" || kind == "mfa-challenge" {
			return true
		}
	}
	kind := firstStr(data["type"], data["page_type"])
	return kind == "mfa_challenge" || kind == "mfa-challenge"
}

// mfaFactor 复刻 mfa_factor：从多层结构或 URL 里解析 factor id。
func mfaFactor(data map[string]any, current string) string {
	objs := []map[string]any{data}
	if page, ok := data["page"].(map[string]any); ok {
		objs = append(objs, page)
		if payload, ok := page["payload"].(map[string]any); ok {
			objs = append(objs, payload)
		}
	}
	for _, obj := range objs {
		if v, ok := obj["factor_id"].(string); ok && v != "" {
			return v
		}
		for _, key := range []string{"factors", "mfa_factors", "mfa_challenge_factors"} {
			list, ok := obj[key].([]any)
			if !ok {
				continue
			}
			for _, item := range list {
				factor, ok := item.(map[string]any)
				if !ok {
					continue
				}
				ftype := firstStr(factor["type"], factor["factor_type"])
				if ftype == "totp" || ftype == "authenticator" {
					if id := firstStr(factor["id"], factor["factor_id"]); id != "" {
						return id
					}
				}
			}
		}
	}
	if m := mfaChallengeRe.FindStringSubmatch(current); len(m) == 2 {
		return m[1]
	}
	return ""
}

// toLoginResult 从 token map 构造结果。
func toLoginResult(tokens map[string]any) *LoginResult {
	r := &LoginResult{Raw: tokens}
	if v, ok := tokens["access_token"].(string); ok {
		r.AccessToken = v
	}
	if v, ok := tokens["refresh_token"].(string); ok {
		r.RefreshToken = v
	}
	if v, ok := tokens["id_token"].(string); ok {
		r.IDToken = v
	}
	switch v := tokens["expires_in"].(type) {
	case float64:
		r.ExpiresIn = int64(v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			r.ExpiresIn = n
		}
	}
	return r
}

func firstStr(vals ...any) string {
	for _, v := range vals {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func joinURL(base, ref string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

func cookieValue(tr *transport, name string) string {
	if tr.client.Jar == nil {
		return ""
	}
	for _, host := range []string{"https://auth.openai.com", "https://sentinel.openai.com"} {
		u, _ := url.Parse(host)
		for _, c := range tr.client.Jar.Cookies(u) {
			if c.Name == name && c.Value != "" {
				return c.Value
			}
		}
	}
	return ""
}
