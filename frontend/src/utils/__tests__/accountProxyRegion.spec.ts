import { describe, expect, it } from 'vitest'
import { accountProxyRegionExtra, accountProxyRegionValidationError, filterProxiesByRegion, readAccountProxyRegion, resolveAccountProxyRegion } from '../accountProxyRegion'

describe('account proxy region', () => {
  it('filters fixed candidates to the region while preserving an incompatible current selection', () => {
    const proxies = [{ id: 1, country_code: 'JP' }, { id: 2, country_code: 'PH' }, { id: 3, country_code: 'US' }, { id: 4 }]
    expect(filterProxiesByRegion(proxies, 'JP', null).map(proxy => proxy.id)).toEqual([1])
    expect(filterProxiesByRegion(proxies, 'JP', 2).map(proxy => proxy.id)).toEqual([1, 2])
    expect(filterProxiesByRegion(proxies, 'JP', 4).map(proxy => proxy.id)).toEqual([1, 4])
    expect(filterProxiesByRegion(proxies, '', 2)).toBe(proxies)
  })

  it('keeps legacy accounts unrestricted and normalizes manual ISO codes', () => {
    expect(readAccountProxyRegion()).toEqual({ mode: 'off', country: '' })
    const selection = readAccountProxyRegion({ proxy_region_mode: 'manual', proxy_region_country: ' jp ' })
    expect(resolveAccountProxyRegion(selection)).toMatchObject({ country: 'JP', source: 'manual' })
    expect(accountProxyRegionExtra(selection)).toEqual({ proxy_region_mode: 'manual', proxy_region_country: 'JP' })
    expect(accountProxyRegionValidationError({ mode: 'manual', country: '' })).toBe('admin.accounts.proxyRegion.countryRequired')
  })

  it.each([['JPY', 'JP'], ['PHP', 'PH'], ['KRW', 'KR'], ['SGD', 'SG']])('infers %s conservatively', (currency, country) => {
    expect(resolveAccountProxyRegion({ mode: 'billing', country: '' }, { billing_currency: currency })).toEqual({ country, currency, source: 'billing_currency' })
  })

  it.each(['USD', 'EUR', '', 'unknown'])('does not infer a country from %s', billing_currency => {
    expect(resolveAccountProxyRegion({ mode: 'billing', country: '' }, { billing_currency }).country).toBe('')
  })

  it.each(['USD', 'EUR', '', 'unknown'])('uses the saved fallback when %s cannot determine a country', billing_currency => {
    expect(resolveAccountProxyRegion({ mode: 'billing', country: '' }, { billing_currency }, ' jp '))
      .toEqual({ country: 'JP', currency: billing_currency.toUpperCase(), source: 'fallback_country' })
  })

  it('uses billing evidence before fallback and ignores fallback outside billing mode', () => {
    expect(resolveAccountProxyRegion({ mode: 'billing', country: '' }, { price_country: 'PH', billing_currency: 'JPY' }, 'US').country).toBe('PH')
    expect(resolveAccountProxyRegion({ mode: 'billing', country: '' }, { billing_currency: 'JPY' }, 'US').country).toBe('JP')
    expect(resolveAccountProxyRegion({ mode: 'off', country: '' }, {}, 'JP').country).toBe('')
    expect(resolveAccountProxyRegion({ mode: 'manual', country: 'PH' }, {}, 'JP').country).toBe('PH')
    expect(resolveAccountProxyRegion({ mode: 'billing', country: '' }, {}, 'Japan').source).toBe('unknown')
  })

  it('prioritizes price country and never writes billing evidence into extra', () => {
    expect(resolveAccountProxyRegion({ mode: 'billing', country: 'JP' }, { price_country: ' ph ', billing_currency: 'USD' }))
      .toEqual({ country: 'PH', currency: 'USD', source: 'price_country' })
    expect(accountProxyRegionExtra({ mode: 'billing', country: 'JP' })).toEqual({ proxy_region_mode: 'billing', proxy_region_country: '' })
    expect(accountProxyRegionExtra({ mode: 'off', country: 'JP' })).toEqual({ proxy_region_mode: 'off', proxy_region_country: '' })
  })
})
