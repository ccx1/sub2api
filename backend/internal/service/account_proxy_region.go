package service

import (
	"context"
	"errors"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	ProxyRegionModeExtraKey    = "proxy_region_mode"
	ProxyRegionCountryExtraKey = "proxy_region_country"
)

var ErrProxyRegionUnknown = errors.New("proxy region cannot be determined from billing; choose a country manually")

type ProxyRegionMatcher interface {
	MatchesProxyRegion(context.Context, *Proxy, string) (bool, error)
}

// 币种映射是代理选路偏好；USD、EUR 等多地区币种不推断国家。
var proxyBillingCurrencyCountries = map[string]string{
	"JPY": "JP", "PHP": "PH", "KRW": "KR", "INR": "IN", "CNY": "CN",
	"GBP": "GB", "AUD": "AU", "CAD": "CA", "HKD": "HK", "TWD": "TW",
	"THB": "TH", "VND": "VN", "IDR": "ID", "MYR": "MY", "SGD": "SG",
}

func normalizeProxyRegionCountry(raw string) string {
	value := strings.ToUpper(strings.TrimSpace(raw))
	if len(value) != 2 || value[0] < 'A' || value[0] > 'Z' || value[1] < 'A' || value[1] > 'Z' {
		return ""
	}
	return value
}

func ValidateProxyRegionExtra(extra map[string]any) error {
	mode, ok := extra[ProxyRegionModeExtraKey].(string)
	if !ok && extra[ProxyRegionModeExtraKey] != nil {
		return infraerrors.BadRequest("INVALID_PROXY_REGION_MODE", "代理地区模式无效")
	}
	mode = strings.TrimSpace(mode)
	if mode != "" && mode != "off" && mode != "billing" && mode != "manual" {
		return infraerrors.BadRequest("INVALID_PROXY_REGION_MODE", "代理地区模式必须是关闭、账单地区或手动指定")
	}
	country, ok := extra[ProxyRegionCountryExtraKey].(string)
	if (!ok && extra[ProxyRegionCountryExtraKey] != nil) || (strings.TrimSpace(country) != "" && normalizeProxyRegionCountry(country) == "") || (mode == "manual" && normalizeProxyRegionCountry(country) == "") {
		return infraerrors.BadRequest("INVALID_PROXY_REGION_COUNTRY", "请选择有效的两位国家代码")
	}
	return nil
}

func (a *Account) ProxyRegionCountry() (string, error) {
	if a == nil {
		return "", nil
	}
	if err := ValidateProxyRegionExtra(a.Extra); err != nil {
		return "", err
	}
	mode, _ := a.Extra[ProxyRegionModeExtraKey].(string)
	switch strings.TrimSpace(mode) {
	case "manual":
		country, _ := a.Extra[ProxyRegionCountryExtraKey].(string)
		return normalizeProxyRegionCountry(country), nil
	case "billing":
		if country := normalizeProxyRegionCountry(a.GetCredential("price_country")); country != "" {
			return country, nil
		}
		if country := proxyBillingCurrencyCountries[strings.ToUpper(strings.TrimSpace(a.GetCredential("billing_currency")))]; country != "" {
			return country, nil
		}
		return "", ErrProxyRegionUnknown
	case "off":
		return "", nil
	default:
		// OAuth accounts carry the subscription billing metadata returned by the
		// provider. Random proxy selection uses it automatically; missing or
		// ambiguous metadata deliberately leaves the pool unrestricted.
		if a.Type == AccountTypeOAuth {
			if country := normalizeProxyRegionCountry(a.GetCredential("price_country")); country != "" {
				return country, nil
			}
			if country := proxyBillingCurrencyCountries[strings.ToUpper(strings.TrimSpace(a.GetCredential("billing_currency")))]; country != "" {
				return country, nil
			}
		}
		return "", nil
	}
}

func normalizeProxyRegionExtra(extra map[string]any) {
	if mode, ok := extra[ProxyRegionModeExtraKey].(string); ok {
		extra[ProxyRegionModeExtraKey] = strings.TrimSpace(mode)
	}
	if country, ok := extra[ProxyRegionCountryExtraKey].(string); ok {
		extra[ProxyRegionCountryExtraKey] = strings.ToUpper(strings.TrimSpace(country))
	}
}
