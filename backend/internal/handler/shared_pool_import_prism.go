package handler

import (
	"encoding/json"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func sharedImportPrismModels(extra map[string]any) []string {
	scope := map[string]any{service.PrismBrowserEnabledKey: true}
	if value, exists := extra[service.PrismBrowserModelsKey]; exists {
		scope[service.PrismBrowserModelsKey] = value
	}
	account := &service.Account{Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Extra: scope}
	models := []string{}
	for _, model := range service.PrismBrowserSupportedModels() {
		if account.IsPrismBrowserEnabledForModel(model) {
			models = append(models, model)
		}
	}
	return models
}

// Read only this protocol's fields; imported owner, routing and permissions are ignored.
func applySharedImportFilePrism(input *service.SharedPoolAccountInput, object map[string]any) error {
	extra, _ := object["extra"].(map[string]any)
	patch := map[string]any{}
	for _, key := range service.PrismBrowserExtraKeys() {
		if value, ok := extra[key]; ok {
			patch[key] = value
		}
	}
	if len(patch) == 0 {
		return nil
	}
	if _, ok := patch["openai_prism_browser"]; !ok {
		patch["openai_prism_browser"] = false
	}
	if err := service.NormalizePrismBrowserExtra(patch); err != nil {
		return err
	}
	account := &service.Account{Platform: input.Platform, Type: input.Type, Credentials: input.Credentials, Extra: patch}
	if err := service.ValidatePrismBrowserAccount(account); err != nil {
		return err
	}
	enabled, ok := patch["openai_prism_browser"].(bool)
	if !ok {
		return fmt.Errorf("invalid Prism enabled setting")
	}
	input.PrismBrowserEnabled = &enabled
	input.PrismBrowserModels = nil
	if _, exists := patch[service.PrismBrowserModelsKey]; !exists {
		return nil
	}
	encoded, err := json.Marshal(patch[service.PrismBrowserModelsKey])
	if err != nil {
		return err
	}
	models := []string{}
	if err := json.Unmarshal(encoded, &models); err != nil {
		return err
	}
	input.PrismBrowserModels = &models
	return nil
}
