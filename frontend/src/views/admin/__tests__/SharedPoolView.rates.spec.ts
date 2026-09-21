import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import SharedPoolView from '../SharedPoolView.vue'

const { accounts, settings, saveSettings, userRates, saveUserRate } = vi.hoisted(() => ({
  accounts: vi.fn(), settings: vi.fn(), saveSettings: vi.fn(), userRates: vi.fn(), saveUserRate: vi.fn()
}))
vi.mock('@/api/sharedPool', () => ({ adminSharedPoolAPI: { accounts, settings, saveSettings, userRates, saveUserRate } }))
vi.mock('@/api/admin/groups', () => ({ default: {}, getAllIncludingInactive: vi.fn().mockResolvedValue([]) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))

const original = { platform_rate_bps: 2000, proxy_rate_bps: 100, max_concurrency: 10, default_group_ids: {}, settlement_multiplier: 1 }
function accountPage(platform: number, proxy: number) {
  return { items: [{ id: 7, name: 'Shared account', platform_rate_bps: platform, proxy_rate_bps: proxy }], total: 1 }
}
async function render() {
  const wrapper = mount(SharedPoolView, { global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' }, RouterLink: true, Pagination: true,
    SharedPoolAdminAccountsTable: { props: ['accounts', 'loading', 'searching'], emits: ['allocate'], template: '<div><div v-for="account in accounts" :key="account.id" data-test="rates">{{ account.platform_rate_bps }}/{{ account.proxy_rate_bps }}<button data-test="allocate" @click="$emit(\'allocate\', account)">Allocate</button></div></div>' },
    SharedPoolAllocationDialog: { props: ['account', 'groups'], emits: ['saved', 'close'], template: '<button data-test="save-allocation" @click="$emit(\'saved\')">Save allocation</button>' }, SharedEarningsTable: true
  } } })
  await flushPromises()
  return wrapper
}

describe('shared pool administrator rate refresh', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    settings.mockResolvedValue(original)
    accounts.mockResolvedValue(accountPage(2000, 100))
    saveSettings.mockImplementation(value => Promise.resolve(value))
    userRates.mockResolvedValue([])
    saveUserRate.mockResolvedValue({})
  })

  it('refreshes account shares immediately after global rates are saved', async () => {
    const wrapper = await render()
    await wrapper.get('[role="tab"]:nth-child(2)').trigger('click')
    await wrapper.get('#pool-platform-rate').setValue(25)
    await wrapper.get('#pool-proxy-rate').setValue(2)
    accounts.mockResolvedValue(accountPage(2500, 200))
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(saveSettings).toHaveBeenCalledWith({ ...original, platform_rate_bps: 2500, proxy_rate_bps: 200, subscription_group_ids: {} })
    await wrapper.get('[role="tab"]:nth-child(1)').trigger('click')
    expect(wrapper.get('[data-test="rates"]').text()).toContain('2500/200')
    expect(accounts).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('refreshes account shares after saving a zero override with inherited proxy rates', async () => {
    const wrapper = await render()
    await wrapper.get('[role="tab"]:nth-child(3)').trigger('click')
    await flushPromises()
    await wrapper.get('#rate-user-id').setValue(9)
    await wrapper.get('#user-platform-rate').setValue(0)
    accounts.mockResolvedValue(accountPage(0, 100))
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(saveUserRate).toHaveBeenCalledWith(9, { platform_rate_bps: 0, proxy_rate_bps: null, settlement_multiplier: null })
    await wrapper.get('[role="tab"]:nth-child(1)').trigger('click')
    expect(wrapper.get('[data-test="rates"]').text()).toContain('0/100')
    expect(accounts).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('does not refresh accounts when saving fails', async () => {
    const wrapper = await render()
    await wrapper.get('[role="tab"]:nth-child(2)').trigger('click')
    saveSettings.mockRejectedValueOnce(new Error('Save failed'))
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('Save failed')
    expect(accounts).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('applies the submitted search and preserves it when refreshing', async () => {
    const wrapper = await render()
    await wrapper.get('input[aria-label="common.search"]').setValue('  owner@example.com  ')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(accounts).toHaveBeenLastCalledWith(1, 'owner@example.com')
    await wrapper.get('input[aria-label="common.search"]').setValue('not submitted')
    await wrapper.get('header > button').trigger('click')
    await flushPromises()
    expect(accounts).toHaveBeenLastCalledWith(1, 'owner@example.com')
    wrapper.unmount()
  })

  it('opens allocation from the admin table and refreshes after saving', async () => {
    const wrapper = await render()
    await wrapper.get('[data-test="allocate"]').trigger('click')
    expect(wrapper.find('[data-test="save-allocation"]').exists()).toBe(true)
    await wrapper.get('[data-test="save-allocation"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="save-allocation"]').exists()).toBe(false)
    expect(accounts).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })
})
