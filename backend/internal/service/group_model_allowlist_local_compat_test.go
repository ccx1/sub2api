//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetAvailableModels_PassthroughPreservesLocalGroupRestriction(t *testing.T) {
	groupID := int64(10)
	restricted := Account{
		ID: 1, Platform: PlatformOpenAI,
		Credentials:   map[string]any{"model_mapping": map[string]any{"stale-model": "upstream-model"}},
		Extra:         map[string]any{"openai_passthrough": true},
		AccountGroups: []AccountGroup{{GroupID: groupID, AllowedModels: []string{"allowed-model"}}},
	}
	mapped := Account{
		ID: 2, Platform: PlatformOpenAI,
		Credentials: map[string]any{"model_mapping": map[string]any{"configured-model": "upstream-model"}},
	}
	for _, accounts := range [][]Account{{restricted}, {restricted, mapped}} {
		repo := &modelsListAccountRepoStub{byGroup: map[int64][]Account{groupID: accounts}}
		svc := &GatewayService{accountRepo: repo}
		want := []string{"allowed-model"}
		if len(accounts) == 2 {
			want = append(want, "configured-model")
		}
		require.Equal(t, want, svc.GetAvailableModels(context.Background(), &groupID, PlatformOpenAI))
	}
}

func TestGroupAllowlistGlobRetainsUserDeniedModelBoundary(t *testing.T) {
	allowlist := GroupModelAllowlist{Enabled: true, Models: []string{"gpt-*-codex"}}
	denied, err := NormalizeUserGroupDeniedModels([]string{"gpt-6-*"})
	require.NoError(t, err)
	require.True(t, allowlist.Allows("gpt-6-codex"))
	require.True(t, UserGroupDeniesModel(denied, "GPT-6-CODEX"))
	require.True(t, allowlist.Allows("gpt-5-codex"))
	require.False(t, UserGroupDeniesModel(denied, "gpt-5-codex"))
	require.False(t, allowlist.Allows("unrelated-model"))
	_, err = NormalizeUserGroupDeniedModels([]string{"gpt-*-codex"})
	require.Error(t, err, "用户禁用清单保留原有校验边界")
}
