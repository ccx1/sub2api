package service

import (
	"context"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type PrismBrowserRuntime struct {
	Enabled bool
	BaseURL string
	APIKey  string
}

func configuredPrismBrowserRuntime(cfg *config.Config) PrismBrowserRuntime {
	runtime := PrismBrowserRuntime{BaseURL: "http://127.0.0.1:8319/v1"}
	if cfg != nil {
		runtime.Enabled = cfg.Gateway.PrismBrowser.Enabled
		if value := strings.TrimSpace(cfg.Gateway.PrismBrowser.BaseURL); value != "" {
			runtime.BaseURL = value
		}
		runtime.APIKey = strings.TrimSpace(cfg.Gateway.PrismBrowser.APIKey)
	}
	return runtime
}

func resolvePrismBrowserRuntime(values map[string]string, fallback PrismBrowserRuntime) PrismBrowserRuntime {
	if value, present := values[SettingKeyPrismBrowserEnabled]; present {
		fallback.Enabled = value == "true"
	}
	if value, present := values[SettingKeyPrismBrowserBaseURL]; present {
		fallback.BaseURL = strings.TrimSpace(value)
	}
	if value, present := values[SettingKeyPrismBrowserAPIKey]; present {
		fallback.APIKey = strings.TrimSpace(value)
	}
	return fallback
}

// Missing keys keep cfg/env values. A failed database read cannot enable a bridge.
func (s *SettingService) GetPrismBrowserRuntime(ctx context.Context) PrismBrowserRuntime {
	if s == nil {
		return configuredPrismBrowserRuntime(nil)
	}
	fallback := configuredPrismBrowserRuntime(s.cfg)
	if s.settingRepo == nil {
		return fallback
	}
	values, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyPrismBrowserEnabled, SettingKeyPrismBrowserBaseURL, SettingKeyPrismBrowserAPIKey})
	if err != nil {
		return PrismBrowserRuntime{}
	}
	return resolvePrismBrowserRuntime(values, fallback)
}

func (s *OpenAIGatewayService) prismBrowserRuntime(ctx context.Context) PrismBrowserRuntime {
	if s.settingService != nil {
		return s.settingService.GetPrismBrowserRuntime(ctx)
	}
	return configuredPrismBrowserRuntime(s.cfg)
}

func ValidatePrismBrowserRuntime(runtime PrismBrowserRuntime) error {
	if runtime.BaseURL != "" {
		if _, err := prismBrowserAdapterURL(runtime.BaseURL); err != nil {
			return err
		}
	}
	if strings.ContainsAny(runtime.APIKey, "\r\n") {
		return errors.New("Prism bridge API key must not contain line breaks")
	}
	if runtime.Enabled && (runtime.BaseURL == "" || runtime.APIKey == "") {
		return errors.New("Prism bridge requires a loopback URL and an adapter API key")
	}
	return nil
}
