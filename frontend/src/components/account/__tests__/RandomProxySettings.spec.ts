import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import RandomProxySettings from '../RandomProxySettings.vue'
import type { Proxy } from '@/types'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const proxies = [
  { id: 1, name: 'Tokyo', protocol: 'http', host: 'tokyo.example.com', port: 8080, country: 'Japan', region: 'Tokyo' },
  { id: 2, name: 'Berlin', protocol: 'socks5', host: '2001:db8::1', port: 1080, country: 'Germany' }
] as Proxy[]

function mountSettings(ids: number[] = []) {
  return mount(RandomProxySettings, { props: { proxies, enabled: true, scope: 'selected', ids, policy: 'reject' } })
}

describe('RandomProxySettings', () => {
  it('explains the shared balancing limit while retaining all empty-pool policies', () => {
    const wrapper = mountSettings([1])
    expect(wrapper.text()).toContain('admin.accounts.randomProxyHint')
    expect(wrapper.text()).toContain('admin.accounts.randomProxyBalanceHint')
    expect(wrapper.findAll('select').at(-1)?.findAll('option').map(option => option.attributes('value')))
      .toEqual(['reject', 'disable', 'direct'])
  })

  it('searches region and displays proxy addresses without credentials', async () => {
    const wrapper = mountSettings()
    expect(wrapper.text()).toContain('socks5://[2001:db8::1]:1080')
    await wrapper.get('input[type="search"]').setValue('Japan')
    expect(wrapper.text()).toContain('Tokyo')
    expect(wrapper.text()).not.toContain('Berlin')
  })

  it('preserves unavailable selections when selecting another proxy', async () => {
    const wrapper = mountSettings([99])
    expect(wrapper.text()).toContain('admin.accounts.randomProxyPoolUnavailable')
    await wrapper.get('input[value="1"]').setValue(true)
    expect(wrapper.emitted('update:ids')?.[0]).toEqual([[99, 1]])
  })

  it('allows removing an unavailable selection explicitly', async () => {
    const wrapper = mountSettings([99])
    await wrapper.get('input[value="99"]').setValue(false)
    expect(wrapper.emitted('update:ids')?.[0]).toEqual([[]])
  })

  it('keeps an empty selected pool explicit and shows validation', async () => {
    const wrapper = mountSettings()
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.accounts.randomProxyPoolRequired')
    await wrapper.setProps({ proxies: [] })
    expect(wrapper.get('[data-testid="random-proxy-scope"]').element).toHaveProperty('value', 'selected')
    expect(wrapper.emitted('update:scope')).toBeUndefined()
  })

  it('disables all settings when bulk proxy editing is not enabled', async () => {
    const wrapper = mountSettings([1])
    await wrapper.setProps({ disabled: true })
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
  })
})
