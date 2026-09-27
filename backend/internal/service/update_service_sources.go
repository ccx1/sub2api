package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const ranxiRepo = "ranxi2001/sub2api"

// VersionSourceInfo 独立记录合并来源，不参与本地二进制自动更新决策。
type VersionSourceInfo struct {
	Repository     string       `json:"repository"`
	CurrentVersion string       `json:"current_version"`
	LatestVersion  string       `json:"latest_version"`
	HasUpdate      bool         `json:"has_update"`
	ReleaseInfo    *ReleaseInfo `json:"release_info,omitempty"`
	Cached         bool         `json:"cached"`
	Warning        string       `json:"warning,omitempty"`
}

func (s *UpdateService) checkLocalSource(ctx context.Context, force bool, cached *UpdateInfo) *UpdateInfo {
	usable := cached != nil && cached.LatestVersion != ""
	if !force && usable {
		return cached
	}
	info, err := s.fetchLatestRelease(ctx)
	if err == nil {
		return info
	}
	if usable {
		cached.Warning = "Using cached data: " + err.Error()
		return cached
	}
	return &UpdateInfo{CurrentVersion: s.currentVersion, BuildType: s.buildType, Warning: err.Error()}
}

func (s *UpdateService) checkRanxiSource(ctx context.Context, force bool, cached *VersionSourceInfo) *VersionSourceInfo {
	if !force && cached != nil {
		return cached
	}
	info, err := s.fetchSourceRelease(ctx, ranxiRepo, s.ranxiVersion)
	if err == nil {
		return &VersionSourceInfo{Repository: ranxiRepo, CurrentVersion: s.ranxiVersion,
			LatestVersion: info.LatestVersion, HasUpdate: info.HasUpdate, ReleaseInfo: info.ReleaseInfo}
	}
	if cached != nil {
		cached.Warning = "Using cached data: " + err.Error()
		return cached
	}
	return &VersionSourceInfo{Repository: ranxiRepo, CurrentVersion: s.ranxiVersion,
		Warning: err.Error()}
}

type cachedSourceRelease struct {
	Repository  string       `json:"repository"`
	Latest      string       `json:"latest"`
	ReleaseInfo *ReleaseInfo `json:"release_info"`
	Timestamp   int64        `json:"timestamp"`
}

type sourceUpdateCache struct {
	Local *cachedSourceRelease `json:"local,omitempty"`
	Ranxi *cachedSourceRelease `json:"ranxi,omitempty"`
}

func (c *cachedSourceRelease) valid(repo string) bool {
	return c != nil && c.Repository == repo && c.Latest != "" &&
		time.Now().Unix()-c.Timestamp <= updateCacheTTL
}

func (s *UpdateService) readSourceCache(ctx context.Context) sourceUpdateCache {
	var cached sourceUpdateCache
	if s.cache == nil {
		return cached
	}
	data, err := s.cache.GetUpdateInfo(ctx)
	if err == nil {
		_ = json.Unmarshal([]byte(data), &cached)
	}
	return cached
}

func (s *UpdateService) getFromCache(ctx context.Context) (*UpdateInfo, error) {
	cached := s.readSourceCache(ctx)
	info := &UpdateInfo{CurrentVersion: s.currentVersion, BuildType: s.buildType}
	if cached.Local.valid(githubRepo) {
		info.LatestVersion = cached.Local.Latest
		info.ReleaseInfo = cached.Local.ReleaseInfo
		info.HasUpdate = compareVersions(s.currentVersion, cached.Local.Latest) < 0
		info.Cached = true
	}
	if s.ranxiVersion != "" && cached.Ranxi.valid(ranxiRepo) {
		info.Ranxi = &VersionSourceInfo{Repository: ranxiRepo, CurrentVersion: s.ranxiVersion,
			LatestVersion: cached.Ranxi.Latest, ReleaseInfo: cached.Ranxi.ReleaseInfo,
			HasUpdate: compareVersions(s.ranxiVersion, cached.Ranxi.Latest) < 0, Cached: true}
	}
	// 旧缓存没有来源标记，可能来自合并前的 Ranxi updater，必须重新获取。
	if !info.Cached && info.Ranxi == nil {
		return nil, fmt.Errorf("no valid source cache")
	}
	return info, nil
}

func (s *UpdateService) saveToCache(ctx context.Context, info *UpdateInfo) {
	if s.cache == nil {
		return
	}
	cached := s.readSourceCache(ctx)
	changed := false
	if !info.Cached && info.Warning == "" && info.ReleaseInfo != nil {
		cached.Local = &cachedSourceRelease{Repository: githubRepo, Latest: info.LatestVersion,
			ReleaseInfo: info.ReleaseInfo, Timestamp: time.Now().Unix()}
		changed = true
	}
	if r := info.Ranxi; r != nil && !r.Cached && r.Warning == "" && r.ReleaseInfo != nil {
		cached.Ranxi = &cachedSourceRelease{Repository: ranxiRepo, Latest: r.LatestVersion,
			ReleaseInfo: r.ReleaseInfo, Timestamp: time.Now().Unix()}
		changed = true
	}
	// 失败回退不能延长旧数据的有效期。
	if changed {
		data, err := json.Marshal(cached)
		if err == nil {
			_ = s.cache.SetUpdateInfo(ctx, string(data), time.Duration(updateCacheTTL)*time.Second)
		}
	}
}
