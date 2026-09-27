package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalProxyCountry(t *testing.T) {
	for raw, want := range map[string]string{
		"US": "US", " us ": "US", "USA": "US", "U.S.A.": "US", "united states": "US", "United  States": "US",
		"美国": "US", "UK": "GB", "United-Kingdom": "GB", "jpn": "JP", "日本": "JP",
		"": "", "Atlantis": "", "U1": "", "not-a-country": "",
	} {
		require.Equal(t, want, CanonicalProxyCountry(raw), raw)
	}
}

func TestNormalizeProxyCountryCodeAcceptsAliases(t *testing.T) {
	got, err := NormalizeProxyCountryCode(" usa ")
	require.NoError(t, err)
	require.Equal(t, "US", got)
	got, err = NormalizeProxyCountryCode("")
	require.NoError(t, err)
	require.Empty(t, got)
	_, err = NormalizeProxyCountryCode("Atlantis")
	require.Error(t, err)
}

func TestResolveAccountProxyPoolSelectionCarriesDefaultCountry(t *testing.T) {
	account := &Account{ID: 7, Credentials: map[string]any{"price_country": "JP"}, Extra: map[string]any{
		ProxyModeExtraKey: ProxyModeRandom, ProxyRegionModeExtraKey: "billing", ProxyRegionFallbackCountryExtraKey: " united states ",
	}}
	selection, err := ResolveAccountProxyPoolSelection(context.Background(), account, nil)
	require.NoError(t, err)
	require.Equal(t, "JP", selection.CountryCode)
	require.Equal(t, "US", selection.FallbackCountryCode)
	require.True(t, selection.AllowCountryFallback)

	// 账单无法识别时默认地区已直接作为账号地区，不再重复作为下一层。
	delete(account.Credentials, "price_country")
	selection, err = ResolveAccountProxyPoolSelection(context.Background(), account, nil)
	require.NoError(t, err)
	require.Equal(t, "US", selection.CountryCode)
	require.Empty(t, selection.FallbackCountryCode)

	// 手动指定地区时不使用默认代理地区。
	account.Extra[ProxyRegionModeExtraKey], account.Extra[ProxyRegionCountryExtraKey] = "manual", "JP"
	selection, err = ResolveAccountProxyPoolSelection(context.Background(), account, nil)
	require.NoError(t, err)
	require.Equal(t, "JP", selection.CountryCode)
	require.Empty(t, selection.FallbackCountryCode)
}

func TestNormalizeProxyModeExtraCanonicalizesRegionAliases(t *testing.T) {
	extra := NormalizeProxyModeExtra(map[string]any{ProxyRegionCountryExtraKey: " Japan ", ProxyRegionFallbackCountryExtraKey: "usa"})
	require.Equal(t, "JP", extra[ProxyRegionCountryExtraKey])
	require.Equal(t, "US", extra[ProxyRegionFallbackCountryExtraKey])
	extra = NormalizeProxyModeExtra(map[string]any{ProxyRegionCountryExtraKey: "atlantis"})
	require.Equal(t, "ATLANTIS", extra[ProxyRegionCountryExtraKey], "无法识别的值保留原样交给校验报错")
}
