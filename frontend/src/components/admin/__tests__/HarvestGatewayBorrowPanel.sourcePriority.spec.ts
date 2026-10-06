import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import HarvestGatewayBorrowPanel from '../HarvestGatewayBorrowPanel.vue'
import { normalizeAstraGateway } from '@/api/admin/astraGateway'

const mocks = vi.hoisted(() => ({ get: vi.fn(), save: vi.fn(), list: vi.fn(), groups: vi.fn() }))
vi.mock('@/api/admin/astraGateway', async importOriginal => ({ ...await importOriginal<typeof import('@/api/admin/astraGateway')>(), getAstraGateway: mocks.get, saveAstraGateway: mocks.save }))
vi.mock('@/components/admin/AstraGatewayRuntime.vue', () => ({ default: { template: '<section />' } }))
vi.mock('@/components/admin/AstraGatewayHistory.vue', () => ({ default: { template: '<section />' } }))
vi.mock('@/api/admin/groups', () => ({ getAllIncludingInactive: mocks.groups }))
vi.mock('@/api/admin/accounts', () => ({ list: mocks.list }))
vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string, values?: { n: number }) => values ? `${key}:${values.n}` : key, te: () => true }) }))
function settings() { return normalizeAstraGateway({ cookie_pool: { enabled: true, source_account_ids: [299], target_account_ids: [300] }, ws_session: { enabled: false, account_ids: [] }, revision: 'one' }) }
function account(id: number, group_ids: number[]) { return { id, name: `account-${id}`, platform: 'openai', type: 'oauth', status: 'active', expires_at: null, group_ids } }
function render() { return mount(HarvestGatewayBorrowPanel, { global: { stubs: { RouterLink: true } } }) }
beforeEach(() => {
  vi.clearAllMocks()
  mocks.get.mockResolvedValue(settings())
  mocks.list.mockResolvedValue({ items: [account(299, [7]), account(300, [7, 8]), account(301, [8])], pages: 1 })
  mocks.groups.mockResolvedValue([7, 8].map(id => ({ id, name: `group-${id}`, platform: 'openai', status: 'active' })))
  mocks.save.mockImplementation(value => Promise.resolve({ ...value, revision: 'two' }))
})

