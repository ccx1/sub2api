package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const SettingKeyCodexRequestStrategy = "openai_codex_request_strategy_policy"

func (s *SettingService) GetCodexRequestStrategyPolicy(ctx context.Context) (CodexRequestStrategyPolicy, error) {
	policy := DefaultCodexRequestStrategyPolicy()
	if s == nil || s.settingRepo == nil {
		return policy, infraerrors.ServiceUnavailable("REQUEST_STRATEGY_UNAVAILABLE", "Request strategy settings unavailable")
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyCodexRequestStrategy)
	if errors.Is(err, ErrSettingNotFound) || (err == nil && strings.TrimSpace(raw) == "") {
		return policy, nil
	}
	if err != nil {
		return policy, err
	}
	if !strings.HasPrefix(strings.TrimSpace(raw), "{") {
		return policy, errors.New("invalid stored request strategy policy")
	}
	if err := json.Unmarshal([]byte(raw), &policy); err != nil {
		return DefaultCodexRequestStrategyPolicy(), errors.New("invalid stored request strategy policy")
	}
	policy = normalizeCodexRequestStrategyPolicy(policy)
	if err := ValidateCodexRequestStrategyPolicy(policy); err != nil {
		return DefaultCodexRequestStrategyPolicy(), err
	}
	return policy, nil
}

func (s *SettingService) UpdateCodexRequestStrategyPolicy(ctx context.Context, policy CodexRequestStrategyPolicy) (CodexRequestStrategyPolicy, error) {
	policy = normalizeCodexRequestStrategyPolicy(policy)
	if err := ValidateCodexRequestStrategyPolicy(policy); err != nil {
		return policy, infraerrors.BadRequest("INVALID_REQUEST_STRATEGY_POLICY", err.Error())
	}
	if s == nil || s.settingRepo == nil {
		return policy, infraerrors.ServiceUnavailable("REQUEST_STRATEGY_UNAVAILABLE", "Request strategy settings unavailable")
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		return policy, err
	}
	s.codexTicketPublishMu.Lock()
	defer s.codexTicketPublishMu.Unlock()
	if err := s.settingRepo.SetMultiple(ctx, map[string]string{SettingKeyCodexRequestStrategy: string(raw)}); err != nil {
		return policy, err
	}
	return policy, nil
}
