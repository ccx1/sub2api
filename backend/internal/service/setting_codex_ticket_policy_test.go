package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type ticketPolicyRepo struct {
	*codexTicketSettingRepo
	writeErr error
	writes   int
}

func (r *ticketPolicyRepo) SetMultiple(ctx context.Context, values map[string]string) error {
	r.writes++
	if r.writeErr != nil {
		return r.writeErr
	}
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

func newTicketPolicySettings() (*SettingService, *ticketPolicyRepo) {
	repo := &ticketPolicyRepo{codexTicketSettingRepo: &codexTicketSettingRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{}}}}
	cfg := &config.Config{}
	cfg.Gateway.OpenAICodexTicket.FailClosed = true
	return NewSettingService(repo, cfg), repo
}

func TestCodexTicketPolicyPersistenceAndRuntimeSnapshots(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTicketPolicySettings()
	cfg, err := svc.GetCodexTicketSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, 332, cfg.TierRules[0].TargetLength)
	require.Equal(t, 292, cfg.TierRules[1].TargetLength)
	require.False(t, cfg.Enabled)
	_ = svc.GetOpenAICodexTicketRuntimeConfig(ctx, cfg)
	cfg.Enabled, cfg.RefreshBeforeSeconds = true, 0
	cfg.TierRules[0].TargetLength = 352
	cfg.RetryBackoffSeconds = []int{10, 20, 40}
	stored, err := svc.UpdateCodexTicketSettings(ctx, cfg)
	require.NoError(t, err)
	require.Equal(t, 1, repo.writes)
	require.Equal(t, "true", repo.values[SettingKeyOpenAICodexTicketEnabled])
	require.Equal(t, stored, svc.GetOpenAICodexTicketRuntimeConfig(ctx, cfg))
	stored.TierRules[0].Aliases[0] = "mutated"
	stored.RetryBackoffSeconds[0] = 999
	next := svc.GetOpenAICodexTicketRuntimeConfig(ctx, cfg)
	require.Equal(t, 10, next.RetryBackoffSeconds[0])
	require.Equal(t, "business", next.TierRules[0].Aliases[0])
	require.Equal(t, 0, config.NormalizeOpenAICodexTicketConfig(next).RefreshBeforeSeconds)
	restarted := NewSettingService(repo, svc.cfg)
	reloaded, err := restarted.GetCodexTicketSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, next, reloaded)
}

func TestCodexTicketPolicyRejectsInvalidWithoutWrites(t *testing.T) {
	cases := map[string]func(*config.OpenAICodexTicketConfig){
		"conflicting alias":   func(c *config.OpenAICodexTicketConfig) { c.TierRules[1].Aliases = []string{"T_e-a m"} },
		"rejected target":     func(c *config.OpenAICodexTicketConfig) { c.TierRules[0].TargetLength = 312 },
		"empty retry":         func(c *config.OpenAICodexTicketConfig) { c.RetryBackoffSeconds = nil },
		"decreasing retry":    func(c *config.OpenAICodexTicketConfig) { c.RetryBackoffSeconds = []int{30, 10} },
		"too many workers":    func(c *config.OpenAICodexTicketConfig) { c.HarvestConcurrency = 65 },
		"zero auth delay":     func(c *config.OpenAICodexTicketConfig) { c.AuthCooldownSeconds = 0 },
		"refresh outside ttl": func(c *config.OpenAICodexTicketConfig) { c.RefreshBeforeSeconds = c.TTLSeconds },
		"empty model":         func(c *config.OpenAICodexTicketConfig) { c.Models = []string{"  "} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			svc, repo := newTicketPolicySettings()
			cfg, err := svc.GetCodexTicketSettings(context.Background())
			require.NoError(t, err)
			mutate(&cfg)
			_, err = svc.UpdateCodexTicketSettings(context.Background(), cfg)
			require.Error(t, err)
			require.Zero(t, repo.writes)
		})
	}
}

