package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
)

type grokModelRecoveryRepository interface {
	ClearGrokModelRateLimitIfUnchanged(context.Context, int64, string, map[string]any) (bool, error)
}

type grokSuccessfulTestSnapshotKey struct{}

type grokSuccessfulTestSnapshot struct {
	id           int64
	model        string
	modelKey     string
	teamKey      string
	modelBlock   grokModelQuotaBlock
	teamBlock    grokTeamModelRateLimit
	modelExists  bool
	teamExists   bool
	durableLimit map[string]any
}

func (s *RateLimitService) WithSuccessfulTestRecoverySnapshot(ctx context.Context, id int64, model string) (context.Context, error) {
	account, err := s.accountRepo.GetByID(ctx, id)
	if err != nil {
		return ctx, err
	}
	model = strings.TrimSpace(model)
	if account == nil || !account.IsGrok() || model == "" {
		return ctx, nil
	}
	snapshot := grokSuccessfulTestSnapshot{id: id, model: model, modelKey: grokModelQuotaBlockKey(id, model),
		teamKey:      grokTeamModelRateLimitKey(grokTeamFingerprint(accountGrokTeamID(account)), model),
		durableLimit: grokRecoverableModelLimit(account, model)}
	globalGrokModelQuotaBlocks.mu.Lock()
	snapshot.modelBlock, snapshot.modelExists = globalGrokModelQuotaBlocks.items[snapshot.modelKey]
	globalGrokModelQuotaBlocks.mu.Unlock()
	globalGrokTeamModelRateLimits.mu.Lock()
	snapshot.teamBlock, snapshot.teamExists = globalGrokTeamModelRateLimits.items[snapshot.teamKey]
	globalGrokTeamModelRateLimits.mu.Unlock()
	return context.WithValue(ctx, grokSuccessfulTestSnapshotKey{}, snapshot), nil
}

func (s *RateLimitService) RecoverAccountAfterSuccessfulTestForModel(ctx context.Context, id int64, model string) (*SuccessfulTestRecoveryResult, error) {
	account, err := s.accountRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if account != nil && account.IsGrok() {
		return s.RecoverGrokAccountAfterSuccessfulTest(ctx, id, model)
	}
	return s.recoverAccountState(ctx, account, AccountRecoveryOptions{})
}

// 成功测活只证明本模型可用，不解除其它模型、管理员停用或账号级额度保护。
func (s *RateLimitService) RecoverGrokAccountAfterSuccessfulTest(ctx context.Context, id int64, model string) (*SuccessfulTestRecoveryResult, error) {
	account, err := s.accountRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	result := &SuccessfulTestRecoveryResult{}
	if account == nil || !account.IsGrok() || account.Status != StatusActive || !account.Schedulable || strings.TrimSpace(model) == "" {
		return result, nil
	}
	snapshot, ok := ctx.Value(grokSuccessfulTestSnapshotKey{}).(grokSuccessfulTestSnapshot)
	if !ok || snapshot.id != id || !strings.EqualFold(snapshot.model, strings.TrimSpace(model)) {
		return result, nil
	}
	if expected := snapshot.durableLimit; expected != nil {
		if repo, ok := s.accountRepo.(grokModelRecoveryRepository); ok {
			result.ClearedRateLimit, err = repo.ClearGrokModelRateLimitIfUnchanged(ctx, id, model, expected)
			if err != nil {
				return nil, err
			}
		}
	}
	globalGrokModelQuotaBlocks.mu.Lock()
	if snapshot.modelExists && globalGrokModelQuotaBlocks.items[snapshot.modelKey] == snapshot.modelBlock {
		delete(globalGrokModelQuotaBlocks.items, snapshot.modelKey)
		result.ClearedRateLimit = true
	}
	globalGrokModelQuotaBlocks.mu.Unlock()
	globalGrokTeamModelRateLimits.mu.Lock()
	if snapshot.teamExists && globalGrokTeamModelRateLimits.items[snapshot.teamKey] == snapshot.teamBlock {
		delete(globalGrokTeamModelRateLimits.items, snapshot.teamKey)
		result.ClearedRateLimit = true
	}
	globalGrokTeamModelRateLimits.mu.Unlock()
	return result, nil
}

func grokRecoverableModelLimit(account *Account, model string) map[string]any {
	raw, err := json.Marshal(account.Extra["model_rate_limits"])
	if err != nil {
		return nil
	}
	var limits map[string]map[string]any
	if json.Unmarshal(raw, &limits) != nil {
		return nil
	}
	limit := limits[model]
	reason, _ := limit["reason"].(string)
	// 自定义规则的 JSON 原因属于管理员保护，不能由探针绕过。
	if !strings.HasPrefix(reason, "grok ") || strings.Contains(reason, "configured") {
		return nil
	}
	return limit
}

func clearGrokModelQuotaBlocksForAccount(id int64) {
	suffix := "|" + strconv.FormatInt(id, 10)
	globalGrokModelQuotaBlocks.mu.Lock()
	defer globalGrokModelQuotaBlocks.mu.Unlock()
	for key := range globalGrokModelQuotaBlocks.items {
		if strings.HasSuffix(key, suffix) {
			delete(globalGrokModelQuotaBlocks.items, key)
		}
	}
}

func clearGrokTeamModelRateLimitsForAccount(account *Account) {
	if account == nil || !account.IsGrokOAuth() {
		return
	}
	fingerprint := grokTeamFingerprint(accountGrokTeamID(account))
	if fingerprint == "" {
		return
	}
	globalGrokTeamModelRateLimits.mu.Lock()
	defer globalGrokTeamModelRateLimits.mu.Unlock()
	for key := range globalGrokTeamModelRateLimits.items {
		if strings.HasPrefix(key, fingerprint+"|") {
			delete(globalGrokTeamModelRateLimits.items, key)
		}
	}
}

func (s *RateLimitService) clearGrokProcessLocalBlocks(ctx context.Context, id int64, observed ...*Account) {
	if s == nil || id <= 0 {
		return
	}
	var account *Account
	if len(observed) > 0 {
		account = observed[0]
	}
	if account == nil {
		loaded, err := s.accountRepo.GetByID(ctx, id)
		if err != nil {
			return
		}
		account = loaded
	}
	if account == nil || !account.IsGrok() {
		return
	}
	clearGrokModelQuotaBlocksForAccount(id)
	clearGrokTeamModelRateLimitsForAccount(account)
}
