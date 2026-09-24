import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import RandomProxySettings from '../RandomProxySettings.vue'
import type { Proxy } from '@/types'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const listGroups = vi.hoisted(() => vi.fn())
vi.mock('@/api/admin', () => ({ adminAPI: { proxies: { listGroups } } }))
beforeEach(() => { listGroups.mockReset().mockResolvedValue([{ id: 7, name: 'Tokyo pool', proxy_count: 2, active_proxy_count: 1 }]) })

const proxies = [
  { id: 1, name: 'Tokyo', account_count: 7, protocol: 'http', host: 'tokyo.example.com', port: 8080, country: 'Japan', country_code: 'JP', region: 'Tokyo' },
  { id: 2, name: 'Berlin', protocol: 'socks5', host: '2001:db8::1', port: 1080, country: 'Germany', country_code: 'DE' }
] as Proxy[]

function mountSettings(ids: number[] = [], regionCountry?: string) {
  return mount(RandomProxySettings, { props: { proxies, enabled: true, scope: 'selected', ids, policy: 'reject', regionCountry } })
}

describe('RandomProxySettings', () => {
  it('loads groups and reports a required selection without defaulting to all', async () => {
    const wrapper = mountSettings([1])
    await wrapper.setProps({ scope: 'group' })
    await flushPromises()
    expect(listGroups).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('accountProxyGroups.required')
    await wrapper.get('[data-testid="random-proxy-group"]').setValue('7')
    expect(wrapper.emitted('update:groupId')?.at(-1)).toEqual([7])
    expect(wrapper.props('ids')).toEqual([1])
    expect(wrapper.emitted('update:scope')).toBeUndefined()
  })

  it('preserves an unavailable group and blocks saving instead of selecting all', async () => {
    const wrapper = mountSettings()
    await wrapper.setProps({ scope: 'group', groupId: 99 })
    await flushPromises()
    expect(wrapper.get('[data-testid="random-proxy-group"]').element).toHaveProperty('value', '99')
    expect(wrapper.text()).toContain('accountProxyGroups.unavailable')
    expect(wrapper.emitted('update:groupError')?.at(-1)).toEqual(['accountProxyGroups.unavailable'])
    expect(wrapper.emitted('update:groupId')).toBeUndefined()
  })

  it('warns about an empty group while retaining the chosen empty-pool policy', async () => {
    listGroups.mockResolvedValue([{ id: 7, name: 'Empty', proxy_count: 0, active_proxy_count: 0 }])
    const wrapper = mountSettings()
    await wrapper.setProps({ scope: 'group', groupId: 7, policy: 'disable' })
    await flushPromises()
    expect(wrapper.text()).toContain('accountProxyGroups.emptyPool')
    expect(wrapper.emitted('update:groupError')?.at(-1)).toEqual([null])
    expect(wrapper.props('policy')).toBe('disable')
    expect(wrapper.emitted('update:scope')).toBeUndefined()
  })

  it('shows loading and failure states and retries the group directory', async () => {
    listGroups.mockRejectedValueOnce(new Error('offline'))
    const wrapper = mountSettings()
    await wrapper.setProps({ scope: 'group', groupId: 7 })
    await flushPromises()
    expect(wrapper.text()).toContain('accountProxyGroups.loadFailed')
    expect(wrapper.emitted('update:groupError')?.at(-1)).toEqual(['accountProxyGroups.loadFailed'])
    await wrapper.get('[data-testid="random-proxy-group-settings"] button').trigger('click')
    await flushPromises()
    expect(listGroups).toHaveBeenCalledTimes(2)
    expect(wrapper.emitted('update:groupError')?.at(-1)).toEqual([null])
    expect(wrapper.text()).not.toContain('accountProxyGroups.loadFailed')
  })

  it('shows an empty directory without changing group scope', async () => {
    listGroups.mockResolvedValue([])
    const wrapper = mountSettings()
    await wrapper.setProps({ scope: 'group' })
    await flushPromises()
    expect(wrapper.text()).toContain('accountProxyGroups.empty')
    expect(wrapper.emitted('update:scope')).toBeUndefined()
  })

  it('explains the shared balancing limit while retaining all empty-pool policies', () => {
    const wrapper = mountSettings([1])
    expect(wrapper.text()).toContain('admin.accounts.randomProxyHint')
    expect(wrapper.text()).toContain('admin.accounts.randomProxyBalanceHint')
    expect(wrapper.findAll('select').at(-1)?.findAll('option').map(option => option.attributes('value')))
      .toEqual(['reject', 'disable', 'direct'])
  })

  it('offers region-first selection with pool fallback', async () => {
    const wrapper = mountSettings([1], 'JP')
    expect(wrapper.get('[data-testid="random-proxy-region-fallback"]').element).toHaveProperty('value', 'pool')
    await wrapper.get('[data-testid="random-proxy-region-fallback"]').setValue('none')
    expect(wrapper.emitted('update:regionFallback')?.at(-1)).toEqual(['none'])
    await wrapper.get('[data-testid="random-proxy-region-fallback"]').setValue('pool')
    expect(wrapper.emitted('update:regionFallback')?.at(-1)).toEqual(['pool'])
  })

  it('hides scheduled rotation while preserving the legacy stored value', async () => {
    const wrapper = mountSettings([1])
    await wrapper.setProps({ maxReuseMinutes: 120 })
    expect(wrapper.find('[data-testid="random-proxy-reuse-minutes"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('admin.accounts.randomProxyMaxReuseHint')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    await wrapper.get('[data-testid="random-proxy-scope"]').setValue('all')
    expect(wrapper.props('maxReuseMinutes')).toBe(120)
    expect(wrapper.emitted('update:maxReuseMinutes')).toBeUndefined()
  })

  it('searches region and displays proxy addresses without credentials', async () => {
    const wrapper = mountSettings()
    expect(wrapper.text()).toContain('(7) Tokyo http://tokyo.example.com:8080')
    expect(wrapper.text()).toContain('socks5://[2001:db8::1]:1080')
    await wrapper.get('input[type="search"]').setValue('Japan')
    expect(wrapper.text()).toContain('Tokyo')
    expect(wrapper.text()).not.toContain('Berlin')
  })

  it('filters by OAuth subscription country while preserving selected proxies', () => {
    const wrapper = mountSettings([2], 'JP')
    expect(wrapper.text()).toContain('Tokyo')
    expect(wrapper.text()).toContain('Berlin')
  })

  it.each(['', 'unknown', 'off', 'all'])('shows the full pool when the subscription country is %s', regionCountry => {
    const wrapper = mountSettings([], regionCountry)
    expect(wrapper.text()).toContain('Tokyo')
    expect(wrapper.text()).toContain('Berlin')
  })

  it('limits random candidates to the account region and keeps an existing cross-region selection visible', async () => {
    const wrapper = mountSettings()
    await wrapper.setProps({ regionCountry: 'JP' })
    expect(wrapper.text()).toContain('Tokyo')
    expect(wrapper.text()).not.toContain('Berlin')

    await wrapper.setProps({ ids: [2] })
    expect(wrapper.text()).toContain('Berlin')
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
