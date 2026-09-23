import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import CodexTicketHistoryModal from '../CodexTicketHistoryModal.vue'
import accounts from '@/i18n/locales/zh/admin/accounts'
import type { CodexTicketHistory } from '@/api/admin/accounts'

const { getHistory } = vi.hoisted(() => ({ getHistory: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { accounts: { getCodexTicketHistory: getHistory } } }))
vi.mock('@/api/admin/codexTicketDiagnostics', () => ({ getCodexTicketRuntimeStatus: vi.fn().mockResolvedValue({ state: 'idle', attempts_used: 0, max_attempts: 6, round: 0, max_rounds: 2, half_open: false, generation: 0 }), previewCodexTicketRequest: vi.fn() }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ locale: ref('zh'), t: translate, te: (key: string) => translate(key) !== key }) }))

function translate(key: string, params: Record<string, unknown> = {}): string {
  const message = key.split('.').reduce<unknown>((value, part) => (value as Record<string, unknown> | undefined)?.[part], { admin: accounts })
  return typeof message === 'string' ? message.replace(/\{(\w+)\}/g, (_, name: string) => String(params[name] ?? '')) : key
}

const BaseDialog = defineComponent({ props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' })
const Pagination = defineComponent({
  props: ['page', 'total', 'pageSize'], emits: ['update:page'],
  template: '<button data-testid="next-page" @click="$emit(\'update:page\', page + 1)">Next</button>'
})
function response(overrides: Partial<CodexTicketHistory> = {}): CodexTicketHistory {
  return {
    summary: { total: 120, success: 110, failed: 10 },
    items: [{ id: 'one', started_at: '2026-09-20T08:00:00Z', finished_at: '2026-09-20T08:00:01Z', model: 'gpt-6-astra', success: true, reason: 'verified' }],
    total: 40, page: 1, page_size: 20, retained_limit: 100,
    filter_options: { models: ['gpt-6-astra', 'gpt-5.6-sol'], reasons: ['attempt:harvest_model_mismatch', 'invalidation:response_model_mismatch', 'invalidation:ttl_expired'] },
    ...overrides
  }
}
const wrappers: Array<{ unmount: () => void }> = []
function mountModal() {
  const wrapper = mount(CodexTicketHistoryModal, {
    props: { show: true, account: { id: 1, name: 'Ticket account' } },
    global: { stubs: { BaseDialog, Pagination, CodexTicketExchangeDialog: true } }
  })
  wrappers.push(wrapper)
  return wrapper
}
beforeEach(() => { getHistory.mockReset() })
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()) })

