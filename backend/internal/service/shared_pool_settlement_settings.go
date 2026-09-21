package service

import (
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func ValidateSharedSettlementSettings(settings *SharedPoolSettings) error {
	if settings == nil {
		return infraerrors.BadRequest("INVALID_SETTLEMENT_SETTINGS", "共享结算配置无效")
	}
	if !ValidSharedPoolSettlementMultiplier(settings.SettlementMultiplier) {
		return infraerrors.BadRequest("INVALID_SETTLEMENT_MULTIPLIER", "结算倍率须为0至100之间的有限数值")
	}
	if _, err := NormalizeSharedSettlementMultipliers(settings.SubscriptionSettlementMultipliers); err != nil {
		return err
	}
	return nil
}

func effectiveSharedSettlementMultiplier(settings *SharedPoolSettings, rates []SharedPoolUserRate, ownerID int64) float64 {
	return effectiveSharedSettlementMultiplierForTier(settings, rates, ownerID, "", "")
}

// EffectiveSharedSettlementMultiplierForTier resolves a settlement multiplier
// for a contributed account. User overrides are intentionally checked first so
// an administrator can customize one supplier without changing tier defaults.
func EffectiveSharedSettlementMultiplierForTier(settings *SharedPoolSettings, rates []SharedPoolUserRate, ownerID int64, platform, tier string) float64 {
	return effectiveSharedSettlementMultiplierForTier(settings, rates, ownerID, platform, tier)
}

func effectiveSharedSettlementMultiplierForTier(settings *SharedPoolSettings, rates []SharedPoolUserRate, ownerID int64, platform, tier string) float64 {
	if settings == nil {
		return 0
	}
	for _, rate := range rates {
		if rate.UserID == ownerID && rate.SettlementMultiplier != nil {
			return *rate.SettlementMultiplier
		}
	}
	if normalizedTier := canonicalSharedSettlementMultiplierTier(platform, tier); normalizedTier != "" {
		if byTier, ok := settings.SubscriptionSettlementMultipliers[strings.ToLower(strings.TrimSpace(platform))]; ok {
			if multiplier, ok := byTier[normalizedTier]; ok && ValidSharedPoolSettlementMultiplier(multiplier) {
				return multiplier
			}
		}
	}
	return settings.SettlementMultiplier
}

// NormalizeSharedSettlementMultipliers validates and canonicalizes the map
// received from the administrator. A nil map is kept nil so callers can use
// nil to mean "preserve the existing value" when updating legacy clients.
func NormalizeSharedSettlementMultipliers(input map[string]map[string]float64) (map[string]map[string]float64, error) {
	if input == nil {
		return nil, nil
	}
	result := make(map[string]map[string]float64, len(input))
	for rawPlatform, tiers := range input {
		platform := strings.ToLower(strings.TrimSpace(rawPlatform))
		if !sharedPlatformSupported(platform) {
			return nil, infraerrors.BadRequest("INVALID_SETTLEMENT_TIER", "订阅档位平台无效")
		}
		if result[platform] == nil {
			result[platform] = make(map[string]float64, len(tiers))
		}
		for rawTier, multiplier := range tiers {
			tier := canonicalSharedSettlementMultiplierTier(platform, rawTier)
			if tier == "" || !ValidSharedPoolSettlementMultiplier(multiplier) {
				return nil, infraerrors.BadRequest("INVALID_SETTLEMENT_TIER", "订阅档位或结算倍率无效")
			}
			if _, exists := result[platform][tier]; exists {
				return nil, infraerrors.BadRequest("INVALID_SETTLEMENT_TIER", "订阅档位重复")
			}
			result[platform][tier] = multiplier
		}
	}
	return result, nil
}

func canonicalSharedSettlementMultiplierTier(platform, raw string) string {
	platform = strings.ToLower(strings.TrimSpace(platform))
	if tier := canonicalSharedSubscriptionTier(platform, raw); tier != "" {
		return tier
	}
	// Anthropic and future OAuth providers may expose a plan label that is not
	// yet in the subscription-group canonicalizer. Keep the label stable while
	// restricting keys to a compact, JSON-safe identifier.
	tier := strings.ToLower(strings.TrimSpace(raw))
	if tier == "" || len([]rune(tier)) > 50 {
		return ""
	}
	for _, r := range tier {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '-' && r != '.' {
			return ""
		}
	}
	return tier
}
