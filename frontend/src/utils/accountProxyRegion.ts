export type AccountProxyRegionMode = 'off' | 'billing' | 'manual'
export interface AccountProxyRegionSelection { mode: AccountProxyRegionMode; country: string }

export const BILLING_CURRENCY_COUNTRIES: Record<string, string> = {
  JPY: 'JP', PHP: 'PH', KRW: 'KR', INR: 'IN', CNY: 'CN', GBP: 'GB', AUD: 'AU', CAD: 'CA',
  HKD: 'HK', TWD: 'TW', THB: 'TH', VND: 'VN', IDR: 'ID', MYR: 'MY', SGD: 'SG'
}

export function normalizeProxyRegionCountry(value: unknown): string {
  const country = typeof value === 'string' ? value.trim().toUpperCase() : ''
  return /^[A-Z]{2}$/.test(country) ? country : ''
}

export function filterProxiesByRegion<T extends { id: number; country_code?: string }>(proxies: T[], country: string, selectedId?: number | null): T[] {
  const target = normalizeProxyRegionCountry(country)
  if (!target) return proxies
  return proxies.filter(proxy => proxy.id === selectedId || normalizeProxyRegionCountry(proxy.country_code) === target)
}

export function readAccountProxyRegion(extra?: Record<string, unknown> | null): AccountProxyRegionSelection {
  const mode = extra?.proxy_region_mode
  return { mode: mode === 'billing' || mode === 'manual' ? mode : 'off', country: normalizeProxyRegionCountry(extra?.proxy_region_country) }
}

export function resolveAccountProxyRegion(selection: AccountProxyRegionSelection, credentials?: Record<string, unknown> | null, fallbackCountry?: unknown) {
  const currency = typeof credentials?.billing_currency === 'string' ? credentials.billing_currency.trim().toUpperCase() : ''
  if (selection.mode === 'off') return { country: '', currency, source: 'off' }
  if (selection.mode === 'manual') return { country: normalizeProxyRegionCountry(selection.country), currency, source: 'manual' }
  const priceCountry = normalizeProxyRegionCountry(credentials?.price_country)
  if (priceCountry) return { country: priceCountry, currency, source: 'price_country' }
  const country = BILLING_CURRENCY_COUNTRIES[currency] || ''
  if (country) return { country, currency, source: 'billing_currency' }
  const fallback = normalizeProxyRegionCountry(fallbackCountry)
  return { country: fallback, currency, source: fallback ? 'fallback_country' : 'unknown' }
}

export function accountProxyRegionValidationError(selection: AccountProxyRegionSelection): string | null {
  return selection.mode === 'manual' && !normalizeProxyRegionCountry(selection.country)
    ? 'admin.accounts.proxyRegion.countryRequired' : null
}

export function accountProxyRegionExtra(selection: AccountProxyRegionSelection): Record<string, unknown> {
  return { proxy_region_mode: selection.mode, proxy_region_country: selection.mode === 'manual' ? normalizeProxyRegionCountry(selection.country) : '' }
}
