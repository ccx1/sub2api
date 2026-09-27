//go:build unit

package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type releaseSourceAuditClient struct {
	sourceReleaseClient
	recentRepositories []string
}

func (c *releaseSourceAuditClient) FetchRecentReleases(ctx context.Context, repo string, perPage int) ([]*GitHubRelease, error) {
	c.recentRepositories = append(c.recentRepositories, repo)
	return c.updateServiceGitHubClientStub.FetchRecentReleases(ctx, repo, perPage)
}

func TestUpdateServiceUsesOwnRepositoryForLatestAndRollback(t *testing.T) {
	svc, original := dualSourceService(&updateServiceCacheStub{}, "v0.2.8.18", "v2.8.15")
	client := &releaseSourceAuditClient{sourceReleaseClient: *original}
	client.recentReleases = []*GitHubRelease{{TagName: "v0.2.8.16"}}
	svc.githubClient = client

	info, err := svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.Equal(t, "0.2.8.18", info.LatestVersion)
	require.True(t, info.HasUpdate)
	require.Equal(t, []string{"ccx1/sub2api", "ranxi2001/sub2api"}, client.calls)

	versions, err := svc.ListRollbackVersions(context.Background())
	require.NoError(t, err)
	require.Len(t, versions, 1)
	require.Equal(t, "0.2.8.16", versions[0].Version)

	// 缺少资产会在下载和二进制替换前退出，仍可验证安装与回滚的来源。
	err = svc.PerformUpdate(context.Background())
	require.ErrorContains(t, err, "no compatible release found")
	err = svc.RollbackToVersion(context.Background(), "0.2.8.16")
	require.ErrorContains(t, err, "no compatible release found")
	require.Equal(t, []string{"ccx1/sub2api", "ranxi2001/sub2api", "ccx1/sub2api", "ranxi2001/sub2api"}, client.calls)
	require.Equal(t, []string{"ccx1/sub2api", "ccx1/sub2api"}, client.recentRepositories)
}

func TestUpdateServiceMissingOwnReleaseDoesNotUseOfficialCache(t *testing.T) {
	for _, cachedRepo := range []string{"", "Wei-Shaw/sub2api"} {
		t.Run("cached_repository_"+cachedRepo, func(t *testing.T) {
			cache := &updateServiceCacheStub{}
			if cachedRepo != "" {
				raw, err := json.Marshal(sourceUpdateCache{Local: &cachedSourceRelease{
					Repository: cachedRepo, Latest: "99.0.0", Timestamp: time.Now().Unix(),
					ReleaseInfo: &ReleaseInfo{HTMLURL: "https://github.com/Wei-Shaw/sub2api/releases/tag/v99.0.0"},
				}})
				require.NoError(t, err)
				cache.data = string(raw)
			}
			svc, client := dualSourceService(cache, "v0.2.8.18", "v2.8.15")
			client.failures[githubRepo] = errors.New("GitHub API returned status 404")

			info, err := svc.CheckUpdate(context.Background(), false)
			require.NoError(t, err)
			require.Equal(t, "0.2.8.17", info.CurrentVersion)
			require.Empty(t, info.LatestVersion)
			require.Nil(t, info.ReleaseInfo)
			require.False(t, info.HasUpdate)
			require.False(t, info.Cached)
			require.Contains(t, info.Warning, "404")
			require.True(t, info.Ranxi.HasUpdate)
			require.Equal(t, []string{"ccx1/sub2api", "ranxi2001/sub2api"}, client.calls)
			require.ErrorIs(t, svc.PerformUpdate(context.Background()), ErrNoUpdateAvailable)
		})
	}
}

func TestUpdateServiceFirstReleaseRecoversAfterMissingRelease(t *testing.T) {
	cache := &updateServiceCacheStub{}
	svc, client := dualSourceService(cache, "v0.2.8.18", "v2.8.14")
	client.failures[githubRepo] = errors.New("GitHub API returned status 404")
	_, err := svc.CheckUpdate(context.Background(), false)
	require.NoError(t, err)

	delete(client.failures, githubRepo)
	info, err := svc.CheckUpdate(context.Background(), false)
	require.NoError(t, err)
	require.Equal(t, "0.2.8.18", info.LatestVersion)
	require.True(t, info.HasUpdate)
	require.Empty(t, info.Warning)
	require.Equal(t, []string{"ccx1/sub2api", "ranxi2001/sub2api", "ccx1/sub2api"}, client.calls)

	var cached sourceUpdateCache
	require.NoError(t, json.Unmarshal([]byte(cache.data), &cached))
	require.Equal(t, "ccx1/sub2api", cached.Local.Repository)
	require.Equal(t, "ranxi2001/sub2api", cached.Ranxi.Repository)
}

type releaseChecksumClient struct {
	updateServiceGitHubClientStub
	checksum []byte
}

func (c *releaseChecksumClient) FetchChecksumFile(context.Context, string) ([]byte, error) {
	return c.checksum, nil
}

func TestUpdateServiceReleaseChecksumMatchesExactAsset(t *testing.T) {
	fileName := "sub2api_0.2.8.18_linux_amd64.tar.gz"
	filePath := filepath.Join(t.TempDir(), fileName)
	content := []byte("release archive checksum fixture")
	require.NoError(t, os.WriteFile(filePath, content, 0600))
	hash := sha256.Sum256(content)
	for _, tc := range []struct {
		name, checksum, wantError string
	}{
		{"exact asset", fmt.Sprintf("%x  %s\n", hash, fileName), ""},
		{"wrong hash", fmt.Sprintf("%064x  %s\n", 0, fileName), "checksum mismatch"},
		{"other asset only", fmt.Sprintf("%x  sub2api_0.2.8.18_linux_arm64.tar.gz\n", hash), "checksum not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &releaseChecksumClient{checksum: []byte(tc.checksum)}
			svc := NewUpdateService(nil, client, "0.2.8.17", "release")
			err := svc.verifyChecksum(context.Background(), filePath, "https://github.com/ccx1/sub2api/releases/download/v0.2.8.18/checksums.txt")
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				return
			}
			require.NoError(t, err)
		})
	}
}
