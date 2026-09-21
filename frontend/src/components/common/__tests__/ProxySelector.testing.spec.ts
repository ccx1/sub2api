import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import ProxySelector from '../ProxySelector.vue'
import type { Proxy } from '@/types'

const testProxy = vi.hoisted(() => vi.fn())
vi.mock('@/api/admin', () => ({ adminAPI: { proxies: { testProxy } } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => { vi.clearAllMocks() })

async function openSelector() {
  const wrapper = mount(ProxySelector, {
    props: { modelValue: null, proxies: [1, 2].map(id => ({
      id, name: `Proxy ${id}`, host: 'localhost', port: 8080, protocol: 'http'
    } as Proxy)) },
    global: { stubs: { Icon: true } }
  })
  await wrapper.get('.select-trigger').trigger('click')
  return wrapper
}

describe('proxy connection tests', () => {
  it('displays and searches the proxy group for fixed selection', async () => {
    const wrapper = await openSelector()
    await wrapper.setProps({ proxies: [
      { ...wrapper.props('proxies')[0], group_name: 'Tokyo pool' },
      wrapper.props('proxies')[1]
    ] })
    await wrapper.get('.select-search-input').setValue('Tokyo pool')
    expect(wrapper.findAll('.select-option')).toHaveLength(2)
    expect(wrapper.text()).toContain('Proxy 1 [Tokyo pool]')
    expect(wrapper.text()).not.toContain('Proxy 2')
    await wrapper.setProps({ modelValue: 1 })
    expect(wrapper.get('.select-value').text()).toContain('[Tokyo pool]')
  })

  it('shows account count, name and credential-free IPv6 address in both options and selection', async () => {
    const wrapper = await openSelector()
    await wrapper.setProps({ proxies: [{ id: 9, name: 'IPv6', account_count: 4, protocol: 'socks5', host: '2001:db8::1', port: 1080, username: 'private-user', password: 'private-pass' } as Proxy] })
    expect(wrapper.text()).toContain('(4) IPv6 socks5://[2001:db8::1]:1080')
    await wrapper.findAll('.select-option')[1].trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([9])
    await wrapper.setProps({ modelValue: 9 })
    expect(wrapper.get('.select-value').text()).toBe('(4) IPv6 socks5://[2001:db8::1]:1080')
    expect(wrapper.text()).not.toContain('private-')
    await wrapper.setProps({ proxies: [{ ...wrapper.props('proxies')[0], account_count: 0 }] })
    expect(wrapper.get('.select-value').text()).toMatch(/^\(0\)/)
  })

  it('hides direct mode for fixed ticket selection and preserves an unavailable selected ID', async () => {
    const wrapper = await openSelector()
    await wrapper.setProps({ allowDirect: false })
    expect(wrapper.findAll('.select-option')).toHaveLength(2)
    expect(wrapper.text()).not.toContain('admin.accounts.noProxy')
    expect(wrapper.get('.select-value').text()).toBe('common.selectOption')
    await wrapper.setProps({ modelValue: 99 })
    expect(wrapper.get('.select-value').text()).toBe('admin.accounts.randomProxyPoolUnavailableItem')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('closes the dropdown and prevents stale selection or proxy tests after disabling', async () => {
    const wrapper = await openSelector()
    const staleOption = wrapper.findAll('.select-option')[1]
    const staleTest = wrapper.findAll('.test-btn')[0]
    await wrapper.setProps({ disabled: true })
    await staleOption.trigger('click')
    await staleTest.trigger('click')
    await vi.waitFor(() => expect(wrapper.find('.select-dropdown').exists()).toBe(false))
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(testProxy).not.toHaveBeenCalled()
  })

  it('does not restart an individual test when a batch is started', async () => {
    let finish!: (result: object) => void
    testProxy.mockImplementation((id: number) => id === 1
      ? new Promise(resolve => { finish = resolve })
      : Promise.resolve({ success: true, country: 'GB' }))
    const wrapper = await openSelector()
    await wrapper.findAll('.test-btn')[0].trigger('click')
    await wrapper.get('.batch-test-btn').trigger('click')
    await flushPromises()
    expect(testProxy.mock.calls.map(([id]) => id)).toEqual([1, 2])
    expect(wrapper.findAll('.test-btn')[0].attributes('disabled')).toBeDefined()
    finish({ success: true, country: 'US' })
    await flushPromises()
    expect(wrapper.text()).toContain('US')
    expect(wrapper.findAll('.test-btn')[0].attributes('disabled')).toBeUndefined()
  })

  it('shows per-proxy outcomes and allows another batch after a failure', async () => {
    testProxy.mockImplementation((id: number) => id === 1
      ? Promise.reject(new Error('offline'))
      : Promise.resolve({ success: true, country: 'GB' }))
    const wrapper = await openSelector()
    await wrapper.get('.batch-test-btn').trigger('click')
    await flushPromises()
    expect(testProxy).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('admin.proxies.testFailed')
    expect(wrapper.text()).toContain('GB')
    expect(wrapper.get('.batch-test-btn').attributes('disabled')).toBeUndefined()
    await wrapper.get('.batch-test-btn').trigger('click')
    await flushPromises()
    expect(testProxy).toHaveBeenCalledTimes(4)
  })
})
