package service

import (
	"context"
	"errors"
	"strings"
	"unicode"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func canonicalSharedSubscriptionTier(platform, raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch platform {
	case PlatformOpenAI:
		value = strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) || r == '_' || r == '-' {
				return -1
			}
			return r
		}, value)
		switch value {
		case "chatgptfree", "freetier":
			return "free"
		case "chatgptplus":
			return "plus"
		case "chatgptpro", "pro20", "pro20x":
			return "pro"
		case "chatgptprolite", "pro5", "pro5x":
			return "prolite"
		case "selfservebusinessprolite":
			return "self_serve_business_prolite"
		case "free", "plus", "pro", "prolite", "team", "business", "enterprise":
			return value
		}
	case PlatformGemini:
		if tier := canonicalGeminiTierID(value); tier != GeminiTierGoogleOneUnknown {
			return tier
		}
	case PlatformAntigravity:
		switch value {
		case "free", "free-tier":
			return "free"
		case "pro", "g1-pro-tier":
			return "pro"
		case "ultra", "g1-ultra-tier":
			return "ultra"
		}
	}
	return ""
}

func sharedAccountSubscriptionTier(a *Account) string {
	if a == nil || a.Type != AccountTypeOAuth {
		return ""
	}
	if tier := canonicalSharedSubscriptionTier(a.Platform, a.GetExtraString(SharedPoolSubscriptionTierKey)); tier != "" {
		return tier
	}
	if a.Platform == PlatformGemini {
		tier := canonicalGeminiTierIDForOAuthType(a.GetCredential("oauth_type"), a.GetCredential("tier_id"))
		return canonicalSharedSubscriptionTier(a.Platform, tier)
	}
	if tier := canonicalSharedSubscriptionTier(a.Platform, a.GetCredential("plan_type")); tier != "" {
		return tier
	}
	if a.Platform == PlatformOpenAI {
		return sharedOpenAISubscriptionTier(a)
	}
	if a.Platform == PlatformAntigravity {
		// 仅使用明确的档位信息；缺失数据不能被推断为 Free。
		load, _ := a.Extra["load_code_assist"].(map[string]any)
		for _, key := range []string{"paidTier", "currentTier"} {
			value, _ := load[key].(map[string]any)
			id, _ := value["id"].(string)
			if tier := canonicalSharedSubscriptionTier(a.Platform, id); tier != "" {
				return tier
			}
		}
	}
	return ""
}

func validSharedAssignmentGroup(g *Group, platform string) bool {
	return g != nil && sharedPlatformSupported(platform) && g.Platform == platform &&
		g.IsActive() && !g.IsExclusive && g.SubscriptionType == SubscriptionTypeStandard
}

func (s *SharedPoolService) sharedAssignmentGroup(ctx context.Context, a *Account, id int64) (*Group, error) {
	if id <= 0 {
		return nil, nil
	}
	g, err := s.groups.GetByID(ctx, id)
	if errors.Is(err, ErrGroupNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !validSharedAssignmentGroup(g, a.Platform) || (!SharedPoolDispatchConsented(a) && !g.IsSharedPool) || (g.RequireOAuthOnly && a.Type == AccountTypeAPIKey) {
		return nil, nil
	}
	return g, nil
}

func (s *SharedPoolService) initialSharedGroups(ctx context.Context, cfg *SharedPoolSettings, a *Account) ([]int64, error) {
	if tier := sharedAccountSubscriptionTier(a); tier != "" {
		ids := make([]int64, 0, len(cfg.SubscriptionGroupIDs[a.Platform][tier]))
		seen := make(map[int64]bool)
		for _, id := range cfg.SubscriptionGroupIDs[a.Platform][tier] {
			if seen[id] {
				continue
			}
			g, err := s.sharedAssignmentGroup(ctx, a, id)
			if err != nil {
				return nil, err
			}
			if g != nil {
				ids = append(ids, g.ID)
				seen[g.ID] = true
			}
		}
		if len(ids) > 0 {
			return ids, nil
		}
	}
	ids := []int64{}
	seen := make(map[int64]bool)
	for _, id := range cfg.DefaultGroupIDs[a.Platform] {
		if seen[id] {
			continue
		}
		g, err := s.sharedAssignmentGroup(ctx, a, id)
		if err != nil {
			return nil, err
		}
		if g != nil {
			ids = append(ids, g.ID)
			seen[id] = true
		}
	}
	if len(ids) == 0 && !SharedPoolDispatchConsented(a) {
		return nil, infraerrors.BadRequest("SHARED_DEFAULT_REQUIRED", "此平台的默认共享分组未配置或不可用于该账号，请联系管理员")
	}
	return ids, nil
}

func (s *SharedPoolService) validateSharedGroupSettings(ctx context.Context, cfg *SharedPoolSettings) error {
	normalizedDefaults, err := NormalizeSharedDefaultGroupIDs(cfg.DefaultGroupIDs)
	if err != nil {
		return err
	}
	for platform, ids := range normalizedDefaults {
		for _, id := range ids {
			g, err := s.groups.GetByID(ctx, id)
			if err != nil || !validSharedAssignmentGroup(g, platform) {
				return infraerrors.BadRequest("INVALID_SHARED_DEFAULT", "默认分组必须是同平台已启用的非专属标准分组")
			}
		}
	}
	cfg.DefaultGroupIDs = normalizedDefaults
	// nil 保留旧客户端未提交的规则，空对象表示主动清空。
	if cfg.SubscriptionGroupIDs == nil {
		return nil
	}
	normalizedInput, err := NormalizeSharedSubscriptionGroupIDs(cfg.SubscriptionGroupIDs)
	if err != nil {
		return err
	}
	normalized := make(SharedPoolSubscriptionGroupIDs, len(cfg.SubscriptionGroupIDs))
	for platform, rules := range cfg.SubscriptionGroupIDs {
		if !sharedPlatformSupported(platform) || (len(rules) > 0 && len(cfg.DefaultGroupIDs[platform]) == 0) {
			return infraerrors.BadRequest("INVALID_SHARED_SUBSCRIPTION_GROUP", "请先为订阅档位规则设置同平台默认共享分组")
		}
		normalized[platform] = make(map[string][]int64, len(rules))
		for raw := range rules {
			tier := canonicalSharedSubscriptionTier(platform, raw)
			normalizedIDs := normalizedInput[platform][raw]
			if tier == "" || len(normalizedIDs) == 0 || len(normalized[platform][tier]) != 0 {
				return infraerrors.BadRequest("INVALID_SHARED_SUBSCRIPTION_GROUP", "订阅档位无效、重复或目标共享分组不可用")
			}
			for _, id := range normalizedIDs {
				g, err := s.groups.GetByID(ctx, id)
				if err != nil || !validSharedAssignmentGroup(g, platform) {
					return infraerrors.BadRequest("INVALID_SHARED_SUBSCRIPTION_GROUP", "订阅档位无效、重复或目标共享分组不可用")
				}
			}
			normalized[platform][tier] = append([]int64(nil), normalizedIDs...)
		}
	}
	cfg.SubscriptionGroupIDs = normalized
	return nil
}
