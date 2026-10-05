package service

import infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"

// Shared contributors may replace only the Prism configuration family.
func sharedPrismBrowserExtra(in SharedPoolAccountInput, account *Account) (map[string]any, error) {
	if in.PrismBrowserEnabled == nil {
		if in.PrismBrowserModels != nil {
			return nil, infraerrors.BadRequest("PRISM_MODELS_REQUIRE_ENABLED", "Prism model scope requires an explicit enabled setting")
		}
		return nil, nil
	}
	extra := map[string]any{"openai_prism_browser": *in.PrismBrowserEnabled}
	if in.PrismBrowserModels != nil {
		extra[PrismBrowserModelsKey] = append([]string{}, (*in.PrismBrowserModels)...)
	}
	if err := NormalizePrismBrowserExtra(extra); err != nil {
		return nil, err
	}
	candidate := *account
	candidate.Extra = extra
	if err := ValidatePrismBrowserAccount(&candidate); err != nil {
		return nil, err
	}
	return extra, nil
}

func sharedPrismBrowserView(account *Account, view *SharedPoolAccountView) {
	if account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth || account.IsShadow() {
		return
	}
	enabled := accountHasPrismBrowser(account)
	view.PrismBrowserEnabled = &enabled
	models := []string{}
	for _, model := range PrismBrowserSupportedModels() {
		if account.isPrismBrowserUpstreamModelEnabled(model) {
			models = append(models, model)
		}
	}
	view.PrismBrowserModels = &models
}
