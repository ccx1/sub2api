import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AccountProxyRegionSettings from '../AccountProxyRegionSettings.vue'
import type { Proxy } from '@/types'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, values?: Record<string, unknown>) => `${key}${values ? JSON.stringify(values) : ''}` }) }))
const proxies = [{ id: 1, country_code: 'JP' }, { id: 2, country_code: 'US' }, { id: 3 }] as Proxy[]

describe('AccountProxyRegionSettings', () => {
  it('shows currency inference and an incompatible fixed proxy without replacing it', () => {
    const wrapper = mount(AccountProxyRegionSettings, { props: { modelValue: { mode: 'billing', country: '' }, credentials: { billing_currency: 'JPY' }, proxies, proxyId: 2 } })
    expect(wrapper.text()).toContain('admin.accounts.proxyRegion.sourceCurrency')
    expect(wrapper.text()).toContain('JPY')
    expect(wrapper.text()).toContain('admin.accounts.proxyRegion.fixedMismatch')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('keeps unavailable saved countries and emits an explicit manual selection', async () => {
    const wrapper = mount(AccountProxyRegionSettings, { props: { modelValue: { mode: 'manual', country: 'NZ' }, proxies } })
    expect(wrapper.get('option[value="NZ"]').exists()).toBe(true)
    await wrapper.get('[data-testid="proxy-region-country"]').setValue('PH')
    expect(wrapper.emitted('update:modelValue')).toEqual([[{ mode: 'manual', country: 'PH' }]])
  })

  it('allows pending billing evidence and makes explicit random direct fallback visible', () => {
    const wrapper = mount(AccountProxyRegionSettings, { props: { modelValue: { mode: 'billing', country: '' }, proxies, billingPending: true, randomEnabled: true, emptyPoolPolicy: 'direct' } })
    expect(wrapper.text()).toContain('admin.accounts.proxyRegion.billingPending')
    expect(wrapper.text()).toContain('admin.accounts.proxyRegion.directFallback')
    expect(wrapper.text()).not.toContain('admin.accounts.proxyRegion.directBlocked')
  })

  it('reports ambiguous currencies and blocks ordinary direct egress', () => {
    const wrapper = mount(AccountProxyRegionSettings, { props: { modelValue: { mode: 'billing', country: '' }, credentials: { billing_currency: 'USD' }, proxies, proxyId: null } })
    expect(wrapper.text()).toContain('admin.accounts.proxyRegion.unknownBilling')
    expect(wrapper.text()).toContain('admin.accounts.proxyRegion.directBlocked')
  })

  it('does not mutate disabled fields', async () => {
    const wrapper = mount(AccountProxyRegionSettings, { props: { modelValue: { mode: 'off', country: '' }, proxies, disabled: true } })
    await wrapper.get('[data-testid="proxy-region-mode"]').setValue('manual')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('shows the saved fallback, preserves unavailable countries, and emits edits or clearing', async () => {
    const wrapper = mount(AccountProxyRegionSettings, { props: { modelValue: { mode: 'billing', country: '' }, credentials: { billing_currency: 'USD' }, fallbackCountry: 'NZ', proxies } })
    const select = wrapper.get<HTMLSelectElement>('[data-testid="proxy-region-fallback-country"]')
    expect(select.element.value).toBe('NZ')
    expect(wrapper.text()).toContain('admin.accounts.proxyRegion.sourceFallback')
    expect(wrapper.text()).not.toContain('admin.accounts.proxyRegion.unknownBilling')
    await select.setValue('JP')
    await select.setValue('')
    expect(wrapper.emitted('update:fallbackCountry')).toEqual([['JP'], ['']])
  })

  it('disables fallback editing and hides it for other modes or callers without support', async () => {
    const wrapper = mount(AccountProxyRegionSettings, { props: { modelValue: { mode: 'billing', country: '' }, fallbackCountry: 'JP', proxies, disabled: true } })
    await wrapper.get('[data-testid="proxy-region-fallback-country"]').setValue('PH')
    expect(wrapper.emitted('update:fallbackCountry')).toBeUndefined()
    await wrapper.setProps({ modelValue: { mode: 'manual', country: 'PH' } })
    expect(wrapper.find('[data-testid="proxy-region-fallback-country"]').exists()).toBe(false)
    await wrapper.setProps({ modelValue: { mode: 'billing', country: '' }, fallbackCountry: undefined })
    expect(wrapper.find('[data-testid="proxy-region-fallback-country"]').exists()).toBe(false)
  })
})
