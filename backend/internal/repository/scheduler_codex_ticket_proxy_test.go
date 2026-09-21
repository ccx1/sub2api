//go:build unit

package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSchedulerProjectionPreservesCodexTicketProxyOverride(t *testing.T) {
	for _, mode := range []string{"inherit", "random", "fixed"} {
		t.Run(mode, func(t *testing.T) {
			id := int64(0)
			if mode == "fixed" {
				id = 17
			}
			account := &service.Account{Extra: filterSchedulerExtra(map[string]any{
				service.CodexTicketProxyModeExtraKey: mode,
				service.CodexTicketProxyIDExtraKey:   id,
				service.ProxyModeExtraKey:            "random",
			})}
			require.Equal(t, mode, account.CodexTicketProxyMode())
			require.Equal(t, id, account.CodexTicketProxyID())
			require.True(t, account.IsRandomProxy())
			require.NoError(t, service.ValidateCodexTicketProxyExtra(account.Extra))
		})
	}
}
