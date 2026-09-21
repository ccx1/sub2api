//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEffectiveSharedSettlementMultiplierForTier(t *testing.T) {
	tierRate := 1.35
	userRate := 0.8
	settings := &SharedPoolSettings{
		SettlementMultiplier: 1,
		SubscriptionSettlementMultipliers: map[string]map[string]float64{
			PlatformOpenAI: {"pro": tierRate},
		},
	}
	require.Equal(t, tierRate, EffectiveSharedSettlementMultiplierForTier(settings, nil, 7, PlatformOpenAI, "pro"))
	require.Equal(t, 1.0, EffectiveSharedSettlementMultiplierForTier(settings, nil, 7, PlatformOpenAI, "plus"))
	require.Equal(t, userRate, EffectiveSharedSettlementMultiplierForTier(settings, []SharedPoolUserRate{{UserID: 7, SettlementMultiplier: &userRate}}, 7, PlatformOpenAI, "pro"))
	require.Equal(t, 1.0, EffectiveSharedSettlementMultiplierForTier(settings, nil, 7, PlatformOpenAI, "unknown"))
}

func TestNormalizeSharedSettlementMultipliersCanonicalizesTiersAndRejectsInvalidValues(t *testing.T) {
	result, err := NormalizeSharedSettlementMultipliers(map[string]map[string]float64{
		" OPENAI ":     {"ChatGPT Pro": 1.2},
		PlatformGemini: {"google_ai_pro": 0},
	})
	require.NoError(t, err)
	require.Equal(t, 1.2, result[PlatformOpenAI]["pro"])
	require.Equal(t, float64(0), result[PlatformGemini][GeminiTierGoogleAIPro])
	_, err = NormalizeSharedSettlementMultipliers(map[string]map[string]float64{PlatformOpenAI: {"pro": 101}})
	require.Error(t, err)
}
