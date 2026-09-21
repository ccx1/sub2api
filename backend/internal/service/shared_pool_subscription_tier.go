package service

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

func NormalizeSharedPoolTierOverride(platform, kind, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	if tier := canonicalSharedSubscriptionTier(platform, value); kind == AccountTypeOAuth && tier != "" {
		return tier, nil
	}
	return "", infraerrors.BadRequest("INVALID_SHARED_SUBSCRIPTION_TIER", "请选择该平台支持的 OAuth 订阅档位")
}

func sharedOpenAISubscriptionTier(a *Account) string {
	for _, key := range []string{"chatgpt_plan_type", "subscription_tier"} {
		if tier := canonicalSharedSubscriptionTier(a.Platform, a.GetCredential(key)); tier != "" {
			return tier
		}
	}
	// 导入的凭据可能只有令牌，沿用导入逻辑提取明确的套餐元数据；缺失不能推断为 Free。
	for _, key := range []string{"id_token", "access_token"} {
		claims := sharedOpenAIPlanClaims(a.GetCredential(key))
		if claims == nil {
			continue
		}
		if id := a.GetCredential("chatgpt_account_id"); id != "" && claims.ChatGPTAccountID != "" && id != claims.ChatGPTAccountID {
			continue
		}
		if tier := canonicalSharedSubscriptionTier(a.Platform, claims.ChatGPTPlanType); tier != "" {
			return tier
		}
	}
	return ""
}

func sharedOpenAIPlanClaims(token string) *openai.OpenAIAuthClaims {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(token) > 32768 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return nil
	}
	// 不读取 aud 等非套餐字段，兼容 ID/Access Token 的不同字段格式；不作为身份认证。
	var claims struct {
		Auth *openai.OpenAIAuthClaims `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return nil
	}
	return claims.Auth
}
