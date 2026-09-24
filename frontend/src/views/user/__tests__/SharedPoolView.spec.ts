import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import SharedPoolView from '../SharedPoolView.vue'
import SharedAccountDialog from '@/components/sharedPool/SharedAccountDialog.vue'
import SharedAccountImportDialog from '@/components/sharedPool/SharedAccountImportDialog.vue'
import SharedAccountCard from '@/components/sharedPool/SharedAccountCard.vue'
import SharedPoolCatalog from '@/components/sharedPool/SharedPoolCatalog.vue'
import SharedDispatchConsentDialog from '@/components/sharedPool/SharedDispatchConsentDialog.vue'
import SharedAutoTransferSettings from '@/components/sharedPool/SharedAutoTransferSettings.vue'

const { config, overview, pools, summary, transfer, refreshUser, showError, showSuccess, showWarning, accounts, importAccounts, codexTicket, enable, autoTransferSettings } = vi.hoisted(() => ({
  config: vi.fn(), overview: vi.fn(), pools: vi.fn(), summary: vi.fn(), transfer: vi.fn(), refreshUser: vi.fn(), showError: vi.fn(), showSuccess: vi.fn(), showWarning: vi.fn(), accounts: vi.fn(), importAccounts: vi.fn(), codexTicket: vi.fn(), enable: vi.fn(),
  autoTransferSettings: vi.fn(async () => ({ enabled: false, threshold: 1, daily_time: '00:00', timezone: 'Asia/Shanghai' }))
}))
vi.mock('@/api/sharedPool', () => ({ sharedPoolAPI: {
  overview, pools, config,
  accounts, importAccounts, summary, transfer, codexTicket, enable, autoTransferSettings
} }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ refreshUser }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess, showWarning }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))

async function render(withImportDialog = false, initialTab = 'myAccounts') {
  const wrapper = mount(SharedPoolView, { global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' }, SharedPoolCatalog: true, SharedAccountDialog: true, SharedAccountImportDialog: !withImportDialog,
    BaseDialog: { props: ['show'], template: '<div v-if="show" role="dialog"><slot /><slot name="footer" /></div>' },
    SharedAccountCard: true, SharedAccountTestDialog: true, SharedEarningsTable: true, Pagination: true,
    ConfirmDialog: { props: ['show', 'title'], template: '<button v-if="show" data-test="confirm" @click="$emit(\'confirm\')">{{ title }}</button>' }
  } } })
  await flushPromises()
  if (initialTab !== 'pools') await wrapper.findAll('button').find(button => button.text() === `sharedPool.${initialTab}`)!.trigger('click')
  return wrapper
}

