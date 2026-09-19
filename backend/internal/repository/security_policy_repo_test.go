package repository

import (
	"context"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSecurityPolicyRepositoryListKeywordsEmpty(t *testing.T) {
	_, client := newAPIKeyRepoSQLite(t)
	repo := NewSecurityPolicyRepository(client, nil)
	ctx := context.Background()

	words, err := repo.ListKeywords(ctx, nil, true)
	require.NoError(t, err)
	require.NotNil(t, words)
	require.Empty(t, words)

	words, err = repo.ListEffectiveKeywords(ctx, nil)
	require.NoError(t, err)
	require.NotNil(t, words)
	require.Empty(t, words)
}

func TestSecurityPolicyRepositoryListKeywordsFilters(t *testing.T) {
	_, client := newAPIKeyRepoSQLite(t)
	repo, groupID := seedSecurityPolicyKeywords(t, client)
	globalID, missingID := int64(0), groupID+100
	tests := []struct {
		name            string
		groupID         *int64
		includeDisabled bool
		want            []string
	}{
		{"all enabled", nil, false, []string{"other", "group", "global"}},
		{"all including disabled", nil, true, []string{"other-disabled", "other", "group-disabled", "group", "global-disabled", "global"}},
		{"global enabled", &globalID, false, []string{"global"}},
		{"global including disabled", &globalID, true, []string{"global-disabled", "global"}},
		{"group enabled", &groupID, false, []string{"group"}},
		{"group including disabled", &groupID, true, []string{"group-disabled", "group"}},
		{"unknown group", &missingID, true, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			words, err := repo.ListKeywords(context.Background(), tt.groupID, tt.includeDisabled)
			require.NoError(t, err)
			require.Equal(t, tt.want, securityPolicyKeywordNames(words))
		})
	}
}

func TestSecurityPolicyRepositoryListEffectiveKeywordsFilters(t *testing.T) {
	_, client := newAPIKeyRepoSQLite(t)
	repo, groupID := seedSecurityPolicyKeywords(t, client)
	globalID, missingID := int64(0), groupID+100
	tests := []struct {
		name    string
		groupID *int64
		want    []string
	}{
		{"global only", nil, []string{"global"}},
		{"zero group", &globalID, []string{"global"}},
		{"global and selected group", &groupID, []string{"global", "group"}},
		{"unknown group retains global", &missingID, []string{"global"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			words, err := repo.ListEffectiveKeywords(context.Background(), tt.groupID)
			require.NoError(t, err)
			require.Equal(t, tt.want, securityPolicyKeywordNames(words))
		})
	}
}

func seedSecurityPolicyKeywords(t *testing.T, client *dbent.Client) (service.SecurityPolicyRepository, int64) {
	t.Helper()
	ctx := context.Background()
	repo := NewSecurityPolicyRepository(client, nil)
	group, err := client.Group.Create().SetName("security-policy-selected").Save(ctx)
	require.NoError(t, err)
	other, err := client.Group.Create().SetName("security-policy-other").Save(ctx)
	require.NoError(t, err)
	rows := []struct {
		keyword string
		groupID *int64
		enabled bool
		deleted bool
	}{
		{"global", nil, true, false},
		{"global-disabled", nil, false, false},
		{"group", &group.ID, true, false},
		{"group-disabled", &group.ID, false, false},
		{"other", &other.ID, true, false},
		{"other-disabled", &other.ID, false, false},
		{"global-deleted", nil, true, true},
		{"group-deleted", &group.ID, true, true},
	}
	for _, row := range rows {
		word, err := client.SecurityPolicyKeyword.Create().
			SetKeyword(row.keyword).
			SetNillableGroupID(row.groupID).
			SetEnabled(row.enabled).
			Save(ctx)
		require.NoError(t, err)
		if row.deleted {
			require.NoError(t, repo.DeleteKeyword(ctx, word.ID))
			stored, err := client.SecurityPolicyKeyword.Get(mixins.SkipSoftDelete(ctx), word.ID)
			require.NoError(t, err)
			require.NotNil(t, stored.DeletedAt)
		}
	}
	return repo, group.ID
}

func securityPolicyKeywordNames(words []service.SecurityPolicyKeyword) []string {
	names := make([]string, 0, len(words))
	for _, word := range words {
		names = append(names, word.Keyword)
	}
	return names
}
