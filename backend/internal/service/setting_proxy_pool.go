package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	OpenAICodexTicketHarvestProxyModeFixed = "fixed"
	OpenAICodexTicketHarvestProxyModePool  = "pool"
	ProxyPoolMaxAccountsLimit              = 10000
	proxyPoolSettingsCacheTTL              = 5 * time.Second
)

type cachedProxyPoolSettings struct {
	values    map[string]string
	expiresAt time.Time
}

func (s *SettingService) defaultCodexTicketHarvestProxyMode(proxyURL string) string {
	if strings.TrimSpace(proxyURL) != "" {
		return OpenAICodexTicketHarvestProxyModeFixed
	}
	if s != nil && s.cfg != nil && strings.TrimSpace(s.cfg.Gateway.OpenAICodexTicket.HarvestProxyURL) != "" {
		return OpenAICodexTicketHarvestProxyModeFixed
	}
	return OpenAICodexTicketHarvestProxyModePool
}

func (s *SettingService) resolveCodexTicketHarvestProxyMode(raw, proxyURL string) (string, error) {
	mode := strings.TrimSpace(raw)
	if mode == "" {
		return s.defaultCodexTicketHarvestProxyMode(proxyURL), nil
	}
	if mode != OpenAICodexTicketHarvestProxyModeFixed && mode != OpenAICodexTicketHarvestProxyModePool {
		return "", fmt.Errorf("%s must be fixed or pool", SettingKeyOpenAICodexTicketHarvestProxyMode)
	}
	return mode, nil
}

func validateProxyPoolMaxAccounts(value int) error {
	if value < 0 || value > ProxyPoolMaxAccountsLimit {
		return fmt.Errorf("%s must be between 0 and %d", SettingKeyProxyPoolMaxAccounts, ProxyPoolMaxAccountsLimit)
	}
	return nil
}

func parseProxyPoolMaxAccounts(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer between 0 and %d", SettingKeyProxyPoolMaxAccounts, ProxyPoolMaxAccountsLimit)
	}
	return value, validateProxyPoolMaxAccounts(value)
}

func (s *SettingService) GetOpenAICodexTicketHarvestProxyMode(ctx context.Context) (string, error) {
	values, err := s.getProxyPoolSettingValues(ctx)
	if err != nil {
		return "", err
	}
	return s.resolveCodexTicketHarvestProxyMode(values[SettingKeyOpenAICodexTicketHarvestProxyMode], values[SettingKeyOpenAICodexTicketHarvestProxyURL])
}

func (s *SettingService) GetProxyPoolMaxAccounts(ctx context.Context) (int, error) {
	values, err := s.getProxyPoolSettingValues(ctx)
	if err != nil {
		return 0, err
	}
	return parseProxyPoolMaxAccounts(values[SettingKeyProxyPoolMaxAccounts])
}

func (s *SettingService) getProxyPoolSettingValues(ctx context.Context) (map[string]string, error) {
	if s == nil || s.settingRepo == nil {
		return nil, fmt.Errorf("proxy pool settings are unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.proxyPoolSettingsMu.Lock()
	defer s.proxyPoolSettingsMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cached := s.proxyPoolSettingsCache; cached != nil && time.Now().Before(cached.expiresAt) {
		return cached.values, nil
	}
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	values, err := s.settingRepo.GetMultiple(dbCtx, []string{
		SettingKeyOpenAICodexTicketHarvestProxyMode,
		SettingKeyOpenAICodexTicketHarvestProxyURL,
		SettingKeyProxyPoolMaxAccounts,
	})
	if err != nil {
		return nil, fmt.Errorf("read proxy pool settings: %w", err)
	}
	if err := dbCtx.Err(); err != nil {
		return nil, err
	}
	s.proxyPoolSettingsCache = &cachedProxyPoolSettings{values: values, expiresAt: time.Now().Add(proxyPoolSettingsCacheTTL)}
	return values, nil
}

func (s *SettingService) InvalidateProxyPoolSettingsCache() {
	if s == nil {
		return
	}
	s.proxyPoolSettingsMu.Lock()
	s.proxyPoolSettingsCache = nil
	s.proxyPoolSettingsMu.Unlock()
}
