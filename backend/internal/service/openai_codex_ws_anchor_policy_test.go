package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func groupAnchorSettings() config.AstraRoutingSettings {
	return config.AstraRoutingSettings{Revision: "policy-one", CookiePool: config.CodexGatewayPinConfig{
		Enabled: true, SourceSelection: "groups", SourceGroupIDs: []int64{1}, SourceAccountIDs: []int64{299},
		TargetSelection: "groups", TargetGroupIDs: []int64{2}, TargetAccountIDs: []int64{300},
	}, WSSession: config.CodexWSAnchorConfig{Enabled: true, AccountIDs: []int64{300}}}
}

func TestCodexWSAnchorFencesDynamicGroupMembers(t *testing.T) {
	for _, changed := range []string{"source", "target", "selection error"} {
		t.Run(changed, func(t *testing.T) {
			s, account := anchorTestService()
			settings := groupAnchorSettings()
			s.cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
			c := anchorTestContext(1, "group-session")
			completeAnchorSeed(t, s, account, c)
			t.Cleanup(s.getOpenAIWSConnPool().Close)
			for key, entry := range s.codexWSAnchors.entries {
				entry.connID = "borrowed-connection"
				s.codexWSAnchors.entries[key] = entry
			}
			switch changed {
			case "source":
				settings.CookiePool.SourceAccountIDs = []int64{298}
			case "target":
				settings.CookiePool.TargetAccountIDs = []int64{301}
			case "selection error":
				settings.SelectionError = "astra_group_accounts_empty"
				settings.CookiePool.SourceAccountIDs = nil
				settings.CookiePool.TargetAccountIDs = nil
			}
			ctx, finish, err := s.prepareCodexWSAnchor(t.Context(), c, account, []byte(`{"model":"gpt-6-astra","previous_response_id":"resp_seed"}`))
			require.Error(t, err, "a persisted revision alone cannot authorize the previous borrowed connection")
			require.Nil(t, finish)
			require.Nil(t, codexWSAnchorFromContext(ctx))
			require.Empty(t, s.codexWSAnchors.entries)
		})
	}
}

func TestCodexWSAnchorGroupScopeStableForSchedulingChanges(t *testing.T) {
	s, account := anchorTestService()
	settings := groupAnchorSettings()
	s.cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	c := anchorTestContext(1, "group-session")
	completeAnchorSeed(t, s, account, c)
	settings.AccountScheduling = true
	settings.SchedulingMode = "model"
	ctx, finish, err := s.prepareCodexWSAnchor(t.Context(), c, account, []byte(`{"model":"gpt-6-astra","previous_response_id":"resp_seed"}`))
	require.NoError(t, err)
	require.NotNil(t, finish)
	require.Equal(t, "resp_seed", codexWSAnchorFromContext(ctx).previousID)
	finish(nil, nil)
}

func TestCodexWSAnchorLegacyScopeRetainsPersistedRevision(t *testing.T) {
	settings := groupAnchorSettings()
	settings.CookiePool.SourceSelection, settings.CookiePool.TargetSelection = "", "accounts"
	require.Equal(t, settings.Revision, astraWSAnchorRevision(settings))
	settings.CookiePool.TargetAccountIDs = []int64{301}
	require.Equal(t, settings.Revision, astraWSAnchorRevision(settings))
}

func TestCodexWSAnchorDoesNotPublishAfterGroupChange(t *testing.T) {
	s, account := anchorTestService()
	settings := groupAnchorSettings()
	s.cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	ctx, finish, err := s.prepareCodexWSAnchor(t.Context(), anchorTestContext(1, "group-session"), account, []byte(`{"model":"gpt-6-astra"}`))
	require.NoError(t, err)
	codexWSAnchorFromContext(ctx).qualified = true
	settings.CookiePool.SourceAccountIDs = []int64{298}
	finish(&OpenAIForwardResult{ResponseID: "resp_stale", UpstreamResponseModel: "gpt-6-astra"}, nil)
	require.Empty(t, s.codexWSAnchors.entries)
	require.Empty(t, s.codexWSAnchors.busy)
}

func TestCodexWSAnchorAllowsCurrentGroupDonor(t *testing.T) {
	s, account := anchorTestService()
	settings := groupAnchorSettings()
	settings.WSSession.AccountIDs = []int64{299}
	account.ID = 299
	s.cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	ctx, finish, err := s.prepareCodexWSAnchor(t.Context(), anchorTestContext(1, "source-session"), account, []byte(`{"model":"gpt-6-astra"}`))
	require.NoError(t, err)
	require.NotNil(t, codexWSAnchorFromContext(ctx))
	finish(nil, nil)
}
