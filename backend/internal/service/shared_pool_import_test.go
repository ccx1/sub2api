//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSharedImportPreservesOrganizationAndFiltersCredentialOverrides(t *testing.T) {
	credentials, err := sanitizeSharedCredentials(PlatformOpenAI, AccountTypeOAuth, map[string]any{
		"access_token": "access", "refresh_token": "refresh", "organization_id": "org-id",
		"base_url": "http://127.0.0.1", "headers": map[string]any{"Host": "private"}, "rate_multiplier": 0,
	})
	require.NoError(t, err)
	require.Equal(t, "org-id", credentials["organization_id"])
	for _, key := range []string{"base_url", "headers", "rate_multiplier"} {
		require.NotContains(t, credentials, key)
	}
}
