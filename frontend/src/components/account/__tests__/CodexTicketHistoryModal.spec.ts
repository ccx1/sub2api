import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import CodexTicketHistoryModal from '../CodexTicketHistoryModal.vue'
import AccountActionMenu from '@/components/admin/account/AccountActionMenu.vue'
import accounts from '@/i18n/locales/zh/admin/accounts'
import type { CodexTicketHistory } from '@/api/admin/accounts'
import type { Account } from '@/types'

const { getHistory, retryTicket } = vi.hoisted(() => ({ getHistory: vi.fn(), retryTicket: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { accounts: { getCodexTicketHistory: getHistory, retryCodexTicket: retryTicket } } }))
vi.mock('@/api/admin/codexTicketDiagnostics', () => ({ getCodexTicketRuntimeStatus: vi.fn().mockResolvedValue({ state: 'idle', attempts_used: 0, max_attempts: 6, round: 0, max_rounds: 2, half_open: false, generation: 0 }), previewCodexTicketRequest: vi.fn() }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ locale: ref('zh'), t: translate, te: (key: string) => translate(key) !== key }) }))

function translate(key: string, params: Record<string, unknown> = {}): string {
  const messages = { admin: accounts, common: { refresh: '刷新', close: '关闭', loading: '加载中' } }
  const message = key.split('.').reduce<unknown>((value, part) => (value as Record<string, unknown> | undefined)?.[part], messages)
  return typeof message === 'string' ? message.replace(/\{(\w+)\}/g, (_, name: string) => String(params[name] ?? '')) : key
}

const BaseDialog = defineComponent({
  props: ['show'],
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})
const Pagination = defineComponent({
  props: ['page', 'total', 'pageSize'],
  emits: ['update:page'],
  template: '<button data-testid="next-page" @click="$emit(\'update:page\', page + 1)">Next</button>'
})

const account = { id: 1, name: 'Ticket account' }
function response(overrides: Partial<CodexTicketHistory> = {}): CodexTicketHistory {
  return {
    summary: { total: 120, success: 110, failed: 10, last_attempt_at: '2026-09-20T08:00:00Z' },
    items: [{
      id: 'attempt-1', started_at: '2026-09-20T08:00:00Z', finished_at: '2026-09-20T08:00:01Z',
      model: 'gpt-6-astra', success: false, reason: 'verification_timeout',
      harvest_proxy: { id: 2, name: 'Harvest US', address: 'http://proxy.example.test:8080' },
      business_proxy: { address: 'direct' }
    }],
    total: 100, page: 1, page_size: 20, retained_limit: 100,
    ...overrides
  }
}

const wrappers: Array<{ unmount: () => void }> = []
function mountModal(props = { show: true, account: account as typeof account | null }) {
  const wrapper = mount(CodexTicketHistoryModal, {
    props, global: { stubs: { BaseDialog, Pagination, Teleport: true } }
  })
  wrappers.push(wrapper)
  return wrapper
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}

beforeEach(() => { getHistory.mockReset(); retryTicket.mockReset() })
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()) })