describe('Astra borrowing source priority', () => {
  it('hides selected sources and removes existing overlapping account targets immediately', async () => {
    const value = settings(); value.cookie_pool.target_account_ids = [299, 300]
    mocks.get.mockResolvedValue(value)
    const w = render(); await flushPromises()
    const targets = w.get('[data-testid="target-scope"]')
    expect(targets.find('input[value="299"]').exists()).toBe(false)
    expect(targets.get('input[value="300"]').element).toHaveProperty('checked', true)
    expect(targets.get('legend').text()).toContain('1/64')
    await w.get('form').trigger('submit'); await flushPromises()
    expect(mocks.save.mock.calls[0][0].cookie_pool.target_account_ids).toEqual([300])
    w.unmount()
  })

  it('cancels a target when it becomes a source and does not reselect it after source removal', async () => {
    const w = render(); await flushPromises()
    await w.get('[data-testid="source-scope"] input[value="300"]').setValue(true)
    expect(w.get('[data-testid="target-scope"]').find('input[value="300"]').exists()).toBe(false)
    expect(w.get('[data-testid="target-scope"] legend').text()).toContain('0/64')
    await w.get('[data-testid="target-scope"] input[value="301"]').setValue(true)
    await w.get('[data-testid="source-scope"] input[value="300"]').setValue(false)
    expect(w.get('[data-testid="target-scope"] input[value="300"]').element).toHaveProperty('checked', false)
    expect(w.get('[data-testid="target-scope"] input[value="301"]').element).toHaveProperty('checked', true)
    await w.get('form').trigger('submit'); await flushPromises()
    expect(mocks.save.mock.calls[0][0].cookie_pool.target_account_ids).toEqual([301])
    w.unmount()
  })

  it('resolves source groups across all pages before pruning the target selection', async () => {
    const value = settings(); value.cookie_pool.target_account_ids = [300, 301]
    mocks.get.mockResolvedValue(value)
    mocks.list.mockResolvedValueOnce({ items: [account(299, [7])], pages: 2 }).mockResolvedValueOnce({ items: [account(300, [7]), account(301, [8])], pages: 2 })
    const w = render(); await flushPromises()
    expect(mocks.list).toHaveBeenCalledTimes(2)
    await w.get('[data-testid="source-selection"]').setValue('groups')
    await w.get('[data-testid="source-groups"] input[value="7"]').setValue(true)
    expect(w.get('[data-testid="target-scope"]').find('input[value="300"]').exists()).toBe(false)
    expect(w.get('[data-testid="target-scope"] input[value="301"]').element).toHaveProperty('checked', true)
    await w.get('form').trigger('submit'); await flushPromises()
    expect(mocks.save.mock.calls[0][0].cookie_pool.target_account_ids).toEqual([301])
    await w.get('[data-testid="source-selection"]').setValue('accounts')
    expect(w.get('[data-testid="target-scope"] input[value="300"]').element).toHaveProperty('checked', false)
    w.unmount()
  })

  it('preserves target groups while previewing only members outside the source groups', async () => {
    const value = settings()
    Object.assign(value.cookie_pool, { source_selection: 'groups', source_group_ids: [7], target_selection: 'groups', target_group_ids: [7, 8] })
    mocks.get.mockResolvedValue(value)
    const w = render(); await flushPromises()
    expect(w.get('[data-testid="source-preview"]').text()).toContain('groupPreview:2')
    expect(w.get('[data-testid="target-preview"]').text()).toContain('groupPreview:1')
    expect(w.get('[data-testid="target-groups"] input[value="7"]').element).toHaveProperty('checked', true)
    await w.get('[data-testid="ip-affinity-toggle"]').trigger('click')
    await w.get('form').trigger('submit'); await flushPromises()
    expect(mocks.save.mock.calls[0][0].cookie_pool).toMatchObject({ target_selection: 'groups', target_group_ids: [7, 8], target_account_ids: [] })
    w.unmount()
  })

  it('reconciles newly loaded source membership without reviving previously removed targets', async () => {
    const value = settings()
    Object.assign(value.cookie_pool, { source_selection: 'groups', source_group_ids: [7], target_account_ids: [300, 301] })
    mocks.get.mockResolvedValue(value)
    mocks.list.mockResolvedValueOnce({ items: [account(299, [7]), account(300, [8]), account(301, [8])], pages: 1 })
    const w = render(); await flushPromises()
    expect(w.get('[data-testid="target-scope"] input[value="300"]').element).toHaveProperty('checked', true)
    await w.get('[data-testid="reload"]').trigger('click'); await flushPromises()
    expect(w.get('[data-testid="target-scope"]').find('input[value="300"]').exists()).toBe(false)
    expect(w.get('[data-testid="target-scope"] input[value="301"]').element).toHaveProperty('checked', true)
    await w.get('[data-testid="source-groups"] input[value="7"]').setValue(false)
    expect(w.get('[data-testid="target-scope"] input[value="300"]').element).toHaveProperty('checked', false)
    w.unmount()
  })

  it('reports an empty target after excluding all members without clearing selected groups', async () => {
    const value = settings()
    Object.assign(value.cookie_pool, { source_selection: 'groups', source_group_ids: [7], target_selection: 'groups', target_group_ids: [7] })
    mocks.get.mockResolvedValue(value)
    const w = render(); await flushPromises()
    expect(w.get('[data-testid="target-preview"]').text()).toContain('groupPreview:0')
    expect(w.text()).toContain('admin.astraGateway.targetAccountsEmpty')
    expect(w.get('[data-testid="target-groups"] input[value="7"]').element).toHaveProperty('checked', true)
    await w.get('form').trigger('submit'); expect(mocks.save).not.toHaveBeenCalled()
    w.unmount()
  })

  it.each(['accounts', 'groups'])('retains selected targets while source %s metadata cannot be loaded', async unavailable => {
    const value = settings()
    Object.assign(value.cookie_pool, { source_selection: 'groups', source_group_ids: [7], source_account_ids: [299, 300], target_account_ids: [300, 301] })
    mocks.get.mockResolvedValue(value)
    if (unavailable === 'accounts') mocks.list.mockRejectedValue(new Error('offline'))
    else mocks.groups.mockRejectedValue(new Error('offline'))
    const w = render(); await flushPromises()
    expect(w.get('[data-testid="target-scope"] legend').text()).toContain('2/64')
    expect(w.get('[data-testid="save"]').attributes('disabled')).toBeDefined()
    await w.get('[data-testid="cookie-toggle"]').trigger('click')
    await w.get('form').trigger('submit'); await flushPromises()
    expect(mocks.save.mock.calls[0][0].cookie_pool.target_account_ids).toEqual([300, 301])
    w.unmount()
  })

  it('does not add a WS donor back into targets after source priority removes it', async () => {
    const value = settings(); value.cookie_pool.target_account_ids = [299, 300]
    value.ws_session = { enabled: true, account_ids: [299] }
    mocks.get.mockResolvedValue(value)
    const w = render(); await flushPromises()
    expect(w.get('[data-testid="target-scope"]').find('input[value="299"]').exists()).toBe(false)
    await w.get('form').trigger('submit'); await flushPromises()
    expect(mocks.save.mock.calls[0][0].cookie_pool.target_account_ids).toEqual([300])
    w.unmount()
  })
})