describe('shared earnings transfer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    config.mockResolvedValue({ platforms: ['openai'], max_concurrency: 5, platform_rate_bps: 2000, proxy_rate_bps: 100, settlement_multiplier: 0.5 })
    overview.mockResolvedValue({ total_accounts: 0, schedulable_accounts: 0, tiers: [] })
    accounts.mockResolvedValue({ items: [], total: 0 })
    summary.mockResolvedValue({ total_earned: 12, available: 10, pending: 2, transferred: 0, platform_amount: 3, billing_amount: 15 })
    refreshUser.mockResolvedValue({ balance: 20 })
  })

  it('shows revenue rates before the user has added any accounts', async () => {
    const wrapper = await render()
    expect(wrapper.get('[data-test="conditional-platform-share"]').text()).toBe('20% + (1%)')
    expect(wrapper.get('[data-test="owner-share"]').text()).toBe('79%')
    expect(wrapper.get('[data-test="custom-owner-share"]').text()).toBe('80%')
    wrapper.unmount()
  })

  it('keeps manual transfers and account actions available when automatic settings fail to load', async () => {
    autoTransferSettings.mockRejectedValueOnce(new Error('Settings offline'))
    const wrapper = await render()
    await flushPromises()
    expect(wrapper.getComponent(SharedAutoTransferSettings).text()).toContain('sharedPool.autoTransferStatusUnavailable')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.findAll('button').find(button => button.text() === 'sharedPool.transfer')!.attributes('disabled')).toBeUndefined()
    expect(wrapper.findAll('button').find(button => button.text() === 'sharedPool.create')!.attributes('disabled')).toBeUndefined()
    expect(wrapper.text()).toContain('$10.0000')
    expect(showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('keeps automatic transfer settings compact beside manual transfer until opened', async () => {
    const wrapper = await render()
    await flushPromises()
    const settingsButton = wrapper.get('[data-test="auto-transfer-settings"]')
    const manualButton = wrapper.findAll('button').find(button => button.text() === 'sharedPool.transfer')!
    expect(manualButton.element.parentElement?.contains(settingsButton.element)).toBe(true)
    expect(wrapper.find('#shared-auto-transfer-form').exists()).toBe(false)
    await settingsButton.trigger('click')
    expect(wrapper.get('[role="dialog"]').find('#shared-auto-transfer-form').exists()).toBe(true)
    expect(manualButton.attributes('disabled')).toBeUndefined()
    expect(transfer).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('requires the dedicated authorization confirmation for legacy accounts', async () => {
    const account = { id: 7, name: 'Legacy account', platform: 'openai', dispatch_consent: false, enabled: false }
    accounts.mockResolvedValue({ items: [account], total: 1 }); enable.mockResolvedValue({})
    const wrapper = await render()
    const card = wrapper.getComponent(SharedAccountCard)
    expect(card.vm.$attrs.onEnable).toBeUndefined()
    card.vm.$emit('authorize'); await flushPromises()
    expect(enable).not.toHaveBeenCalled()
    expect(wrapper.getComponent(SharedDispatchConsentDialog).text()).toContain('sharedPool.dispatchConsentHint')
    wrapper.getComponent(SharedDispatchConsentDialog).vm.$emit('close'); await flushPromises()
    expect(enable).not.toHaveBeenCalled()
    card.vm.$emit('authorize'); await flushPromises()
    wrapper.getComponent(SharedDispatchConsentDialog).vm.$emit('confirm'); await flushPromises()
    expect(enable).toHaveBeenLastCalledWith(7, true, true)
    expect(wrapper.findComponent(SharedDispatchConsentDialog).exists()).toBe(false)
    wrapper.unmount()
  })

  it('locks duplicate ticket updates and refreshes the actual saved state', async () => {
    const account = { id: 7, codex_ticket_enabled: true }
    accounts.mockResolvedValue({ items: [account], total: 1 })
    let resolve!: () => void
    codexTicket.mockImplementationOnce(() => new Promise<void>(done => { resolve = done }))
    const wrapper = await render()
    const card = wrapper.getComponent(SharedAccountCard)
    card.vm.$emit('codexTicket', false)
    card.vm.$emit('codexTicket', false)
    await flushPromises()
    expect(codexTicket).toHaveBeenCalledTimes(1)
    expect(codexTicket).toHaveBeenCalledWith(7, false)
    expect(card.props('busy')).toBe(true)
    expect(card.props('account').codex_ticket_enabled).toBe(true)
    accounts.mockResolvedValue({ items: [{ ...account, codex_ticket_enabled: false }], total: 1 })
    resolve(); await flushPromises()
    expect(card.props('busy')).toBe(false)
    expect(card.props('account').codex_ticket_enabled).toBe(false)
    wrapper.unmount()
  })

  it('keeps the previous ticket state and unlocks after a failed update', async () => {
    accounts.mockResolvedValue({ items: [{ id: 7, codex_ticket_enabled: true }], total: 1 })
    codexTicket.mockRejectedValueOnce(new Error('Update failed'))
    const wrapper = await render()
    const card = wrapper.getComponent(SharedAccountCard)
    card.vm.$emit('codexTicket', false)
    await flushPromises()
    expect(card.props('account').codex_ticket_enabled).toBe(true)
    expect(card.props('busy')).toBe(false)
    expect(showError).toHaveBeenCalledWith('Update failed')
    wrapper.unmount()
  })

  it('refreshes a recovered account after quota reset without unmounting the quota controls', async () => {
    accounts.mockResolvedValue({ items: [{ id: 7, status: 'error' }], total: 1 })
    const wrapper = await render()
    const card = wrapper.getComponent(SharedAccountCard)
    accounts.mockResolvedValue({ items: [{ id: 7, status: 'active' }], total: 1 })
    card.vm.$emit('usageUpdated')
    await flushPromises()
    expect(accounts).toHaveBeenCalledTimes(2)
    expect(card.props('account').status).toBe('active')
    expect(wrapper.getComponent(SharedAccountCard).vm).toBe(card.vm)
    expect(showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('opens import from creation while preserving the selected shared-account settings', async () => {
    const wrapper = await render()
    await wrapper.findAll('button').find(button => button.text() === 'sharedPool.create')!.trigger('click')
    const defaults = { name: 'My imported account', platform: 'openai', type: 'oauth', concurrency: 3, proxy_url: '', enabled: true, protection_enabled: true }
    wrapper.findComponent(SharedAccountDialog).vm.$emit('import', defaults)
    await flushPromises()
    expect(wrapper.findComponent(SharedAccountDialog).exists()).toBe(false)
    expect(wrapper.findComponent(SharedAccountImportDialog).props('initialDefaults')).toEqual(defaults)
    wrapper.unmount()
  })

  it('disables transfer when only pending earnings exist', async () => {
    summary.mockResolvedValue({ total_earned: 2, available: 0, pending: 2, transferred: 0 })
    const wrapper = await render()
    expect(wrapper.findAll('button').find(button => button.text() === 'sharedPool.transfer')!.attributes('disabled')).toBeDefined()
    expect(transfer).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('closes a successful import dialog and refreshes the account list', async () => {
    importAccounts.mockResolvedValue({ total: 1, created: 1, failed: 0, items: [{ index: 1, account_id: 10 }], warnings: [] })
    const wrapper = await render(true)
    await wrapper.findAll('button').find(button => button.text() === 'sharedPool.importAccounts')!.trigger('click')
    const dialog = wrapper.getComponent(SharedAccountImportDialog)
    await dialog.get('textarea').setValue('{}')
    await dialog.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.findComponent(SharedAccountImportDialog).exists()).toBe(false)
    expect(accounts).toHaveBeenCalledTimes(2)
    expect(summary).toHaveBeenCalledTimes(2)
    expect(showSuccess).toHaveBeenCalledWith('sharedPool.importResult')
    wrapper.unmount()
  })

  it('requires confirmation, locks duplicate requests, and refreshes account balance', async () => {
    let resolve!: (value: unknown) => void
    transfer.mockImplementation(() => new Promise(done => { resolve = done }))
    const wrapper = await render()
    const button = wrapper.findAll('button').find(button => button.text() === 'sharedPool.transfer')!
    await button.trigger('click')
    expect(transfer).not.toHaveBeenCalled()
    await wrapper.get('[data-test="confirm"]').trigger('click')
    expect(transfer).toHaveBeenCalledTimes(1)
    expect(button.attributes('disabled')).toBeDefined()
    summary.mockResolvedValue({ total_earned: 12, available: 0, pending: 2, transferred: 10 })
    resolve({ id: 1, amount: 10, balance: 20 })
    await flushPromises()
    expect(refreshUser).toHaveBeenCalledTimes(1)
    expect(showSuccess).toHaveBeenCalledWith('sharedPool.transferredSuccess')
    expect(button.attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('keeps a successful transfer when refreshing the balance fails', async () => {
    transfer.mockResolvedValue({ id: 1, amount: 10, balance: 20 })
    const wrapper = await render()
    refreshUser.mockRejectedValue(new Error('offline'))
    summary.mockRejectedValue(new Error('offline'))
    const button = wrapper.findAll('button').find(button => button.text() === 'sharedPool.transfer')!
    await button.trigger('click')
    await wrapper.get('[data-test="confirm"]').trigger('click')
    await flushPromises()
    expect(showSuccess).toHaveBeenCalledWith('sharedPool.transferredSuccess')
    expect(showError).not.toHaveBeenCalled()
    expect(showWarning).toHaveBeenCalledWith('sharedPool.refreshFailed')
    expect(button.attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
})

describe('shared pool live concurrency', () => {
  let wrapper: Awaited<ReturnType<typeof render>> | undefined
  const pool = { total_accounts: 10, schedulable_accounts: 7, current_concurrency: 3, concurrency_capacity: 8, concurrency_unlimited: false, settlement_multiplier: 0.5, platform_rate_bps: 500, proxy_rate_bps: 100, updated_at: '2026-09-20T12:00:00Z', tiers: [] }

  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    config.mockResolvedValue({ platforms: ['openai'], max_concurrency: 5, platform_rate_bps: 2000, proxy_rate_bps: 100, settlement_multiplier: 0.5 })
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    overview.mockReset().mockResolvedValue(pool)
    accounts.mockResolvedValue({ items: [], total: 0 })
    summary.mockResolvedValue({ total_earned: 0, available: 0, pending: 0, transferred: 0 })
  })
  afterEach(() => {
    wrapper?.unmount()
    wrapper = undefined
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('updates only the pool catalog every ten seconds without remounting it', async () => {
    wrapper = await render(false, 'pools')
    const catalog = wrapper.getComponent(SharedPoolCatalog)
    overview.mockResolvedValue({ ...pool, current_concurrency: 5 })
    await vi.advanceTimersByTimeAsync(9_999)
    expect(overview).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(overview).toHaveBeenCalledTimes(2)
    expect(catalog.props('overview').current_concurrency).toBe(5)
    expect(pools).not.toHaveBeenCalled()
    expect(wrapper.getComponent(SharedPoolCatalog).vm).toBe(catalog.vm)
    expect(summary).toHaveBeenCalledTimes(1)
    expect(accounts).toHaveBeenCalledTimes(1)
  })

  it('stops polling outside the pool tab and while the page is hidden', async () => {
    wrapper = await render()
    await vi.advanceTimersByTimeAsync(20_000)
    expect(overview).toHaveBeenCalledTimes(1)
    await wrapper.get('[role="tab"]').trigger('click')
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(20_000)
    expect(overview).toHaveBeenCalledTimes(1)
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(10_000)
    expect(overview).toHaveBeenCalledTimes(2)
  })

  it('waits for a pending pool request before polling again', async () => {
    wrapper = await render(false, 'pools')
    let resolve!: (value: typeof pool) => void
    overview.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    await vi.advanceTimersByTimeAsync(30_000)
    expect(overview).toHaveBeenCalledTimes(2)
    resolve({ ...pool, current_concurrency: 6 })
    await flushPromises()
    expect(wrapper.getComponent(SharedPoolCatalog).props('overview').current_concurrency).toBe(6)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(overview).toHaveBeenCalledTimes(3)
  })

  it('waits for the initial load before starting background refreshes', async () => {
    let resolve!: (value: typeof pool) => void
    overview.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    wrapper = await render(false, 'pools')
    await vi.advanceTimersByTimeAsync(20_000)
    expect(overview).toHaveBeenCalledTimes(1)
    resolve(pool)
    await flushPromises()
    expect(wrapper.getComponent(SharedPoolCatalog).props('overview')).toEqual(pool)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(overview).toHaveBeenCalledTimes(2)
  })

  it('keeps account contribution available when initial overview loading fails', async () => {
    overview.mockRejectedValueOnce(new Error('Overview offline'))
    wrapper = await render(false, 'pools')
    const catalog = wrapper.getComponent(SharedPoolCatalog)
    expect(catalog.props('overview')).toBeNull()
    expect(catalog.props('stale')).toBe(true)
    catalog.vm.$emit('contribute'); await flushPromises()
    expect(wrapper.findComponent(SharedAccountDialog).exists()).toBe(true)
    expect(wrapper.getComponent(SharedAccountDialog).props('config').settlement_multiplier).toBe(0.5)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('does not mark a newer successful snapshot stale when an older poll fails', async () => {
    wrapper = await render(false, 'pools')
    let reject!: (error: Error) => void
    overview.mockImplementationOnce(() => new Promise((_resolve, fail) => { reject = fail }))
    await vi.advanceTimersByTimeAsync(10_000)
    overview.mockResolvedValue({ ...pool, current_concurrency: 7 })
    await wrapper.findAll('button').find(button => button.text() === 'common.refresh')!.trigger('click')
    await flushPromises()
    reject(new Error('Old request failed')); await flushPromises()
    const catalog = wrapper.getComponent(SharedPoolCatalog)
    expect(catalog.props('overview').current_concurrency).toBe(7)
    expect(catalog.props('stale')).toBe(false)
  })

  it('discards a late poll after a manual refresh has fetched newer data', async () => {
    wrapper = await render(false, 'pools')
    let resolve!: (value: typeof pool) => void
    overview.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    await vi.advanceTimersByTimeAsync(10_000)
    overview.mockResolvedValue({ ...pool, current_concurrency: 7 })
    await wrapper.findAll('button').find(button => button.text() === 'common.refresh')!.trigger('click')
    await flushPromises()
    resolve({ ...pool, current_concurrency: 4 })
    await flushPromises()
    expect(wrapper.getComponent(SharedPoolCatalog).props('overview').current_concurrency).toBe(7)
  })

  it('discards a late poll after changing an account has refreshed the catalog', async () => {
    accounts.mockResolvedValue({ items: [{ id: 7, codex_ticket_enabled: true }], total: 1 })
    codexTicket.mockResolvedValue({ codex_ticket_enabled: false })
    wrapper = await render(false, 'pools')
    let resolve!: (value: typeof pool) => void
    overview.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    await vi.advanceTimersByTimeAsync(10_000)
    await wrapper.findAll('button').find(button => button.text() === 'sharedPool.myAccounts')!.trigger('click')
    overview.mockResolvedValue({ ...pool, current_concurrency: 7 })
    wrapper.getComponent(SharedAccountCard).vm.$emit('codexTicket', false)
    await flushPromises()
    resolve({ ...pool, current_concurrency: 4 })
    await flushPromises()
    await wrapper.get('[role="tab"]').trigger('click')
    expect(wrapper.getComponent(SharedPoolCatalog).props('overview').current_concurrency).toBe(7)
  })

  it('marks the snapshot stale after a failed poll and clears the flag after recovery', async () => {
    wrapper = await render(false, 'pools')
    overview.mockRejectedValueOnce(new Error('offline'))
    await vi.advanceTimersByTimeAsync(10_000)
    expect(wrapper.getComponent(SharedPoolCatalog).props('overview')).toEqual(pool)
    expect(wrapper.getComponent(SharedPoolCatalog).props('stale')).toBe(true)
    expect(showError).not.toHaveBeenCalled()
    expect(showWarning).not.toHaveBeenCalled()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(wrapper.getComponent(SharedPoolCatalog).props('overview')).toEqual(pool)
    expect(wrapper.getComponent(SharedPoolCatalog).props('stale')).toBe(false)
  })

  it('cleans up on unmount and ignores a late response', async () => {
    wrapper = await render(false, 'pools')
    const state = wrapper.vm as unknown as { overview: typeof pool }
    let resolve!: (value: typeof pool) => void
    overview.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    await vi.advanceTimersByTimeAsync(10_000)
    wrapper.unmount()
    wrapper = undefined
    resolve({ ...pool, current_concurrency: 7 })
    await flushPromises()
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(20_000)
    expect(overview).toHaveBeenCalledTimes(2)
    expect(state.overview).toEqual(pool)
    expect(vi.getTimerCount()).toBe(0)
  })
})