describe('CodexTicketHistoryModal', () => {
  it('labels deferred attempts separately while explaining legacy totals', async () => {
    const data = response()
    Object.assign(data.items[0]!, { outcome: 'verification_deferred', reason: 'verification_deferred' })
    data.summary.outcome_counts = { verification_deferred: 1 }
    data.summary.classification_started_at = '2026-09-22T08:00:00Z'
    getHistory.mockResolvedValue(data)
    const wrapper = mountModal()
    await flushPromises()
    const row = wrapper.get('[data-testid="ticket-history-row-attempt-1"]')
    expect(row.text()).toContain('已采集，复验暂缓')
    expect(row.find('.bg-red-50').exists()).toBe(false)
    expect(wrapper.text()).toContain('不能作为上游失败率')
    expect(wrapper.text()).toContain('2026-09-22T08:00:00Z')
  })
  it('compares requested and both response models, then opens the chosen attempt in a dialog', async () => {
    const data = response()
    Object.assign(data.items[0]!, {
      reason: 'business_model_mismatch',
      harvest_exchange: { requested_model: 'gpt-6-astra', reported_models: ['gpt-6-astra', 'gpt-6-astra-2026-09'] },
      business_exchange: { requested_model: 'gpt-6-astra', reported_models: ['gpt-5.6-sol'] }
    })
    getHistory.mockResolvedValue(data)
    const wrapper = mountModal()
    await flushPromises()
    const row = wrapper.get('[data-testid="ticket-history-row-attempt-1"]')
    expect(row.get('[data-testid="harvest_exchange-models"]').text()).toBe('gpt-6-astra, gpt-6-astra-2026-09')
    expect(row.get('[data-testid="business_exchange-models"]').text()).toBe('gpt-5.6-sol')
    expect(wrapper.find('[data-testid="ticket-exchange-details"]').exists()).toBe(false)
    await row.get('[data-testid="view-exchange"]').trigger('click')
    expect(row.get('[data-testid="view-exchange"]').attributes('aria-haspopup')).toBe('dialog')
    expect(wrapper.get('[data-testid="ticket-exchange-details"]').text()).toContain('超出保留范围的详情已清理')
    await wrapper.get('[data-testid="ticket-exchange-dialog"] button').trigger('click')
    expect(wrapper.find('[data-testid="ticket-exchange-details"]').exists()).toBe(false)
  })

  it('shows legacy model and payload availability explicitly and clears expanded details on paging', async () => {
    getHistory.mockResolvedValueOnce(response()).mockResolvedValueOnce(response({ page: 2 }))
    const wrapper = mountModal()
    await flushPromises()
    expect(wrapper.get('[data-testid="harvest_exchange-models"]').text()).toBe('未记录/阶段未开始')
    expect(wrapper.text()).toContain('最近 10 条新记录')
    await wrapper.get('[data-testid="view-exchange"]').trigger('click')
    expect(wrapper.get('[data-testid="ticket-exchange-details"]').text()).toContain('旧记录无法补回')
    await wrapper.get('[data-testid="next-page"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="ticket-exchange-details"]').exists()).toBe(false)
  })

  it('shows quality round progress and failed response models after raw exchanges are pruned', async () => {
    const data = response()
    Object.assign(data.items[0]!, {
      reason: 'business_model_mismatch', business_verification_rounds: 5,
      business_verification_passed: 2, business_verification_models: ['gpt-6-astra', 'gpt-5.6-luna']
    })
    getHistory.mockResolvedValue(data)
    const wrapper = mountModal()
    await flushPromises()
    expect(wrapper.get('[data-testid="business-verification-progress"]').text()).toBe('质量探测通过轮次:2/5')
    expect(wrapper.get('[data-testid="business-verification-models"]').text()).toBe('探测实际返回模型:gpt-6-astra, gpt-5.6-luna')
    expect(wrapper.get('[data-testid="business_exchange-models"]').text()).toBe('未记录/阶段未开始')
  })

  it('keeps quality progress separate from an additional business egress failure', async () => {
    const data = response()
    Object.assign(data.items[0]!, {
      model: 'gpt-5.6-sol', reason: 'business_model_mismatch', business_verification_rounds: 3,
      business_verification_passed: 3, business_verification_models: ['gpt-5.6-sol'],
      business_exchange: { requested_model: 'gpt-5.6-sol', reported_models: ['gpt-5.6-luna'] }
    })
    getHistory.mockResolvedValue(data)
    const wrapper = mountModal()
    await flushPromises()
    expect(wrapper.get('[data-testid="business-verification-progress"]').text()).toContain('3/3')
    expect(wrapper.get('[data-testid="business-verification-models"]').text()).toContain('gpt-5.6-sol')
    expect(wrapper.get('[data-testid="business_exchange-models"]').text()).toBe('gpt-5.6-luna')
    expect(wrapper.get('[data-testid="attempt-details"]').text()).toContain('业务复验响应模型不匹配')
  })

  it('shows zero passed quality rounds and leaves legacy attempts without invented progress', async () => {
    const data = response()
    data.items = [
      { ...data.items[0]!, business_verification_rounds: 5, business_verification_models: ['gpt-5.6-luna'] },
      { ...data.items[0]!, id: 'legacy' },
      { ...data.items[0]!, id: 'single', business_verification_rounds: 1 }
    ]
    getHistory.mockResolvedValue(data)
    const wrapper = mountModal()
    await flushPromises()
    expect(wrapper.get('[data-testid="business-verification-progress"]').text()).toContain('0/5')
    expect(wrapper.findAll('[data-testid="business-verification-progress"]')).toHaveLength(1)
  })

  it('explains a strict mismatch using the recorded length and historical requirement', async () => {
    const data = response()
    Object.assign(data.items[0]!, {
      reason: 'ticket_length_mismatch', length_mode: 'strict', target_length: 292,
      rejected_lengths: [312], harvest_ticket_length: 312, harvest_http_status: 200
    })
    getHistory.mockResolvedValue(data)
    const wrapper = mountModal()
    await flushPromises()
    const row = wrapper.get('[data-testid="ticket-history-row-attempt-1"]')
    expect(row.get('[data-testid="attempt-details"]').text()).toContain('票据长度不匹配')
    expect(row.get('[data-testid="harvest_ticket_length"]').text()).toBe('312 · HTTP 200')
    expect(row.get('[data-testid="business_ticket_length"]').text()).toBe('未记录/未收到响应')
    expect(row.get('[data-testid="attempt-rule"]').text()).toBe('严格筛选；本次要求 292')
    expect(row.get('[data-testid="attempt-rejected-lengths"]').text()).toBe('本次异常长度：312')
    expect(wrapper.get('[data-testid="ticket-history-table"]').classes()).toContain('overflow-x-auto')
  })

  it('distinguishes no observation, an explicit missing ticket, and measured lengths in both stages', async () => {
    const data = response()
    data.items = [
      { ...data.items[0]!, id: 'legacy', harvest_ticket_length: undefined, business_ticket_length: null },
      { ...data.items[0]!, id: 'missing', reason: 'ticket_missing', harvest_ticket_length: 0, business_ticket_length: 0 },
      { ...data.items[0]!, id: 'observed', harvest_ticket_length: 332, business_ticket_length: 292 }
    ]
    getHistory.mockResolvedValue(data)
    const wrapper = mountModal()
    await flushPromises()
    const legacy = wrapper.get('[data-testid="ticket-history-row-legacy"]')
    expect(legacy.get('[data-testid="harvest_ticket_length"]').text()).toBe('未记录/未收到响应')
    expect(legacy.get('[data-testid="business_ticket_length"]').text()).toBe('未记录/未收到响应')
    expect(legacy.get('[data-testid="attempt-rule"]').text()).toBe('未记录')
    expect(legacy.text()).not.toContain('未返回票据')
    expect(legacy.text()).not.toContain('本次要求')
    const missing = wrapper.get('[data-testid="ticket-history-row-missing"]')
    expect(missing.get('[data-testid="harvest_ticket_length"]').text()).toBe('未返回票据（0）')
    expect(missing.get('[data-testid="business_ticket_length"]').text()).toBe('未返回票据（0）')
    const observed = wrapper.get('[data-testid="ticket-history-row-observed"]')
    expect(observed.get('[data-testid="harvest_ticket_length"]').text()).toBe('332')
    expect(observed.get('[data-testid="business_ticket_length"]').text()).toBe('292')
  })

  it('shows observed lengths for dynamic model mismatch without applying stored strict lengths', async () => {
    const data = response()
    Object.assign(data.items[0]!, {
      reason: 'business_model_mismatch', length_mode: 'auto', target_length: 292, rejected_lengths: [312],
      harvest_ticket_length: 312, business_ticket_length: 352, harvest_http_status: 200, business_http_status: 200
    })
    getHistory.mockResolvedValue(data)
    const wrapper = mountModal()
    await flushPromises()
    const details = wrapper.get('[data-testid="attempt-details"]')
    expect(details.text()).toContain('业务复验响应模型不匹配')
    expect(details.get('[data-testid="harvest_ticket_length"]').text()).toBe('312 · HTTP 200')
    expect(details.get('[data-testid="business_ticket_length"]').text()).toBe('352 · HTTP 200')
    expect(details.get('[data-testid="attempt-rule"]').text()).toBe('动态验证（不限制固定长度）')
    expect(details.text()).not.toContain('本次要求')
    expect(details.find('[data-testid="attempt-rejected-lengths"]').exists()).toBe(false)
  })

  it('keeps business HTTP failures and unrecorded strict targets explicit', async () => {
    const data = response()
    Object.assign(data.items[0]!, {
      reason: 'business_http_rejected', length_mode: 'strict', target_length: null,
      harvest_ticket_length: 292, business_ticket_length: 0, harvest_http_status: 200, business_http_status: 403
    })
    getHistory.mockResolvedValue(data)
    const wrapper = mountModal()
    await flushPromises()
    const details = wrapper.get('[data-testid="attempt-details"]')
    expect(details.text()).toContain('业务复验请求被拒绝')
    expect(details.get('[data-testid="business_ticket_length"]').text()).toBe('未返回票据（0） · HTTP 403')
    expect(details.get('[data-testid="attempt-rule"]').text()).toBe('严格筛选；目标长度未记录')
  })

  it('shows cumulative counts, attempt evidence and distinct proxy states', async () => {
    const data = response()
    data.items.push({ ...data.items[0]!, id: 'attempt-2', reason: 'business_transport_failed', harvest_proxy: null, business_proxy: null })
    data.items.push({ ...data.items[0]!, id: 'attempt-3', success: true, reason: 'verified' })
    getHistory.mockResolvedValue(data)
    const wrapper = mountModal()
    await flushPromises()

    expect(getHistory).toHaveBeenCalledWith(1, { page: 1, page_size: 20 })
    expect(wrapper.get('[data-testid="ticket-history-total"]').text()).toBe('120')
    expect(wrapper.get('[data-testid="ticket-history-success"]').text()).toBe('110')
    expect(wrapper.get('[data-testid="ticket-history-failed"]').text()).toBe('10')
    expect(wrapper.text()).toContain('最近 100 条')
    expect(wrapper.text()).toContain('2026-09-20T08:00:00Z')
    expect(wrapper.text()).toContain('gpt-6-astra')
    expect(wrapper.text()).toContain('verification_timeout')
    expect(wrapper.text()).toContain('业务复验连接失败')
    expect(wrapper.text()).not.toContain('verified')
    expect(wrapper.text()).toContain('耗时 1000 ms')
    expect(wrapper.text()).toContain('Harvest US')
    expect(wrapper.text()).toContain('http://proxy.example.test:8080')
    expect(wrapper.text()).toContain('直连')
    expect(wrapper.text()).toContain('未选择（阶段未开始）')
    expect(wrapper.findComponent(Pagination).props('total')).toBe(100)
  })

  it('requests explicit pages and resets pagination when the account changes', async () => {
    const pages = [292, 312, 352].map((length, index) => {
      const data = response({ page: index === 1 ? 2 : 1 })
      data.items[0]!.harvest_ticket_length = length
      return data
    })
    getHistory.mockResolvedValueOnce(pages[0]).mockResolvedValueOnce(pages[1]).mockResolvedValueOnce(pages[2])
    const wrapper = mountModal()
    await flushPromises()
    expect(wrapper.get('[data-testid="harvest_ticket_length"]').text()).toBe('292')
    await wrapper.get('[data-testid="next-page"]').trigger('click')
    await flushPromises()
    expect(getHistory).toHaveBeenLastCalledWith(1, { page: 2, page_size: 20 })
    expect(wrapper.get('[data-testid="harvest_ticket_length"]').text()).toBe('312')
    await wrapper.setProps({ account: { id: 2, name: 'Second account' } })
    await flushPromises()
    expect(getHistory).toHaveBeenLastCalledWith(2, { page: 1, page_size: 20 })
    expect(wrapper.findComponent(Pagination).props('page')).toBe(1)
    expect(wrapper.get('[data-testid="harvest_ticket_length"]').text()).toBe('352')
  })

  it('ignores the prior account response when requests finish out of order', async () => {
    const previous = deferred<CodexTicketHistory>()
    getHistory.mockReturnValueOnce(previous.promise).mockResolvedValueOnce(response({ items: [], total: 0 }))
    const wrapper = mountModal()
    expect(wrapper.find('[role="status"]').exists()).toBe(true)
    await wrapper.setProps({ account: { id: 2, name: 'Second account' } })
    await flushPromises()
    previous.resolve(response())
    await flushPromises()
    expect(wrapper.text()).toContain('暂无打票记录')
    expect(wrapper.text()).not.toContain('gpt-6-astra')
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
  })

  it('retries the failed page and clears its error when data arrives', async () => {
    getHistory.mockResolvedValueOnce(response()).mockRejectedValueOnce(new Error('unavailable')).mockResolvedValueOnce(response({ page: 2 }))
    const wrapper = mountModal()
    await flushPromises()
    await wrapper.get('[data-testid="next-page"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('加载打票记录失败')
    await wrapper.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(getHistory).toHaveBeenLastCalledWith(1, { page: 2, page_size: 20 })
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.findComponent(Pagination).props('page')).toBe(2)
  })

  it('starts a forced one-shot retry for configured models', async () => {
    getHistory.mockResolvedValue(response())
    retryTicket.mockResolvedValue({ scheduled: 1, skipped: 1, models: ['gpt-6-astra'] })
    const wrapper = mountModal()
    await flushPromises()
    await wrapper.get('[data-testid="retry-tickets"]').trigger('click')
    await flushPromises()
    expect(retryTicket).toHaveBeenCalledWith(1)
    expect(wrapper.text()).toContain('已发起 1 个模型的重新打票：gpt-6-astra')
  })

  it.each(['success', 'failure'] as const)('ignores stale retry %s after switching accounts', async (outcome) => {
    const previous = deferred<{ scheduled: number; skipped: number; models: string[] }>()
    const current = deferred<{ scheduled: number; skipped: number; models: string[] }>()
    getHistory.mockResolvedValue(response())
    retryTicket.mockReturnValueOnce(previous.promise).mockReturnValueOnce(current.promise)
    const wrapper = mountModal()
    await flushPromises()
    await wrapper.get('[data-testid="retry-tickets"]').trigger('click')
    await wrapper.setProps({ account: { id: 2, name: 'Second account' } })
    await flushPromises()
    expect(wrapper.get('[data-testid="retry-tickets"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-testid="retry-tickets"]').trigger('click')
    expect(retryTicket).toHaveBeenLastCalledWith(2)
    if (outcome === 'success') previous.resolve({ scheduled: 1, skipped: 0, models: ['gpt-6-astra'] })
    else previous.reject(new Error('unavailable'))
    await flushPromises()
    expect(wrapper.get('[data-testid="retry-tickets"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(getHistory).toHaveBeenCalledTimes(2)
    current.resolve({ scheduled: 1, skipped: 0, models: ['gpt-5.6-sol'] })
    await flushPromises()
    expect(wrapper.text()).toContain('已发起 1 个模型的重新打票：gpt-5.6-sol')
    expect(wrapper.get('[data-testid="retry-tickets"]').attributes('disabled')).toBeUndefined()
  })

  it('discards retry results after closing and reopening the same account', async () => {
    const previous = deferred<{ scheduled: number; skipped: number; models: string[] }>()
    getHistory.mockResolvedValue(response())
    retryTicket.mockReturnValueOnce(previous.promise)
    const wrapper = mountModal()
    await flushPromises()
    await wrapper.get('[data-testid="retry-tickets"]').trigger('click')
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()
    previous.resolve({ scheduled: 1, skipped: 0, models: ['gpt-6-astra'] })
    await flushPromises()
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="retry-tickets"]').attributes('disabled')).toBeUndefined()
    expect(getHistory).toHaveBeenCalledTimes(2)
  })

  it('does not fetch while closed and discards results across close and reopen', async () => {
    const previous = deferred<CodexTicketHistory>()
    getHistory.mockReturnValueOnce(previous.promise).mockResolvedValueOnce(response({ items: [], total: 0 }))
    const wrapper = mountModal({ show: false, account })
    expect(getHistory).not.toHaveBeenCalled()
    await wrapper.setProps({ show: true })
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()
    previous.resolve(response())
    await flushPromises()
    expect(wrapper.text()).toContain('暂无打票记录')
    expect(wrapper.text()).not.toContain('gpt-6-astra')
    expect(getHistory).toHaveBeenCalledTimes(2)
  })

  it('emits close from the footer', async () => {
    getHistory.mockResolvedValue(response({ items: [], total: 0 }))
    const wrapper = mountModal()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === '关闭')!.trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})

describe('AccountActionMenu ticket history action', () => {
  it('uses the visibility prop and emits the selected account before closing', async () => {
    const selected = { ...account, platform: 'openai', type: 'oauth', extra: {} } as Account
    const wrapper = mount(AccountActionMenu, {
      props: { show: true, account: selected, anchorRect: new DOMRect(0, 0, 30, 30), showCodexTicketHistory: false },
      global: { stubs: { Teleport: true, Icon: true } }
    })
    wrappers.push(wrapper)
    expect(wrapper.text()).not.toContain('打票记录')
    await wrapper.setProps({ showCodexTicketHistory: true })
    await wrapper.findAll('button').find(button => button.text() === '打票记录')!.trigger('click')
    expect(wrapper.emitted('codex-ticket-history')).toEqual([[selected]])
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
