import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import CodexTicketProxySettings from '../CodexTicketProxySettings.vue'
import ProxySelector from '@/components/common/ProxySelector.vue'
import type { Proxy } from '@/types'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin', () => ({ adminAPI: { proxies: { testProxy: vi.fn() } } }))

const proxies = [
  { id: 7, name: 'Tokyo', protocol: 'http', host: 'tokyo.example', port: 8080, status: 'active', account_count: 3 },
  { id: 8, name: 'Inactive', protocol: 'http', host: 'expired.example', port: 8080, status: 'inactive' },
  { id: 9, name: 'Expired', protocol: 'http', host: 'expired.example', port: 8080, status: 'active', expires_at: '2000-01-01T00:00:00Z' }
] as Proxy[]

describe('CodexTicketProxySettings', () => {
  it('allows explicit account/global/random/fixed choices without changing the stored selection implicitly', async () => {
    const wrapper = mount(CodexTicketProxySettings, { props: { proxies, modelValue: { mode: 'inherit', proxyId: null } } })
    expect(wrapper.get<HTMLInputElement>('[value="inherit"]').element.checked).toBe(true)
    expect(wrapper.findAll('input[type="radio"]').map(input => input.attributes('value'))).toEqual(['account', 'inherit', 'random', 'fixed'])
    await wrapper.get('[value="account"]').setValue(true)
    await wrapper.get('[value="random"]').setValue(true)
    await wrapper.get('[value="fixed"]').setValue(true)
    expect(wrapper.emitted('update:modelValue')).toEqual([[{ mode: 'account', proxyId: null }], [{ mode: 'random', proxyId: null }], [{ mode: 'fixed', proxyId: null }]])
    wrapper.unmount()
  })

  it('follows account settings without another proxy selector or independent availability error', () => {
    const wrapper = mount(CodexTicketProxySettings, { props: { proxies: [], modelValue: { mode: 'account', proxyId: 99 } } })
    expect(wrapper.findComponent(ProxySelector).exists()).toBe(false)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('admin.accounts.codexTicketProxy.accountHint')
    expect(wrapper.get<HTMLInputElement>('[value="account"]').element.checked).toBe(true)
    wrapper.unmount()
  })

  it('selects only available managed proxies and provides no direct option', async () => {
    const wrapper = mount(CodexTicketProxySettings, { props: { proxies, modelValue: { mode: 'fixed', proxyId: null } } })
    expect(wrapper.getComponent(ProxySelector).props('allowDirect')).toBe(false)
    await wrapper.get('.select-trigger').trigger('click')
    expect(wrapper.text()).not.toContain('admin.accounts.noProxy')
    expect(wrapper.text()).not.toContain('Inactive')
    expect(wrapper.text()).not.toContain('Expired')
    await wrapper.get('.select-option').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[{ mode: 'fixed', proxyId: 7 }]])
    wrapper.unmount()
  })

  it('retains a missing fixed proxy instead of silently falling back', () => {
    const wrapper = mount(CodexTicketProxySettings, { props: { proxies, modelValue: { mode: 'fixed', proxyId: 99 } } })
    expect(wrapper.get('[role="alert"]').text()).toContain('99')
    expect(wrapper.get('[role="alert"]').text()).toContain('unavailable')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    wrapper.unmount()
  })

  it('blocks changes while saving, including stale selector events', async () => {
    const wrapper = mount(CodexTicketProxySettings, { props: { proxies, modelValue: { mode: 'fixed', proxyId: 7 }, disabled: true } })
    expect(wrapper.getComponent(ProxySelector).props('disabled')).toBe(true)
    await wrapper.get('[value="random"]').trigger('change')
    wrapper.getComponent(ProxySelector).vm.$emit('update:modelValue', 8)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    wrapper.unmount()
  })
})
