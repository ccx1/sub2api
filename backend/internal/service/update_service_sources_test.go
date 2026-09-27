//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type sourceReleaseClient struct {
	updateServiceGitHubClientStub
	releases map[string]*GitHubRelease
	failures map[string]error
	calls    []string
}

func (c *sourceReleaseClient) FetchLatestRelease(_ context.Context, repo string) (*GitHubRelease, error) {
	c.calls = append(c.calls, repo)
	return c.releases[repo], c.failures[repo]
}

func dualSourceService(cache *updateServiceCacheStub, local, ranxi string) (*UpdateService, *sourceReleaseClient) {
	client := &sourceReleaseClient{releases: map[string]*GitHubRelease{
		githubRepo: {TagName: local, Body: "local notes"},
		ranxiRepo:  {TagName: ranxi, Body: "ranxi notes"},
	}, failures: map[string]error{}}
	svc := ProvideUpdateService(cache, client, BuildInfo{Version: "0.2.8.17", RanxiVersion: "2.8.14", BuildType: "release"})
	return svc, client
}

func TestUpdateServiceIndependentSources(t *testing.T) {
	for _, tc := range []struct {
		name, local, ranxi       string
		localUpdate, ranxiUpdate bool
	}{
		{"neither", "v0.2.8", "v2.8.14", false, false},
		{"local revision", "v0.2.8.18", "v2.8.14", true, false},
		{"ranxi only", "v0.2.8", "v2.8.15", false, true},
		{"both", "v0.2.9", "v2.8.15", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, client := dualSourceService(&updateServiceCacheStub{}, tc.local, tc.ranxi)
			info, err := svc.CheckUpdate(context.Background(), true)
			require.NoError(t, err)
			require.Equal(t, "0.2.8.17", info.CurrentVersion)
			require.Equal(t, tc.localUpdate, info.HasUpdate)
			require.Equal(t, "2.8.14", info.Ranxi.CurrentVersion)
			require.Equal(t, tc.ranxiUpdate, info.Ranxi.HasUpdate)
			require.Equal(t, "local notes", info.ReleaseInfo.Body)
			require.Equal(t, "ranxi notes", info.Ranxi.ReleaseInfo.Body)
			require.Equal(t, []string{githubRepo, ranxiRepo}, client.calls)
		})
	}
}

func TestUpdateServiceRanxiOnlyCannotInstall(t *testing.T) {
	svc, _ := dualSourceService(&updateServiceCacheStub{}, "v0.2.8", "v2.8.15")
	require.ErrorIs(t, svc.PerformUpdate(context.Background()), ErrNoUpdateAvailable)
}

func TestUpdateServiceCacheUsesCurrentBuildVersions(t *testing.T) {
	cache := &updateServiceCacheStub{}
	svc, client := dualSourceService(cache, "v0.2.8.18", "v2.8.15")
	_, err := svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	svc.currentVersion, svc.ranxiVersion = "0.2.8.19", "2.8.16"
	info, err := svc.CheckUpdate(context.Background(), false)
	require.NoError(t, err)
	require.Len(t, client.calls, 2)
	require.True(t, info.Cached)
	require.True(t, info.Ranxi.Cached)
	require.Equal(t, "0.2.8.19", info.CurrentVersion)
	require.Equal(t, "2.8.16", info.Ranxi.CurrentVersion)
	require.False(t, info.HasUpdate)
	require.False(t, info.Ranxi.HasUpdate)
}

