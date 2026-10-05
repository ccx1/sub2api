package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type grokTempRecoveryRepository interface {
	ClearTempUnschedulableIfUnchanged(context.Context, int64, time.Time, string) (bool, error)
}

type grokTempRecoveryCache interface {
	DeleteTempUnschedIfUnchanged(context.Context, int64, *TempUnschedState) (bool, error)
}

type grokRefreshRuntimeSnapshotKey struct{}

type grokRefreshRuntimeSnapshot struct {
	gateway *OpenAIGatewayService
	block   openAIAccountRuntimeBlockSnapshot
}

func (s *TokenRefreshService) withGrokRefreshRuntimeSnapshot(ctx context.Context, account *Account) context.Context {
	gateway, ok := s.runtimeBlocker.(*OpenAIGatewayService)
	if !ok || !account.IsGrok() || account.TempUnschedulableUntil == nil ||
		!isGrokCredentialUnauthorizedTempReason(account.TempUnschedulableReason) {
		return ctx
	}
	block := gateway.peekOpenAIAccountRuntimeBlock(account)
	if !block.until.Equal(*account.TempUnschedulableUntil) {
		return ctx
	}
	return context.WithValue(ctx, grokRefreshRuntimeSnapshotKey{}, grokRefreshRuntimeSnapshot{gateway, block})
}

func isGrokCredentialUnauthorizedTempReason(reason string) bool {
	reason = strings.TrimSpace(reason)
	if reason == "grok credentials unauthorized" || reason == "grok oauth token unauthorized" {
		return true
	}
	var state TempUnschedState
	return json.Unmarshal([]byte(reason), &state) == nil && state.StatusCode == http.StatusUnauthorized
}

func (s *TokenRefreshService) recoverGrokRefreshCooldown(ctx context.Context, account *Account) (bool, error) {
	if account == nil || account.TempUnschedulableUntil == nil || !account.TempUnschedulableUntil.After(time.Now()) ||
		!isGrokCredentialUnauthorizedTempReason(account.TempUnschedulableReason) {
		return false, nil
	}
	repo, ok := s.accountRepo.(grokTempRecoveryRepository)
	if !ok {
		return false, nil
	}
	expected, cacheCAS, err := s.grokRefreshCacheSnapshot(ctx, account)
	if err != nil || s.tempUnschedCache != nil && cacheCAS == nil {
		return false, err
	}
	cleared, err := repo.ClearTempUnschedulableIfUnchanged(ctx, account.ID, *account.TempUnschedulableUntil, account.TempUnschedulableReason)
	if err != nil || !cleared {
		return false, err
	}
	if cacheCAS != nil {
		cleared, err = cacheCAS.DeleteTempUnschedIfUnchanged(ctx, account.ID, expected)
		if err != nil || !cleared {
			return false, err
		}
	}
	if snapshot, ok := ctx.Value(grokRefreshRuntimeSnapshotKey{}).(grokRefreshRuntimeSnapshot); ok {
		snapshot.gateway.clearOpenAIAccountRuntimeBlockIfUnchanged(account.ID, snapshot.block)
	}
	return true, nil
}

func (s *TokenRefreshService) grokRefreshCacheSnapshot(ctx context.Context, account *Account) (*TempUnschedState, grokTempRecoveryCache, error) {
	if s.tempUnschedCache == nil {
		return nil, nil, nil
	}
	cacheCAS, ok := s.tempUnschedCache.(grokTempRecoveryCache)
	if !ok {
		return nil, nil, nil
	}
	expected := &TempUnschedState{UntilUnix: account.TempUnschedulableUntil.Unix(), StatusCode: http.StatusUnauthorized,
		ErrorMessage: account.TempUnschedulableReason}
	cached, err := s.tempUnschedCache.GetTempUnsched(ctx, account.ID)
	if err != nil {
		return nil, nil, err
	}
	if cached != nil {
		if cached.StatusCode != http.StatusUnauthorized || cached.UntilUnix != expected.UntilUnix {
			return nil, nil, nil
		}
		expected = cached
	}
	return expected, cacheCAS, nil
}