describe('ticket history filtering and lifecycle', () => {
  it('filters classified deferrals separately from legacy failed results', async () => {
    getHistory.mockResolvedValue(response())
    const wrapper = mountModal()
    await flushPromises()
    await wrapper.get('[data-testid="filter-outcome"]').setValue('verification_deferred')
    await wrapper.get('[data-testid="ticket-history-filters"]').trigger('submit')
    await flushPromises()
    expect(getHistory).toHaveBeenLastCalledWith(1, { page: 1, page_size: 20, outcome: 'verification_deferred' })
  })
  it('sends combined filters to the server before pagination and resets page on apply or clear', async () => {
    getHistory.mockResolvedValueOnce(response()).mockResolvedValueOnce(response({ page: 2 }))
      .mockResolvedValueOnce(response({ total: 1 })).mockResolvedValueOnce(response({ page: 2, total: 21 }))
      .mockResolvedValueOnce(response())
    const wrapper = mountModal()
    await flushPromises()
    await wrapper.get('[data-testid="next-page"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="filter-result"]').setValue('success')
    await wrapper.get('[data-testid="filter-ticket-status"]').setValue('invalidated')
    await wrapper.get('[data-testid="filter-model"]').setValue('gpt-6-astra')
    await wrapper.get('[data-testid="filter-reason"]').setValue('invalidation:response_model_mismatch')
    await wrapper.get('[data-testid="filter-started-from"]').setValue('2026-09-20T10:00:00')
    await wrapper.get('[data-testid="filter-started-to"]').setValue('2026-09-20T11:00:00')
    await wrapper.get('[data-testid="ticket-history-filters"]').trigger('submit')
    await flushPromises()
    const filters = {
      result: 'success', ticket_status: 'invalidated', model: 'gpt-6-astra', reason: 'invalidation:response_model_mismatch',
      started_from: new Date('2026-09-20T10:00:00').toISOString(), started_to: new Date('2026-09-20T11:00:00').toISOString()
    }
    expect(getHistory).toHaveBeenLastCalledWith(1, { page: 1, page_size: 20, ...filters })
    expect(wrapper.get('[data-testid="ticket-history-total"]').text()).toBe('120')
    expect(wrapper.findComponent(Pagination).props('total')).toBe(1)
    await wrapper.get('[data-testid="next-page"]').trigger('click')
    await flushPromises()
    expect(getHistory).toHaveBeenLastCalledWith(1, { page: 2, page_size: 20, ...filters })
    await wrapper.get('[data-testid="filter-reset"]').trigger('click')
    await flushPromises()
    expect(getHistory).toHaveBeenLastCalledWith(1, { page: 1, page_size: 20 })
    expect((wrapper.get('[data-testid="filter-reason"]').element as HTMLSelectElement).value).toBe('')
  })

  it('keeps filters and options usable through loading, failure and an empty filtered response', async () => {
    let fail!: (error: Error) => void
    const pending = new Promise<CodexTicketHistory>((_, reject) => { fail = reject })
    getHistory.mockResolvedValueOnce(response()).mockReturnValueOnce(pending).mockResolvedValueOnce(response({ items: [], total: 0 }))
    const wrapper = mountModal()
    await flushPromises()
    await wrapper.get('[data-testid="filter-reason"]').setValue('attempt:harvest_model_mismatch')
    await wrapper.get('[data-testid="ticket-history-filters"]').trigger('submit')
    expect(wrapper.get('[data-testid="filter-apply"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-testid="filter-reason"]').text()).toContain('打票：采集响应模型不匹配')
    fail(new Error('unavailable'))
    await flushPromises()
    expect(wrapper.get('[data-testid="filter-reason"]').text()).toContain('失效：本地缓存 TTL 已到期')
    expect((wrapper.get('[data-testid="filter-reason"]').element as HTMLSelectElement).value).toBe('attempt:harvest_model_mismatch')
    await wrapper.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(getHistory).toHaveBeenLastCalledWith(1, { page: 1, page_size: 20, reason: 'attempt:harvest_model_mismatch' })
    expect(wrapper.text()).toContain('没有符合筛选条件的打票记录')
  })

  it('rejects reversed dates and resets filters when the account changes', async () => {
    getHistory.mockResolvedValue(response())
    const wrapper = mountModal()
    await flushPromises()
    await wrapper.get('[data-testid="filter-started-from"]').setValue('2026-09-21T10:00:00')
    await wrapper.get('[data-testid="filter-started-to"]').setValue('2026-09-20T10:00:00')
    await wrapper.get('[data-testid="ticket-history-filters"]').trigger('submit')
    expect(wrapper.get('[role="alert"]').text()).toContain('开始时间不能晚于结束时间')
    expect(getHistory).toHaveBeenCalledTimes(1)
    await wrapper.get('[data-testid="filter-result"]').setValue('failed')
    await wrapper.setProps({ account: { id: 2, name: 'Second' } })
    await flushPromises()
    expect(getHistory).toHaveBeenLastCalledWith(2, { page: 1, page_size: 20 })
    expect((wrapper.get('[data-testid="filter-result"]').element as HTMLSelectElement).value).toBe('')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('shows revocation evidence alongside the original successful attempt without changing success counts', async () => {
    const data = response()
    Object.assign(data.items[0]!, {
      ticket_status: 'invalidated', ticket_expires_at: '2026-09-20T09:00:00Z',
      invalidation: { attempt_id: 'one', model: 'gpt-6-astra', captured_at: '2026-09-20T08:00:00Z', invalidated_at: '2026-09-20T08:08:00Z',
        reason: 'response_model_mismatch', source: 'websocket_prewarm', reported_models: ['gpt-5.6-luna'], returned_ticket_length: 312 }
    })
    getHistory.mockResolvedValue(data)
    const wrapper = mountModal()
    await flushPromises()
    const row = wrapper.get('[data-testid="ticket-history-row-one"]')
    expect(row.text()).toContain('成功')
    expect(row.text()).toContain('票据状态: 已撤销')
    expect(row.text()).toContain('失效原因: 业务响应模型不匹配')
    expect(row.text()).toContain('撤销时间: 2026-09-20T08:08:00Z')
    expect(row.text()).toContain('触发来源: WebSocket 预热')
    expect(row.text()).toContain('触发响应模型: gpt-5.6-luna')
    expect(row.text()).toContain('触发响应票长: 312')
    expect(wrapper.get('[data-testid="ticket-history-success"]').text()).toBe('110')
  })

  it('distinguishes TTL expiry and legacy unknown status without inventing a revocation time', async () => {
    const data = response()
    data.items.push({ ...data.items[0]!, id: 'expired', ticket_status: 'ttl_elapsed', ticket_expires_at: '2026-09-20T09:00:00Z' })
    data.items.push({ ...data.items[0]!, id: 'issued', ticket_status: 'issued' })
    getHistory.mockResolvedValue(data)
    const wrapper = mountModal()
    await flushPromises()
    const legacy = wrapper.get('[data-testid="ticket-history-row-one"]')
    expect(legacy.text()).toContain('票据状态: 未记录')
    expect(legacy.text()).toContain('旧记录未保存失效信息')
    expect(legacy.text()).not.toContain('失效原因:')
    const expired = wrapper.get('[data-testid="ticket-history-row-expired"]')
    expect(expired.text()).toContain('票据状态: 缓存到期')
    expect(expired.text()).toContain('本地缓存 TTL 已到期')
    expect(expired.text()).not.toContain('撤销时间:')
    expect(wrapper.get('[data-testid="ticket-history-row-issued"]').text()).toContain('票据状态: 已出票')
  })
})
