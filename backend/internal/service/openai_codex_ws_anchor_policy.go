package service

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// 动态成员不改持久化版本，但不能继承旧成员集合建立的借票连接。
func astraWSAnchorRevision(settings config.AstraRoutingSettings) string {
	if !settings.CookiePool.UsesGroups() {
		return settings.Revision
	}
	revision := settings.Revision
	settings.Revision = ""
	settings.AccountScheduling = false
	settings.SchedulingMode = ""
	settings.SchedulingGroupIDs = nil
	raw, _ := json.Marshal(settings)
	return fmt.Sprintf("%s:groups:%x", revision, sha256.Sum256(raw))
}

func (s *OpenAIGatewayService) validateCodexWSGroupPolicy(settings config.AstraRoutingSettings, accountID int64) error {
	if !settings.CookiePool.UsesGroups() {
		return nil
	}
	suffix := ":" + astraWSAnchorRevision(settings)
	store := &s.codexWSAnchors
	stale := map[codexWSAnchorKey]string{}
	store.mu.Lock()
	for key, entry := range store.entries {
		if !store.busy[key] && (settings.SelectionError != "" || !strings.HasSuffix(key.scope, suffix)) {
			delete(store.entries, key)
			stale[key] = entry.connID
		}
	}
	store.mu.Unlock()
	for key, connID := range stale {
		if connID != "" {
			s.getOpenAIWSConnPool().evictConn(key.account, connID)
		}
	}
	if settings.SelectionError != "" {
		return errors.New(settings.SelectionError)
	}
	if !settings.CookiePool.Enabled || (!slices.Contains(settings.CookiePool.SourceAccountIDs, accountID) && !slices.Contains(settings.CookiePool.TargetAccountIDs, accountID)) {
		return errors.New("astra_ws_outside_targets")
	}
	return nil
}
