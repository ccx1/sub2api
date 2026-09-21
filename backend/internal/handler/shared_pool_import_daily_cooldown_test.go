//go:build unit

package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const sharedCooldownImportContent = `[
	{"name":"one","platform":"gemini","type":"apikey","credentials":{"api_key":"fixture-one"},"daily_cooldown":{"enabled":false},"extra":{"daily_cooldown":{"enabled":false},"private":"secret"}},
	{"name":"two","platform":"gemini","type":"apikey","credentials":{"api_key":"fixture-two"}}
]`

func TestSharedImportDailyCooldownDefaultsReachCreateAndView(t *testing.T) {
	for _, cooldown := range []*service.SharedPoolDailyCooldown{nil, {}, {Enabled: true, Start: "23:00", End: "08:00"}} {
		defaults := importTestDefaults()
		defaults.Enabled, defaults.ProtectionEnabled, defaults.DailyCooldown = false, false, cooldown
		entries, err := parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Content: sharedCooldownImportContent}}, Defaults: defaults})
		require.NoError(t, err)
		if cooldown != nil {
			require.NotSame(t, cooldown, entries[0].input.DailyCooldown)
			require.NotSame(t, entries[0].input.DailyCooldown, entries[1].input.DailyCooldown)
		}
		repo := &sharedImportRepositoryStub{accounts: map[int64]*service.Account{}, owners: map[int64]int64{}, seen: map[string]bool{}}
		pool := service.NewSharedPoolService(repo, repo, nil, nil, nil, sharedImportEarningsStub{}, nil)
		result, err := executeSharedImport(context.Background(), 915, entries, pool.Create)
		require.NoError(t, err)
		require.Equal(t, 2, result.Created)
		require.Zero(t, result.Failed)
		for id, account := range repo.accounts {
			view, err := pool.Get(context.Background(), 915, id)
			require.NoError(t, err)
			if cooldown == nil {
				require.Nil(t, view.DailyCooldown)
				require.NotContains(t, account.Extra, service.DailyCooldownExtraKey)
			} else {
				require.Equal(t, cooldown.ExtraValue(), account.Extra[service.DailyCooldownExtraKey])
				require.Equal(t, cooldown.ExtraValue(), view.DailyCooldown.ExtraValue())
			}
			require.NotContains(t, account.Extra, "private")
		}
	}
}

func TestSharedImportDailyCooldownInvalidDefaultsFailEachItemBeforeCreate(t *testing.T) {
	defaults := importTestDefaults()
	defaults.Enabled, defaults.ProtectionEnabled = false, false
	defaults.DailyCooldown = &service.SharedPoolDailyCooldown{Enabled: true, Start: "23:00", End: "08:00", Timezone: "Mars/Unknown"}
	proxy := "http://8.8.8.8:8080"
	defaults.ProxyURL = &proxy
	entries, err := parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Content: sharedCooldownImportContent}}, Defaults: defaults})
	require.NoError(t, err)
	repo := &sharedImportRepositoryStub{accounts: map[int64]*service.Account{}, owners: map[int64]int64{}, seen: map[string]bool{}}
	pool := service.NewSharedPoolService(repo, repo, nil, nil, nil, sharedImportEarningsStub{}, nil)
	result, err := executeSharedImport(context.Background(), 916, entries, pool.Create)
	require.NoError(t, err)
	require.Zero(t, result.Created)
	require.Equal(t, 2, result.Failed)
	require.Empty(t, repo.accounts)
	for _, item := range result.Items {
		require.Contains(t, item.Message, "每日冷却")
	}
}

func TestSharedDailyCooldownStrictBindingRejectsArbitraryNestedFields(t *testing.T) {
	for _, raw := range []string{
		`{"daily_cooldown":{"enabled":true,"start":"23:00","end":"08:00"}}`,
		`{"daily_cooldown":null}`, `{"daily_cooldown":{"enabled":false}}`,
		`{"daily_cooldown":{"enabled":true,"credentials":{}}}`, `{"daily_cooldown":{"enabled":"true"}}`,
	} {
		valid := !strings.Contains(raw, "credentials") && !strings.Contains(raw, `"enabled":"`)
		for _, importing := range []bool{false, true} {
			body := raw
			if importing {
				body = `{"defaults":` + raw + `}`
			}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/", strings.NewReader(body))
			if importing {
				require.Equal(t, valid, bindSharedImport(c, &sharedImportRequest{}))
			} else {
				require.Equal(t, valid, sharedBind(c, &service.SharedPoolAccountInput{}))
			}
		}
	}
}
