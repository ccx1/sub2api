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
})
