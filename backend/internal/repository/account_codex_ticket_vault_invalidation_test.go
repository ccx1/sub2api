package repository

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// 票库作废写入的原因与来源属于白名单，撤销历史中原样保留。
func TestCodexTicketVaultInvalidationKeepsReasonAndSource(t *testing.T) {
	account, replacement, event := codexTicketInvalidationFixture()
	event.Reason, event.Source = service.CodexTicketVaultInvalidationReason, service.CodexTicketVaultInvalidationSource
	event.ReportedModels = nil
	replacement["invalidation"] = event
	request, err := prepareCodexTicketCAS(account, "model", replacement)
	require.NoError(t, err)
	require.True(t, request.revoke)
	require.True(t, request.withInvalidation)
	var got service.CodexTicketInvalidation
	require.NoError(t, json.Unmarshal([]byte(request.args[6].(string)), &got))
	require.Equal(t, "admin_revoked", got.Reason)
	require.Equal(t, "ticket_vault", got.Source)
	require.NotContains(t, request.args[6].(string), "private-ticket")
}
