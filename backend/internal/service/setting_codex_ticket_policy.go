package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const SettingKeyCodexTicketPolicy = "openai_codex_ticket_policy"

type cachedCodexTicketSettings struct {
	config    config.OpenAICodexTicketConfig
	expiresAt time.Time
}

func cloneCodexTicketSettings(cfg config.OpenAICodexTicketConfig) config.OpenAICodexTicketConfig {
	cfg.Models = slices.Clone(cfg.Models)
	cfg.RejectedLengths = slices.Clone(cfg.RejectedLengths)
	cfg.RetryBackoffSeconds = slices.Clone(cfg.RetryBackoffSeconds)
	cfg.TierRules = slices.Clone(cfg.TierRules)
	for i := range cfg.TierRules {
		cfg.TierRules[i].Aliases = slices.Clone(cfg.TierRules[i].Aliases)
	}
	return cfg
}

func (s *SettingService) defaultCodexTicketSettings() config.OpenAICodexTicketConfig {
	cfg := config.OpenAICodexTicketConfig{FailClosed: true}
	if s != nil && s.cfg != nil {
		cfg = s.cfg.Gateway.OpenAICodexTicket
	}
	return config.NormalizeOpenAICodexTicketConfig(cfg)
}

func (s *SettingService) readCodexTicketPolicy(ctx context.Context, fallback config.OpenAICodexTicketConfig) (config.OpenAICodexTicketConfig, error) {
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyCodexTicketPolicy)
	if errors.Is(err, ErrSettingNotFound) || err == nil && raw == "" {
		return cloneCodexTicketSettings(fallback), nil
	}
	if err != nil {
		return fallback, fmt.Errorf("read codex ticket policy: %w", err)
	}
	var cfg config.OpenAICodexTicketConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return fallback, fmt.Errorf("invalid stored codex ticket policy")
	}
	if err := validateCodexTicketPolicy(&cfg); err != nil {
		return fallback, err
	}
	cfg.HarvestProxyURL = fallback.HarvestProxyURL
	return cfg, nil
}

// 管理端直读存储，读取失败不显示可保存的伪默认值。
func (s *SettingService) GetCodexTicketSettings(ctx context.Context) (config.OpenAICodexTicketConfig, error) {
	fallback := s.defaultCodexTicketSettings()
	if s == nil || s.settingRepo == nil {
		return fallback, infraerrors.ServiceUnavailable("CODEX_TICKET_SETTINGS_UNAVAILABLE", "打票配置服务不可用")
	}
	s.codexTicketSettingsMu.Lock()
	defer s.codexTicketSettingsMu.Unlock()
	cfg, err := s.readCodexTicketPolicy(ctx, fallback)
	if err != nil {
		return cfg, err
	}
	value, err := s.settingRepo.GetValue(ctx, SettingKeyOpenAICodexTicketEnabled)
	if err != nil && !errors.Is(err, ErrSettingNotFound) {
		return cfg, err
	}
	cfg.Enabled = fallback.Enabled
	if err == nil && value != "" {
		cfg.Enabled = value == "true"
	}
	return cfg, nil
}

// 一条 JSON 保存整份规则，与旧总开关键放在同一事务，避免半份配置生效。
func (s *SettingService) UpdateCodexTicketSettings(ctx context.Context, cfg config.OpenAICodexTicketConfig) (config.OpenAICodexTicketConfig, error) {
	cfg = cloneCodexTicketSettings(cfg)
	if err := validateCodexTicketPolicy(&cfg); err != nil {
		return cfg, err
	}
	if s == nil || s.settingRepo == nil {
		return cfg, infraerrors.ServiceUnavailable("CODEX_TICKET_SETTINGS_UNAVAILABLE", "打票配置服务不可用")
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return cfg, err
	}
	s.codexTicketPublishMu.Lock()
	defer s.codexTicketPublishMu.Unlock()
	s.codexTicketSettingsMu.Lock()
	defer s.codexTicketSettingsMu.Unlock()
	if err := s.settingRepo.SetMultiple(ctx, map[string]string{SettingKeyCodexTicketPolicy: string(raw), SettingKeyOpenAICodexTicketEnabled: strconv.FormatBool(cfg.Enabled)}); err != nil {
		return cfg, err
	}
	cfg.HarvestProxyURL = s.defaultCodexTicketSettings().HarvestProxyURL
	s.codexTicketSettingsCache.Store(&cachedCodexTicketSettings{config: cloneCodexTicketSettings(cfg), expiresAt: time.Now().Add(5 * time.Second)})
	s.cacheOpenAICodexTicketEnabled(cfg.Enabled)
	return cfg, nil
}

// 热路径取不可变快照；同实例保存立即更新，其余实例最多五秒刷新一次。
// 暂时读库失败保留最近一次有效规则，避免恢复为错误套餐长度。
func (s *SettingService) GetOpenAICodexTicketRuntimeConfig(ctx context.Context, fallback config.OpenAICodexTicketConfig) config.OpenAICodexTicketConfig {
	fallback = config.NormalizeOpenAICodexTicketConfig(fallback)
	if s == nil || s.settingRepo == nil {
		return cloneCodexTicketSettings(fallback)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cached := s.codexTicketSettingsCache.Load()
	if cached == nil || !time.Now().Before(cached.expiresAt) {
		cached = s.refreshCodexTicketPolicy(ctx, fallback)
	}
	cfg := cloneCodexTicketSettings(cached.config)
	cfg.HarvestProxyURL = fallback.HarvestProxyURL
	cfg.Enabled = s.GetOpenAICodexTicketEnabled(ctx, fallback.Enabled)
	return cfg
}

func (s *SettingService) refreshCodexTicketPolicy(ctx context.Context, fallback config.OpenAICodexTicketConfig) *cachedCodexTicketSettings {
	s.codexTicketSettingsMu.Lock()
	defer s.codexTicketSettingsMu.Unlock()
	cached := s.codexTicketSettingsCache.Load()
	if cached != nil && time.Now().Before(cached.expiresAt) {
		return cached
	}
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cfg, err := s.readCodexTicketPolicy(dbCtx, fallback)
	ttl := 5 * time.Second
	if err != nil {
		ttl = time.Second
		cfg = fallback
		if cached != nil {
			cfg = cached.config
		}
	}
	cached = &cachedCodexTicketSettings{config: cloneCodexTicketSettings(cfg), expiresAt: time.Now().Add(ttl)}
	s.codexTicketSettingsCache.Store(cached)
	return cached
}
