package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSchedulerMetadataPreservesProxyRegionResolution(t *testing.T) {
	for _, mode := range []string{"billing", "manual"} {
		t.Run(mode, func(t *testing.T) {
			original := service.Account{ID: 1,
				Credentials: map[string]any{"billing_currency": "JPY", "price_country": "PH", "refresh_token": "private"},
				Extra:       map[string]any{service.ProxyRegionModeExtraKey: mode, service.ProxyRegionCountryExtraKey: "US"}}
			want, err := original.ProxyRegionCountry()
			require.NoError(t, err)
			metadata := buildSchedulerMetadataAccount(original)
			got, err := metadata.ProxyRegionCountry()
			require.NoError(t, err)
			require.Equal(t, want, got)
			require.NotContains(t, metadata.Credentials, "refresh_token")
		})
	}
}
