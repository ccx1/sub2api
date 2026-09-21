package service

import (
	"context"
	"maps"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type codexTicketProxySnapshotSettings struct {
	SettingRepository
	mu      sync.Mutex
	values  map[string]string
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *codexTicketProxySnapshotSettings) GetMultiple(ctx context.Context, _ []string) (map[string]string, error) {
	r.mu.Lock()
	values := maps.Clone(r.values)
	r.mu.Unlock()
	var err error
	r.once.Do(func() {
		close(r.started)
		select {
		case <-r.release:
		case <-ctx.Done():
			err = ctx.Err()
		}
	})
	return values, err
}

func TestCodexTicketProxySettingsInvalidationWinsOverInFlightOldRead(t *testing.T) {
	repo := &codexTicketProxySnapshotSettings{started: make(chan struct{}), release: make(chan struct{}), values: map[string]string{
		SettingKeyOpenAICodexTicketHarvestProxyMode: "fixed", SettingKeyOpenAICodexTicketHarvestProxyURL: "http://old.example:8080",
	}}
	svc := NewSettingService(repo, &config.Config{})
	readDone, saved := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(readDone)
		_, _, _ = svc.GetOpenAICodexTicketHarvestProxySettings(context.Background())
	}()
	select {
	case <-repo.started:
	case <-time.After(time.Second):
		t.Fatal("setting read did not start")
	}
	repo.mu.Lock()
	repo.values[SettingKeyOpenAICodexTicketHarvestProxyURL] = "http://new.example:8080"
	repo.mu.Unlock()
	go func() { svc.InvalidateProxyPoolSettingsCache(); close(saved) }()
	close(repo.release)
	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Fatal("setting read did not finish")
	}
	select {
	case <-saved:
	case <-time.After(time.Second):
		t.Fatal("setting invalidation did not finish")
	}
	// 模拟独立旧 URL 缓存的迟到写入；新的统一入口不再读取它。
	svc.openAICodexTicketHarvestProxyCache.Store(&cachedOpenAICodexTicketHarvestProxy{value: "http://stale.example:8080", expiresAt: time.Now().Add(time.Hour).UnixNano()})
	mode, proxyURL, err := svc.GetOpenAICodexTicketHarvestProxySettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, "fixed", mode)
	require.Equal(t, "http://new.example:8080", proxyURL)
}

func TestCodexTicketProxySettingsUsesOneSnapshotAndLegacyDefaults(t *testing.T) {
	for _, tc := range []struct{ mode, storedURL, yamlURL, wantMode, wantURL string }{
		{wantMode: "pool"},
		{yamlURL: "http://yaml.example:8080", wantMode: "fixed", wantURL: "http://yaml.example:8080"},
		{storedURL: "http://stored.example:8080", yamlURL: "http://yaml.example:8080", wantMode: "fixed", wantURL: "http://stored.example:8080"},
		{mode: "pool", storedURL: "http://stored.example:8080", wantMode: "pool"},
		{mode: "fixed", wantMode: "fixed"},
	} {
		repo := &codexTicketSettingRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{
			SettingKeyOpenAICodexTicketHarvestProxyMode: tc.mode, SettingKeyOpenAICodexTicketHarvestProxyURL: tc.storedURL,
		}}}
		cfg := &config.Config{}
		cfg.Gateway.OpenAICodexTicket.HarvestProxyURL = tc.yamlURL
		svc := NewSettingService(repo, cfg)
		mode, proxyURL, err := svc.GetOpenAICodexTicketHarvestProxySettings(context.Background())
		require.NoError(t, err)
		require.Equal(t, tc.wantMode, mode)
		require.Equal(t, tc.wantURL, proxyURL)
	}
}
