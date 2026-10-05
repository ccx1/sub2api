//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedPoolImportCurrentOpenAIPlansPersist(t *testing.T) {
	for _, tier := range []string{"go", "promax", "self_serve_business_usage_based", "ent26",
		"enterprise_cbp_usage_based", "enterprise_cbp_automation", "edu", "edu_plus", "edu_pro"} {
		t.Run(tier, func(t *testing.T) {
			f := newSharedImportFixture(t)
			f.account.Credentials["plan_type"] = tier
			rules, err := json.Marshal(service.SharedPoolSubscriptionGroupIDs{service.PlatformOpenAI: {tier: {11}}})
			require.NoError(t, err)
			f.mock.ExpectBegin()
			expectSharedImportGroup(f.mock, f.group)
			expectSharedImportRules(f.mock, new(string(rules)))
			expectSharedImportWrites(f.mock, "")
			require.NoError(t, f.repo.CreateSharedAccount(context.Background(), f.account, 7, "fixture-fingerprint"))
			require.Equal(t, int64(41), f.account.ID)
			require.Equal(t, []int64{11}, f.account.GroupIDs)
			require.Equal(t, tier, f.account.Credentials["plan_type"])
		})
	}
}
