package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEffectiveSharedSettlementMultiplierForTierPriority(t *testing.T) {
	settings := &SharedPoolSettings{
		SettlementMultiplier: 1,
		SubscriptionSettlementMultipliers: map[string]map[string]float64{
			PlatformOpenAI: {"pro": 2.5},
		},
	}
	ownerOverride := 3.25
	rates := []SharedPoolUserRate{{UserID: 7, SettlementMultiplier: &ownerOverride}}

	require.Equal(t, 3.25, EffectiveSharedSettlementMultiplierForTier(settings, rates, 7, PlatformOpenAI, "pro"), "user override wins")
	require.Equal(t, 2.5, EffectiveSharedSettlementMultiplierForTier(settings, nil, 7, PlatformOpenAI, "ChatGPT Pro"), "tier default wins over global")
	require.Equal(t, 1.0, EffectiveSharedSettlementMultiplierForTier(settings, nil, 7, PlatformOpenAI, "unknown"), "unknown tier falls back to global")
}

func TestNormalizeSharedSettlementMultipliersCanonicalizesAndValidates(t *testing.T) {
	got, err := NormalizeSharedSettlementMultipliers(map[string]map[string]float64{
		" OPENAI ":        {"ChatGPT Pro": 1.5},
		PlatformAnthropic: {"claude-pro": 2},
	})
	require.NoError(t, err)
	require.Equal(t, map[string]map[string]float64{
		PlatformOpenAI:    {"pro": 1.5},
		PlatformAnthropic: {"claude-pro": 2},
	}, got)
	_, err = NormalizeSharedSettlementMultipliers(map[string]map[string]float64{PlatformOpenAI: {"pro": 101}})
	require.Error(t, err)
	_, err = NormalizeSharedSettlementMultipliers(map[string]map[string]float64{"unknown": {"pro": 1}})
	require.Error(t, err)
}
