import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DOMWrapper, flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'

import AccountsView from '../AccountsView.vue'
import AccountActionMenu from '@/components/admin/account/AccountActionMenu.vue'
import CodexTicketAlerts from '@/components/account/CodexTicketAlerts.vue'
import ImportDataModal from '@/components/admin/account/ImportDataModal.vue'
import { BulkEditAccountModal } from '@/components/account'

const {
  listAccounts,
  listWithEtag,
  getById,
  getBatchTodayStats,
  getUpstreamBillingProbeSettings,
  getAllProxies,
  listProxyGroups,
  getAllGroups,
  refreshCredentials,
  showError,
  showWarning
} = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  listWithEtag: vi.fn(),
  getById: vi.fn(),
  getBatchTodayStats: vi.fn(),
  getUpstreamBillingProbeSettings: vi.fn(),
  getAllProxies: vi.fn(),
  listProxyGroups: vi.fn(),
  getAllGroups: vi.fn(),
  refreshCredentials: vi.fn(),
  showError: vi.fn(),
  showWarning: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      list: listAccounts,
      getById,
      listWithEtag,
      getBatchTodayStats,
      getUpstreamBillingProbeSettings,
      delete: vi.fn(),
      batchClearError: vi.fn(),
      batchRefresh: vi.fn(),
      toggleSchedulable: vi.fn(),
      refreshCredentials
    },
    proxies: { getAll: getAllProxies, listGroups: listProxyGroups },
    groups: { getAll: getAllGroups }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showWarning, showSuccess: vi.fn(), showInfo: vi.fn() })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ token: 'test-token', isSimpleMode: false })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const DataTableStub = defineComponent({
  props: { data: { type: Array, default: () => [] } },
  template: `
    <div>
      <div v-for="row in data" :key="row.id" :data-account-name="row.name">
        <slot name="cell-select" :row="row" />
        <slot name="cell-groups" :row="row" />
        <slot name="cell-proxy" :row="row" />
        <slot name="cell-actions" :row="row" />
      </div>
    </div>
  `
})

const AccountGroupsCellStub = defineComponent({
  props: { groups: { type: Array, default: () => [] } },
  template: '<span data-test="account-groups">{{ groups.map(group => group.name).join(",") }}</span>'
})

const EditAccountModalStub = defineComponent({
  props: { show: Boolean, account: { type: Object, default: null } },
  template: '<div data-test="edit-account">{{ show ? account?.name : "" }}</div>'
})

const AccountTestModalStub = defineComponent({
  props: { show: Boolean, account: { type: Object, default: null } },
  template: '<div data-test="test-account">{{ show ? account?.name : "" }}</div>'
})

const AccountStatsModalStub = defineComponent({
  props: { show: Boolean, account: { type: Object, default: null } },
  template: '<div data-test="stats-account">{{ show ? account?.name : "" }}</div>'
})

const AccountTableActionsStub = defineComponent({
  emits: ['refresh'],
  template: '<div><button data-test="manual-refresh" @click="$emit(\'refresh\')">Refresh</button><slot name="after" /></div>'
})

function mountView(stubActionMenu = true) {
  return mount(AccountsView, {
    attachTo: document.body,
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
        DataTable: DataTableStub,
        AccountTableActions: AccountTableActionsStub,
        AccountTableFilters: true,
        AccountBulkActionsBar: true,
        Pagination: true,
        ConfirmDialog: true,
        AccountActionMenu: stubActionMenu,
        ImportDataModal: true,
        ReAuthAccountModal: true,
        AccountTestModal: AccountTestModalStub,
        AccountStatsModal: AccountStatsModalStub,
        ScheduledTestsPanel: true,
        SyncFromCrsModal: true,
        TempUnschedStatusModal: true,
        ErrorPassthroughRulesModal: true,
        TLSFingerprintProfilesModal: true,
        CreateAccountModal: true,
        EditAccountModal: EditAccountModalStub,
        BulkEditAccountModal: true,
        PlatformTypeBadge: true,
        AccountCapacityCell: true,
        AccountStatusIndicator: true,
        AccountTodayStatsCell: true,
        AccountGroupsCell: AccountGroupsCellStub,
        AccountUsageCell: true,
        UpstreamBillingRateCell: true,
        HelpTooltip: true,
        Icon: true,
        Teleport: stubActionMenu
      }
    }
  })
}

