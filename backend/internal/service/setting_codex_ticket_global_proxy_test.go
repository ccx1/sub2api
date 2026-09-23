package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type globalCodexTicketSettingsRepo struct {
	*codexTicketSettingRepo
	updates map[string]string
}

func (r *globalCodexTicketSettingsRepo) SetMultiple(_ context.Context, values map[string]string) error {
	r.updates = make(map[string]string, len(values))
	for key, value := range values {
		r.values[key] = value
		r.updates[key] = value
	}
	return nil
}

// Only GetByID is needed by global fixed-proxy validation. Embedding keeps the
// test focused on the setting contract while satisfying ProxyRepository.
type globalCodexTicketProxyRepo struct {
	ProxyRepository
	proxy *Proxy
	err   error
}

func (r *globalCodexTicketProxyRepo) GetByID(context.Context, int64) (*Proxy, error) {
	return r.proxy, r.err
}

func newGlobalCodexTicketSettings(t *testing.T, values map[string]string, proxy *Proxy) (*SettingService, *globalCodexTicketSettingsRepo) {
	t.Helper()
	repo := &globalCodexTicketSettingsRepo{codexTicketSettingRepo: &codexTicketSettingRepo{
		codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: values},
	}}
	svc := NewSettingService(repo, &config.Config{})
	svc.SetProxyRepository(&globalCodexTicketProxyRepo{proxy: proxy})
	return svc, repo
}

func globalManagedProxy(id int64) *Proxy {
	return &Proxy{ID: id, Status: StatusActive, Protocol: "http", Host: "managed.example", Port: 8080}
}

func TestGlobalCodexTicketProxySettingsRuntimeModes(t *testing.T) {
	for _, mode := range []string{
		OpenAICodexTicketHarvestProxyModeAccount,
		OpenAICodexTicketHarvestProxyModeInherit,
		OpenAICodexTicketHarvestProxyModeRandom,
		OpenAICodexTicketHarvestProxyModeFixed,
	} {
		t.Run(mode, func(t *testing.T) {
			values := map[string]string{SettingKeyOpenAICodexTicketHarvestProxyMode: mode}
			if mode == OpenAICodexTicketHarvestProxyModeFixed {
				values[SettingKeyOpenAICodexTicketHarvestProxyURL] = "http://legacy.example:8080"
			}
			svc, _ := newGlobalCodexTicketSettings(t, values, nil)
			gotMode, gotURL, gotID, err := svc.GetOpenAICodexTicketHarvestProxySettingsWithID(context.Background())
			require.NoError(t, err)
			require.Equal(t, mode, gotMode)
			if mode == OpenAICodexTicketHarvestProxyModeFixed {
				require.Equal(t, "http://legacy.example:8080", gotURL)
			} else {
				require.Empty(t, gotURL)
			}
			require.Zero(t, gotID)
		})
	}
}

func TestGlobalCodexTicketProxySettingsLegacyURLFallback(t *testing.T) {
	repo := &globalCodexTicketSettingsRepo{codexTicketSettingRepo: &codexTicketSettingRepo{
		codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{
			SettingKeyOpenAICodexTicketHarvestProxyMode: OpenAICodexTicketHarvestProxyModeFixed,
		}},
	}}
	cfg := &config.Config{}
	cfg.Gateway.OpenAICodexTicket.HarvestProxyURL = "socks5h://legacy.example:1080"
	svc := NewSettingService(repo, cfg)
	mode, proxyURL, id, err := svc.GetOpenAICodexTicketHarvestProxySettingsWithID(context.Background())
	require.NoError(t, err)
	require.Equal(t, OpenAICodexTicketHarvestProxyModeFixed, mode)
	require.Equal(t, "socks5h://legacy.example:1080", proxyURL)
	require.Zero(t, id)
}

func TestGlobalCodexTicketProxySettingsManagedIDPersistsAndClearsURL(t *testing.T) {
	proxyRepo := &globalCodexTicketProxyRepo{proxy: globalManagedProxy(7)}
	repo := &globalCodexTicketSettingsRepo{codexTicketSettingRepo: &codexTicketSettingRepo{
		codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{}},
	}}
	svc := NewSettingService(repo, &config.Config{})
	svc.SetProxyRepository(proxyRepo)
	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		OpenAICodexTicketHarvestProxyMode: OpenAICodexTicketHarvestProxyModeFixed,
		OpenAICodexTicketHarvestProxyID:   7,
		OpenAICodexTicketHarvestProxyURL:  "http://user:***@old.example:8080",
	})
	require.NoError(t, err)
	require.Equal(t, "fixed", repo.updates[SettingKeyOpenAICodexTicketHarvestProxyMode])
	require.Equal(t, "7", repo.updates[SettingKeyOpenAICodexTicketHarvestProxyID])
	require.Empty(t, repo.updates[SettingKeyOpenAICodexTicketHarvestProxyURL])
}

func TestGlobalCodexTicketProxySettingsRejectUnavailableManagedID(t *testing.T) {
	tests := []struct {
		name  string
		proxy *Proxy
		err   error
	}{
		{name: "missing", err: errors.New("not found")},
		{name: "disabled", proxy: func() *Proxy { p := globalManagedProxy(7); p.Status = StatusDisabled; return p }()},
		{name: "expired", proxy: func() *Proxy {
			p := globalManagedProxy(7)
			expiry := time.Now().Add(-time.Minute)
			p.ExpiresAt = &expiry
			return p
		}()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &globalCodexTicketSettingsRepo{codexTicketSettingRepo: &codexTicketSettingRepo{
				codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{}},
			}}
			svc := NewSettingService(repo, &config.Config{})
			svc.SetProxyRepository(&globalCodexTicketProxyRepo{proxy: tc.proxy, err: tc.err})
			err := svc.UpdateSettings(context.Background(), &SystemSettings{
				OpenAICodexTicketHarvestProxyMode: OpenAICodexTicketHarvestProxyModeFixed,
				OpenAICodexTicketHarvestProxyID:   7,
			})
			require.Error(t, err)
			require.Nil(t, repo.updates)
		})
	}
}

func TestGlobalCodexTicketProxySettingsNonFixedModeClearsManagedSelection(t *testing.T) {
	repo := &globalCodexTicketSettingsRepo{codexTicketSettingRepo: &codexTicketSettingRepo{
		codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{
			SettingKeyOpenAICodexTicketHarvestProxyMode: OpenAICodexTicketHarvestProxyModeFixed,
			SettingKeyOpenAICodexTicketHarvestProxyID:   "7",
			SettingKeyOpenAICodexTicketHarvestProxyURL:  "http://old.example:8080",
		}},
	}}
	svc := NewSettingService(repo, &config.Config{})
	svc.SetProxyRepository(&globalCodexTicketProxyRepo{proxy: globalManagedProxy(7)})
	require.NoError(t, svc.UpdateSettings(context.Background(), &SystemSettings{
		OpenAICodexTicketHarvestProxyMode: OpenAICodexTicketHarvestProxyModeRandom,
		OpenAICodexTicketHarvestProxyID:   7,
		OpenAICodexTicketHarvestProxyURL:  "http://masked.example:8080",
	}))
	require.Equal(t, "random", repo.updates[SettingKeyOpenAICodexTicketHarvestProxyMode])
	require.Equal(t, "0", repo.updates[SettingKeyOpenAICodexTicketHarvestProxyID])
	require.Empty(t, repo.updates[SettingKeyOpenAICodexTicketHarvestProxyURL])
}