func TestUpdateServicePartialFailures(t *testing.T) {
	for _, failedRepo := range []string{githubRepo, ranxiRepo} {
		t.Run(failedRepo, func(t *testing.T) {
			cache := &updateServiceCacheStub{}
			svc, client := dualSourceService(cache, "v0.2.9", "v2.8.15")
			client.failures[failedRepo] = errors.New("offline")
			info, err := svc.CheckUpdate(context.Background(), true)
			require.NoError(t, err)
			if failedRepo == githubRepo {
				require.Contains(t, info.Warning, "offline")
				require.True(t, info.Ranxi.HasUpdate)
				require.Empty(t, info.Ranxi.Warning)
			} else {
				require.True(t, info.HasUpdate)
				require.Empty(t, info.Warning)
				require.Contains(t, info.Ranxi.Warning, "offline")
			}
			delete(client.failures, failedRepo)
			info, err = svc.CheckUpdate(context.Background(), false)
			require.NoError(t, err)
			require.Len(t, client.calls, 3)
			require.Equal(t, failedRepo, client.calls[2])
			require.True(t, info.HasUpdate)
			require.True(t, info.Ranxi.HasUpdate)
		})
	}
}

func TestUpdateServiceFailureDoesNotRefreshCachedTimestamp(t *testing.T) {
	cache := &updateServiceCacheStub{}
	svc, client := dualSourceService(cache, "v0.2.9", "v2.8.15")
	_, err := svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	var before sourceUpdateCache
	require.NoError(t, json.Unmarshal([]byte(cache.data), &before))
	before.Local.Timestamp -= 30
	before.Ranxi.Timestamp -= 30
	raw, err := json.Marshal(before)
	require.NoError(t, err)
	cache.data = string(raw)
	client.failures[githubRepo], client.failures[ranxiRepo] = errors.New("offline"), errors.New("offline")
	info, err := svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.True(t, info.Cached)
	require.True(t, info.Ranxi.Cached)
	require.Contains(t, info.Warning, "offline")
	require.Contains(t, info.Ranxi.Warning, "offline")
	var after sourceUpdateCache
	require.NoError(t, json.Unmarshal([]byte(cache.data), &after))
	require.Equal(t, before, after)
}

func TestUpdateServiceRejectsUnscopedAndExpiredCache(t *testing.T) {
	for _, data := range []string{
		`{"latest":"2.8.99","timestamp":9999999999}`,
		`{"local":{"repository":"ranxi2001/sub2api","latest":"2.8.99","timestamp":9999999999}}`,
		`{"local":{"repository":"Wei-Shaw/sub2api","latest":"0.9.0","timestamp":9999999999}}`,
		`{"local":{"repository":"ccx1/sub2api","latest":"0.9.0","timestamp":1}}`,
	} {
		cache := &updateServiceCacheStub{data: data}
		svc, client := dualSourceService(cache, "v0.2.8", "v2.8.14")
		info, err := svc.CheckUpdate(context.Background(), false)
		require.NoError(t, err)
		require.False(t, info.HasUpdate)
		require.Len(t, client.calls, 2)
	}
}

func TestUpdateServiceLegacyConstructorAndEmptyRelease(t *testing.T) {
	svc, client := dualSourceService(&updateServiceCacheStub{}, "v0.2.8", "v2.8.14")
	svc.ranxiVersion = ""
	client.releases[githubRepo] = nil
	info, err := svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.Nil(t, info.Ranxi)
	require.NotEmpty(t, info.Warning)
	require.False(t, info.HasUpdate)
	require.Len(t, client.calls, 1)
	require.Less(t, compareVersions("0.2.8.17", "0.2.8.18"), 0)
	require.Greater(t, compareVersions("0.2.8.17", "0.2.8"), 0)
}

func TestUpdateServiceExpiresSourcesIndependently(t *testing.T) {
	cache := &updateServiceCacheStub{}
	svc, client := dualSourceService(cache, "v0.2.9", "v2.8.15")
	_, err := svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	var data sourceUpdateCache
	require.NoError(t, json.Unmarshal([]byte(cache.data), &data))
	data.Ranxi.Timestamp = time.Now().Unix() - updateCacheTTL - 1
	raw, err := json.Marshal(data)
	require.NoError(t, err)
	cache.data = string(raw)
	info, err := svc.CheckUpdate(context.Background(), false)
	require.NoError(t, err)
	require.True(t, info.Cached)
	require.False(t, info.Ranxi.Cached)
	require.Equal(t, []string{githubRepo, ranxiRepo, ranxiRepo}, client.calls)
}