const listRow = {
  id: 42,
  name: 'compact row',
  platform: 'openai',
  type: 'oauth',
  status: 'active',
  schedulable: true,
  concurrency: 2,
  priority: 1,
  group_ids: [7],
  extra: {},
  credentials: {}
}

const fullAccount = {
  ...listRow,
  groups: [{ id: 7, name: 'codex', platform: 'openai' }],
  account_groups: [{ account_id: 42, group_id: 7 }],
  credentials: { api_key: 'redacted' },
  extra: { detail_only: true }
}

const ticketProxy = {
  id: 7,
  name: 'Ticket Tokyo',
  protocol: 'http',
  host: 'ticket.example',
  port: 8080,
  status: 'active',
  account_count: 3
}

function useFixedTicketProxyAccount() {
  listAccounts.mockResolvedValue({
    items: [{ ...listRow, extra: { codex_ticket_proxy_mode: 'fixed', codex_ticket_proxy_id: ticketProxy.id } }],
    total: 1,
    page: 1,
    page_size: 20,
    pages: 1
  })
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(complete => { resolve = complete })
  return { promise, resolve }
}

describe('admin AccountsView lite account list', () => {
  beforeEach(() => {
    localStorage.clear()
    listAccounts.mockReset().mockResolvedValue({ items: [listRow], total: 1, page: 1, page_size: 20, pages: 1 })
    listWithEtag.mockReset().mockResolvedValue({ notModified: true, etag: 'compact-etag', data: null })
    getById.mockReset().mockResolvedValue(fullAccount)
    getBatchTodayStats.mockReset().mockResolvedValue({ stats: {} })
    getUpstreamBillingProbeSettings.mockReset().mockResolvedValue({ enabled: true })
    getAllProxies.mockReset().mockResolvedValue([])
    listProxyGroups.mockReset().mockResolvedValue([{ id: 7, name: 'Tokyo pool', proxy_count: 2, active_proxy_count: 1 }])
    getAllGroups.mockReset().mockResolvedValue([{ id: 7, name: 'codex', platform: 'openai' }])
    refreshCredentials.mockReset()
    showError.mockReset()
    showWarning.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('opens the full editor for one imported account outside the current page', async () => {
    const imported = { ...fullAccount, id: 71, name: 'new account' }
    getById.mockResolvedValue(imported)
    const wrapper = mountView()
    await flushPromises()
    wrapper.getComponent(ImportDataModal).vm.$emit('imported-and-edit', [71])
    await flushPromises()

    expect(getById).toHaveBeenCalledWith(71)
    expect(wrapper.getComponent(EditAccountModalStub).props()).toMatchObject({ show: true, account: imported })
    expect(wrapper.getComponent(BulkEditAccountModal).props('show')).toBe(false)
    expect(listAccounts).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('opens bulk editing for only the imported IDs and their actual platforms', async () => {
    getById.mockImplementation((id: number) => Promise.resolve({
      ...fullAccount, id, platform: id === 71 ? 'openai' : 'anthropic', type: id === 71 ? 'oauth' : 'apikey'
    }))
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-account-name="compact row"] input[type="checkbox"]').setValue(true)
    wrapper.getComponent(ImportDataModal).vm.$emit('imported-and-edit', [71, 72])
    await flushPromises()

    const bulkEditor = wrapper.getComponent(BulkEditAccountModal)
    expect(bulkEditor.props('show')).toBe(true)
    expect(bulkEditor.props('accountIds')).toEqual([71, 72])
    expect(bulkEditor.props('target')).toEqual({
      mode: 'selected', accountIds: [71, 72],
      selectedPlatforms: ['openai', 'anthropic'], selectedTypes: ['oauth', 'apikey']
    })
    expect(wrapper.getComponent(EditAccountModalStub).props('show')).toBe(false)
    wrapper.unmount()
  })

  it('does not open either editor when import reports no account IDs', async () => {
    const wrapper = mountView()
    await flushPromises()
    wrapper.getComponent(ImportDataModal).vm.$emit('imported-and-edit', [])
    await flushPromises()

    expect(getById).not.toHaveBeenCalled()
    expect(wrapper.getComponent(EditAccountModalStub).props('show')).toBe(false)
    expect(wrapper.getComponent(BulkEditAccountModal).props('show')).toBe(false)
    wrapper.unmount()
  })

  it('keeps imported data after a detail read fails and does not open an incomplete editor', async () => {
    getById.mockRejectedValue(new Error('detail unavailable'))
    const wrapper = mountView()
    await flushPromises()
    wrapper.getComponent(ImportDataModal).vm.$emit('imported-and-edit', [71, 72])
    await flushPromises()

    expect(showError).toHaveBeenCalled()
    expect(listAccounts).toHaveBeenCalledTimes(2)
    expect(wrapper.getComponent(EditAccountModalStub).props('show')).toBe(false)
    expect(wrapper.getComponent(BulkEditAccountModal).props('show')).toBe(false)
    wrapper.unmount()
  })

  it.each(['revalidation_required', 'expired'] as const)('refreshes a %s ticket without an account timestamp change', async credential_state => {
    vi.useFakeTimers()
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    localStorage.setItem('account-auto-refresh', JSON.stringify({ enabled: true, interval_seconds: 5 }))
    localStorage.setItem('codex-ticket-desktop-alerts', 'true')
    const ticket = { model: 'gpt-6-astra', ready: true, blocked: false, remaining_seconds: 60, credential_state: 'available' }
    const initial = { ...listRow, updated_at: '2026-09-22T00:00:00Z', codex_turn_tickets: [ticket] }
    const next = { ...initial, codex_turn_tickets: [{ ...ticket, credential_state, ready: credential_state !== 'expired' }] }
    const page = { items: [initial], total: 1, page: 1, page_size: 20, pages: 1 }
    listAccounts.mockResolvedValue(page)
    listWithEtag.mockResolvedValueOnce({ notModified: false, etag: 'updated-ticket-state', data: { ...page, items: [next] } })
    const wrapper = mountView()
    try {
      await flushPromises()
      expect(showWarning).not.toHaveBeenCalled()
      await vi.advanceTimersByTimeAsync(6000)
      await flushPromises()
      expect(listWithEtag).toHaveBeenCalledOnce()
      expect(wrapper.findComponent(CodexTicketAlerts).props('accounts')[0].codex_turn_tickets?.[0].credential_state).toBe(credential_state)
      expect(showWarning).toHaveBeenCalledTimes(credential_state === 'expired' ? 1 : 0)
    } finally { wrapper.unmount() }
  })

  it('keeps lite=1 on the initial list request', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(listAccounts).toHaveBeenCalledWith(
      1,
      20,
      expect.objectContaining({ lite: '1' }),
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    wrapper.unmount()
  })

  it('maps group_ids through the group catalog for the table cell', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-test="account-groups"]').text()).toBe('codex')
    wrapper.unmount()
  })

  it('shows the random proxy group name and preserves unavailable group scope', async () => {
    listAccounts.mockResolvedValue({ items: [
      { ...listRow, extra: { proxy_mode: 'random', random_proxy_pool_scope: 'group', random_proxy_group_id: 7 } },
      { ...listRow, id: 43, extra: { proxy_mode: 'random', random_proxy_pool_scope: 'group', random_proxy_group_id: 99 } }
    ], total: 2, page: 1, page_size: 20, pages: 1 })
    const wrapper = mountView()
    await flushPromises()
    const cells = wrapper.findAll('[data-testid="account-random-proxy"]')
    expect(cells[0].text()).toContain('accountProxyGroups.listScope')
    expect(cells[0].text()).toContain('Tokyo pool')
    expect(cells[1].text()).toContain('accountProxyGroups.unavailableId')
    expect(cells[1].text()).not.toContain('admin.accounts.randomProxyPoolAll')
    wrapper.unmount()
  })

  it('retries proxy groups after a directory failure on manual refresh', async () => {
    listAccounts.mockResolvedValue({ items: [{ ...listRow, extra: {
      proxy_mode: 'random', random_proxy_pool_scope: 'group', random_proxy_group_id: 7
    } }], total: 1, page: 1, page_size: 20, pages: 1 })
    listProxyGroups.mockRejectedValueOnce(new Error('offline'))
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[data-testid="account-random-proxy"]').text()).toContain('accountProxyGroups.listLoadFailed')
    await wrapper.get('[data-test="manual-refresh"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="account-random-proxy"]').text()).toContain('Tokyo pool')
    wrapper.unmount()
  })

  it('resolves a fixed proxy group from its ID when the account snapshot omits the name', async () => {
    listAccounts.mockResolvedValue({ items: [{ ...listRow, proxy: { ...ticketProxy, group_id: 7 } }], total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[data-account-name="compact row"]').text()).toContain('Tokyo pool')
    wrapper.unmount()
  })

  it('waits for the proxy catalog and displays fixed proxies without waiting for groups', async () => {
    useFixedTicketProxyAccount()
    const proxiesRequest = deferred<typeof ticketProxy[]>()
    const groupsRequest = deferred<unknown[]>()
    getAllProxies.mockReturnValue(proxiesRequest.promise)
    getAllGroups.mockReturnValue(groupsRequest.promise)
    const wrapper = mountView()
    await flushPromises()

    const cell = wrapper.get('[data-testid="account-ticket-proxy"]')
    expect(cell.text()).toContain('common.loading')
    expect(cell.text()).not.toContain('admin.accounts.codexTicketProxy.listUnavailable')

    await wrapper.get('[data-test="manual-refresh"]').trigger('click')
    await flushPromises()
    expect(getAllProxies).toHaveBeenCalledTimes(1)

    proxiesRequest.resolve([ticketProxy])
    await flushPromises()
    expect(cell.text()).toContain(ticketProxy.name)
    expect(cell.text()).toContain('http://ticket.example:8080')
    expect(cell.text()).not.toContain('common.loading')
    expect(wrapper.get('[data-test="account-groups"]').text()).toBe('')

    groupsRequest.resolve([])
    await flushPromises()
    wrapper.unmount()
  })

  it('marks a fixed proxy unavailable only after a successful catalog response omits it', async () => {
    useFixedTicketProxyAccount()
    const wrapper = mountView()
    await flushPromises()

    const cell = wrapper.get('[data-testid="account-ticket-proxy"]')
    expect(cell.text()).toContain('admin.accounts.codexTicketProxy.listUnavailable')
    expect(cell.text()).toContain('ID: 7')
    expect(cell.text()).not.toContain('common.loading')
    wrapper.unmount()
  })

  it('displays explicit account outbound inheritance without requiring an independent proxy', async () => {
    listAccounts.mockResolvedValue({ items: [{ ...listRow, extra: {
      proxy_mode: 'random', random_proxy_pool_scope: 'group', random_proxy_group_id: 7,
      codex_ticket_proxy_mode: 'account', codex_ticket_proxy_id: 0
    } }], total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = mountView()
    await flushPromises()
    const cell = wrapper.get('[data-testid="account-ticket-proxy"]')
    expect(cell.text()).toContain('admin.accounts.codexTicketProxy.account')
    expect(cell.text()).not.toContain('admin.accounts.codexTicketProxy.inherit')
    expect(cell.text()).not.toContain('admin.accounts.codexTicketProxy.listUnavailable')
    wrapper.unmount()
  })

  it('shows proxy loading errors and retries them on manual refresh', async () => {
    useFixedTicketProxyAccount()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    getAllProxies.mockRejectedValueOnce(new Error('catalog failed')).mockResolvedValue([ticketProxy])
    const wrapper = mountView()
    await flushPromises()

    const cell = wrapper.get('[data-testid="account-ticket-proxy"]')
    expect(cell.text()).toContain('admin.proxies.failedToLoad')
    expect(cell.text()).not.toContain('admin.accounts.codexTicketProxy.listUnavailable')

    await wrapper.get('[data-test="manual-refresh"]').trigger('click')
    await flushPromises()
    expect(getAllProxies).toHaveBeenCalledTimes(2)
    expect(cell.text()).toContain(ticketProxy.name)
    expect(cell.text()).not.toContain('admin.proxies.failedToLoad')

    await wrapper.get('[data-test="manual-refresh"]').trigger('click')
    await flushPromises()
    expect(getAllProxies).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('keeps the action menu open during internal scrolling but closes it on table scrolling', async () => {
    const wrapper = mountView(false)
    await flushPromises()

    const trigger = wrapper.findAll('button').find(button => button.text() === 'common.more')!
    await trigger.trigger('click')
    const menu = new DOMWrapper(document.body.querySelector('.action-menu-content')!)
    menu.element.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(wrapper.findComponent(AccountActionMenu).props('show')).toBe(true)

    menu.get('button').element.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(wrapper.findComponent(AccountActionMenu).props('show')).toBe(true)

    wrapper.getComponent(DataTableStub).element.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(wrapper.findComponent(AccountActionMenu).props('show')).toBe(false)
    wrapper.unmount()
  })

  it('keeps lite=1 on automatic ETag refreshes', async () => {
    vi.useFakeTimers()
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    localStorage.setItem('account-auto-refresh', JSON.stringify({ enabled: true, interval_seconds: 5 }))
    const wrapper = mountView()
    await flushPromises()

    await vi.advanceTimersByTimeAsync(6000)
    await flushPromises()

    expect(listWithEtag).toHaveBeenCalledWith(
      1,
      20,
      expect.objectContaining({ lite: '1' }),
      expect.objectContaining({ etag: null })
    )
    expect(getAllProxies).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('loads the full account by id before opening edit, test, and stats actions', async () => {
    const wrapper = mountView()
    await flushPromises()

    const editButton = wrapper.findAll('button').find(button => button.text().includes('common.edit'))
    expect(editButton).toBeTruthy()
    await editButton!.trigger('click')
    await flushPromises()
    expect(getById).toHaveBeenCalledWith(42)
    expect(wrapper.get('[data-test="edit-account"]').text()).toBe('compact row')

    const menu = wrapper.findComponent(AccountActionMenu)
    menu.vm.$emit('test', listRow)
    await flushPromises()
    expect(getById).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-test="test-account"]').text()).toBe('compact row')

    menu.vm.$emit('stats', listRow)
    await flushPromises()
    expect(getById).toHaveBeenCalledTimes(3)
    expect(wrapper.get('[data-test="stats-account"]').text()).toBe('compact row')
    wrapper.unmount()
  })

  it('shows the warning and patches the account after a partial Antigravity refresh', async () => {
    refreshCredentials.mockResolvedValue({
      account: { ...fullAccount, name: 'refreshed account' },
      message: 'Token refreshed, but project_id is temporarily unavailable',
      warning: 'missing_project_id_temporary'
    })
    const wrapper = mountView(false)
    await flushPromises()

    wrapper.findComponent(AccountActionMenu).vm.$emit('refresh-token', listRow)
    await flushPromises()

    expect(refreshCredentials).toHaveBeenCalledWith(42)
    expect(wrapper.get('[data-account-name]').attributes('data-account-name')).toBe('refreshed account')
    expect(showWarning).toHaveBeenCalledWith('Token refreshed, but project_id is temporarily unavailable')
    wrapper.unmount()
  })

  it('shows an error and keeps the modal closed when detail loading fails', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    getById.mockRejectedValueOnce(new Error('detail failed'))
    const wrapper = mountView()
    await flushPromises()

    const editButton = wrapper.findAll('button').find(button => button.text().includes('common.edit'))
    await editButton!.trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('detail failed')
    expect(wrapper.get('[data-test="edit-account"]').text()).toBe('')
    consoleError.mockRestore()
    wrapper.unmount()
  })
})
