package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSchedulerMetadataPreservesProxyRegionResolution(t *testing.T) {
	for _, tc := range []struct {
		name, mode, currency, fallback, want string
	}{
		{name: "billing", mode: "billing", currency: "JPY", want: "JP"},
		{name: "manual", mode: "manual", currency: "JPY", want: "US"},
		{name: "billing fallback", mode: "billing", currency: "USD", fallback: "JP", want: "JP"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := service.Account{ID: 1,
				Credentials: map[string]any{"billing_currency": tc.currency, "refresh_token": "private"},
				Extra:       map[string]any{service.ProxyRegionModeExtraKey: tc.mode, service.ProxyRegionCountryExtraKey: "US", service.ProxyRegionFallbackCountryExtraKey: tc.fallback}}
			resolved, err := original.ProxyRegionCountry()
			require.NoError(t, err)
			require.Equal(t, tc.want, resolved)
			metadata := buildSchedulerMetadataAccount(original)
			got, err := metadata.ProxyRegionCountry()
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			require.NotContains(t, metadata.Credentials, "refresh_token")
		})
	}
}
