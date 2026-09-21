//go:build unit

package admin

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSharedCodexParserReusesAdminNormalization(t *testing.T) {
	values, err := ParseCodexImportValues("raw-token\n{\"tokens\":{\"access_token\":\"token-two\",\"refresh_token\":\"refresh-two\"}}")
	require.NoError(t, err)
	require.Len(t, values, 2)
	first, err := NormalizeCodexImportCredentials(values[0], 1)
	require.NoError(t, err)
	require.Equal(t, "raw-token", first.Credentials["access_token"])
	require.NotEmpty(t, first.Warnings)
	second, err := NormalizeCodexImportCredentials(values[1], 2)
	require.NoError(t, err)
	require.Equal(t, "refresh-two", second.Credentials["refresh_token"])
	require.NotEmpty(t, second.Credentials["client_id"])
}

func TestSharedCodexParserRejectsExpiredJWT(t *testing.T) {
	token := buildCodexImportTestJWT(t, time.Now().Add(-time.Hour), nil)
	_, err := NormalizeCodexImportCredentials(token, 1)
	require.ErrorContains(t, err, "已过期")
	require.NotContains(t, err.Error(), token)
}

func TestSharedDataEnrichmentDoesNotMutateInput(t *testing.T) {
	token := buildCodexImportTestJWT(t, time.Now().Add(time.Hour), map[string]any{"email": "import@example.test"})
	input := map[string]any{"id_token": token, "access_token": "token"}
	result := EnrichAccountDataCredentials("openai", "oauth", input)
	require.Equal(t, "import@example.test", result["email"])
	require.NotContains(t, input, "email")
}
