package service

import (
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const PrismBrowserEnabledKey = "openai_prism_browser"

func PrismBrowserExtraKeys() []string {
	return []string{PrismBrowserEnabledKey, PrismBrowserModelsKey}
}

// 缺少模型范围沿用四模型；显式空列表表示不选模型，关闭时清空子配置。
func NormalizePrismBrowserExtra(extra map[string]any) error {
	if raw, exists := extra[PrismBrowserEnabledKey]; exists {
		enabled, ok := raw.(bool)
		if !ok {
			return infraerrors.BadRequest("PRISM_SETTINGS_INVALID", "Prism enabled must be a boolean")
		}
		if !enabled {
			extra[PrismBrowserModelsKey] = []string{}
			return nil
		}
	}
	raw, exists := extra[PrismBrowserModelsKey]
	if !exists {
		return nil
	}
	models := []string{}
	switch values := raw.(type) {
	case []string:
		models = values
	case []any:
		for _, value := range values {
			model, ok := value.(string)
			if !ok {
				return infraerrors.BadRequest("PRISM_SETTINGS_INVALID", "Prism models must be an array of supported model names")
			}
			models = append(models, model)
		}
	default:
		return infraerrors.BadRequest("PRISM_SETTINGS_INVALID", "Prism models must be an array of supported model names")
	}
	normalized := []string{}
	seen := map[string]bool{}
	for _, model := range models {
		model = strings.TrimSpace(model)
		if !isPrismBrowserModel(model) {
			return infraerrors.BadRequest("PRISM_SETTINGS_INVALID", "unsupported Prism model")
		}
		if !seen[model] {
			normalized = append(normalized, model)
			seen[model] = true
		}
	}
	extra[PrismBrowserModelsKey] = normalized
	return nil
}

func ValidatePrismBrowserAccount(account *Account) error {
	if account == nil {
		return nil
	}
	if err := NormalizePrismBrowserExtra(account.Extra); err != nil {
		return err
	}
	if account.Extra[PrismBrowserEnabledKey] == true && !accountHasPrismBrowser(account) {
		return infraerrors.BadRequest("PRISM_ACCOUNT_INVALID", "Prism requires a non-shadow OpenAI OAuth account")
	}
	return nil
}