func TestCodexTicketPolicyFailedSaveAndReadKeepLastGood(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTicketPolicySettings()
	cfg, _ := svc.GetCodexTicketSettings(ctx)
	cfg.TierRules[0].TargetLength = 352
	_, err := svc.UpdateCodexTicketSettings(ctx, cfg)
	require.NoError(t, err)
	repo.writeErr = errors.New("write unavailable")
	bad := cloneCodexTicketSettings(cfg)
	bad.TierRules[0].TargetLength = 372
	_, err = svc.UpdateCodexTicketSettings(ctx, bad)
	require.Error(t, err)
	require.Equal(t, 352, svc.GetOpenAICodexTicketRuntimeConfig(ctx, cfg).TierRules[0].TargetLength)
	repo.err = errors.New("read unavailable")
	svc.codexTicketSettingsCache.Store(&cachedCodexTicketSettings{config: cfg})
	require.Equal(t, 352, svc.GetOpenAICodexTicketRuntimeConfig(ctx, bad).TierRules[0].TargetLength)
	_, err = svc.GetCodexTicketSettings(ctx)
	require.Error(t, err)
}

func TestCodexTicketPolicyValidationBoundariesMatchAdminForm(t *testing.T) {
	base := func(t *testing.T) (*SettingService, *ticketPolicyRepo, config.OpenAICodexTicketConfig) {
		t.Helper()
		svc, repo := newTicketPolicySettings()
		cfg, err := svc.GetCodexTicketSettings(context.Background())
		require.NoError(t, err)
		return svc, repo, cfg
	}
	t.Run("model byte limit and empty value", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			model   string
			wantErr bool
		}{
			{"160 ascii bytes", strings.Repeat("a", 160), false},
			{"161 ascii bytes", strings.Repeat("a", 161), true},
			{"160 UTF-8 bytes", strings.Repeat("a", 158) + "é", false},
			{"161 UTF-8 bytes", strings.Repeat("a", 159) + "é", true},
			{"empty", "", true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				svc, repo, cfg := base(t)
				cfg.Models = []string{tc.model}
				_, err := svc.UpdateCodexTicketSettings(context.Background(), cfg)
				require.Equal(t, tc.wantErr, err != nil)
				require.Equal(t, !tc.wantErr, repo.writes == 1)
			})
		}
	})
	t.Run("trimmed tier and aliases", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			aliases int
			wantErr bool
		}{
			{"sixteen aliases", 16, false},
			{"seventeen aliases", 17, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				svc, repo, cfg := base(t)
				aliases := make([]string, tc.aliases)
				for i := range aliases {
					aliases[i] = "alias" + fmt.Sprint(i)
				}
				cfg.TierRules = []config.CodexTicketTierRule{{Tier: " " + strings.Repeat("a", 80) + " ", Aliases: aliases, TargetLength: 332}}
				_, err := svc.UpdateCodexTicketSettings(context.Background(), cfg)
				require.Equal(t, tc.wantErr, err != nil)
				require.Equal(t, tc.wantErr, repo.writes == 0)
			})
		}
	})
}

func TestCodexTicketPolicyRefreshFromOtherInstanceAndLegacySwitch(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTicketPolicySettings()
	cfg, _ := svc.GetCodexTicketSettings(ctx)
	_ = svc.GetOpenAICodexTicketRuntimeConfig(ctx, cfg)
	other := NewSettingService(repo, svc.cfg)
	cfg.Enabled = true
	cfg.TierRules = []config.CodexTicketTierRule{}
	cfg.RejectedLengths = []int{}
	_, err := other.UpdateCodexTicketSettings(ctx, cfg)
	require.NoError(t, err)
	cached := svc.codexTicketSettingsCache.Load()
	svc.codexTicketSettingsCache.Store(&cachedCodexTicketSettings{config: cached.config, expiresAt: time.Now().Add(-time.Second)})
	svc.InvalidateOpenAICodexTicketEnabledCache()
	next := svc.GetOpenAICodexTicketRuntimeConfig(ctx, cfg)
	require.True(t, next.Enabled)
	require.Empty(t, next.TierRules)
	require.Empty(t, config.NormalizeOpenAICodexTicketConfig(next).RejectedLengths)
	repo.values[SettingKeyOpenAICodexTicketEnabled] = "false"
	svc.InvalidateOpenAICodexTicketEnabledCache()
	require.False(t, svc.GetOpenAICodexTicketRuntimeConfig(ctx, cfg).Enabled)
	raw, err := json.Marshal(next)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "harvest_proxy_url")
}
