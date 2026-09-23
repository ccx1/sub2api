package service

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const SettingKeyCodexTicketProtectionDisabledAt = "openai_codex_ticket_protection_disabled_at"

// 与策略在同一事务保存关闭水位；即使两次调度之间重新开启，也不能补回预算。
func (s *SettingService) recordCodexTicketProtectionDisable(ctx context.Context, cfg config.OpenAICodexTicketConfig, values map[string]string) error {
	if cfg.TicketProtection().Enabled {
		return nil
	}
	previous, err := s.readCodexTicketPolicy(ctx, s.defaultCodexTicketSettings())
	if err != nil {
		return err
	}
	if previous.TicketProtection().Enabled {
		values[SettingKeyCodexTicketProtectionDisabledAt] = strconv.FormatInt(time.Now().UnixMilli(), 10)
	}
	return nil
}

func (s *SettingService) GetCodexTicketProtectionDisabledAt(ctx context.Context) (time.Time, error) {
	if s == nil || s.settingRepo == nil {
		return time.Time{}, errors.New("codex ticket settings unavailable")
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyCodexTicketProtectionDisabledAt)
	if errors.Is(err, ErrSettingNotFound) || err == nil && raw == "" {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return time.Time{}, errors.New("invalid codex ticket protection transition")
	}
	return time.UnixMilli(value), nil
}
