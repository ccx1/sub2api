import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CodexTicketDiagnostics from '../CodexTicketDiagnostics.vue'
import CodexTicketRequestPreview from '../CodexTicketRequestPreview.vue'
import CodexTicketRuntimePanel from '../CodexTicketRuntimePanel.vue'
import CodexTicketLifecycle from '../CodexTicketLifecycle.vue'
import accounts from '@/i18n/locales/zh/admin/accounts'
import type { CodexTicketPreview, CodexTicketRuntimeStatus } from '@/api/admin/codexTicketDiagnostics'

const mocks = vi.hoisted(() => ({ preview: vi.fn(), runtime: vi.fn() }))
vi.mock('@/api/admin/codexTicketDiagnostics', () => ({ previewCodexTicketRequest: mocks.preview, getCodexTicketRuntimeStatus: mocks.runtime }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: translate, te: (key: string) => translate(key) !== key }) }))
function translate(key: string): string {
  const message = key.split('.').reduce<unknown>((value, part) => (value as Record<string, unknown> | undefined)?.[part], { admin: accounts, common: { close: '关闭', refresh: '刷新' } })
  return typeof message === 'string' ? message : key
}
const wrappers: Array<{ unmount: () => void }> = []
const previewResult = (): CodexTicketPreview => ({
  before_headers: { Authorization: ['Bearer [PLACEHOLDER]'], 'X-Test': ['old'] },
  after_headers: { Authorization: ['Bearer [PLACEHOLDER]'], 'X-Test': ['new'] },
  changes: [{ name: 'X-Test', before: ['old'], after: ['new'], kind: 'changed' }],
  validation_errors: [{ field: 'set[0].name', message: 'invalid header name' }],
  header_sources: [{ name: 'Authorization', source: 'placeholder', reason: 'Preview only' }], sent: false
})
const runtime = (changes: Partial<CodexTicketRuntimeStatus> = {}): CodexTicketRuntimeStatus => ({
  state: 'waiting', reason: 'verification_deferred', attempts_used: 2, max_attempts: 6,
  round: 1, max_rounds: 2, half_open: false, generation: 4,
  retry_at: '2026-09-22T08:05:00Z', cooldown_until: '2026-09-22T08:30:00Z', ...changes
})
beforeEach(() => { vi.clearAllMocks() })
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); document.body.innerHTML = '' })

describe('ticket diagnostic observations', () => {
  it('shows HTTP or WS invalidation signals without inventing a harvest exchange', () => {
    const wrapper = mount(CodexTicketLifecycle, { props: { attempt: {
      id: 'issued', started_at: '2026-09-22T08:00:00Z', finished_at: '2026-09-22T08:00:01Z', model: 'astra', success: true, reason: 'verified', ticket_status: 'invalidated',
      invalidation: { attempt_id: 'issued', model: 'astra', captured_at: '2026-09-22T08:00:00Z', invalidated_at: '2026-09-22T08:01:00Z', reason: 'response_model_mismatch', source: 'websocket', signals: { safety_buffering_enabled: true, faster_model: 'luna', quota: [{ id: '5h', used_percent: 100, limit_reached: true }] } }
    } } })
    wrappers.push(wrapper)
    expect(wrapper.text()).toContain('撤销时的上游信号')
    expect(wrapper.get('[data-testid="upstream-signals"]').text()).toContain('Faster Model: luna')
    expect(wrapper.get('[data-testid="upstream-signals"]').text()).toContain('已达限制: 是')
    expect(wrapper.find('[data-testid="ticket-diagnostics"]').exists()).toBe(false)
  })
  it('marks legacy values as unrecorded instead of zero or a model conflict', () => {
    const wrapper = mount(CodexTicketDiagnostics, { props: {} })
    wrappers.push(wrapper)
    expect(wrapper.text()).toContain('未记录')
    expect(wrapper.text()).toContain('不代表公网出口 IP')
    expect(wrapper.text()).not.toContain('响应头耗时（毫秒）: 0')
  })

  it('keeps zero timing, false conflict, error status and bounded signals distinct', async () => {
    const exchange = {
      requested_model: 'astra', network: { response_header_ms: 0, peer_addr: '127.0.0.1:8080', http_version: 'HTTP/2', final_origin: 'https://example.test' },
      model_declaration: { first_model: 'luna', terminal_model: 'luna', conflict: false },
      upstream_error: { wire_http_status: 200, effective_status: 429, type: 'rate_limit', retry_at: '2026-09-22T08:00:00Z' },
      signals: { safety_buffering_enabled: true, quota: Array.from({ length: 10 }, (_, index) => ({ id: `window-${index}`, used_percent: 100, limit_reached: true })) }
    }
    const wrapper = mount(CodexTicketDiagnostics, { props: { exchange } })
    wrappers.push(wrapper)
    expect(wrapper.text()).toContain('响应头耗时（毫秒）: 0')
    expect(wrapper.text()).toContain('模型声明冲突: 否')
    expect(wrapper.get('[data-testid="upstream-error"]').text()).toContain('有效错误状态: 429')
    expect(wrapper.get('[data-testid="upstream-signals"]').text()).not.toContain('window-8')
    await wrapper.setProps({ exchange: { ...exchange, model_declaration: { first_model: 'luna', terminal_model: 'astra', conflict: true } } })
    expect(wrapper.text()).toContain('模型声明冲突: 是')
  })
})

describe('ticket request preview', () => {
  function render() {
    const wrapper = mount(CodexTicketRequestPreview, { props: { accountId: 7, initialModel: 'astra' }, global: { stubs: { Teleport: true } }, attachTo: document.body })
    wrappers.push(wrapper)
    return wrapper
  }
  it('makes no automatic request and sends invalid characters intact for server validation', async () => {
    mocks.preview.mockResolvedValue(previewResult())
    const wrapper = render()
    expect(mocks.preview).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="preview-set-name-0"]').setValue('X-Bad\nName')
    await wrapper.get('[data-testid="preview-set-value-0"]').setValue('value\nnext')
    await wrapper.get('[data-testid="preview-add-remove"]').trigger('click')
    await wrapper.get('[data-testid="preview-remove-0"]').setValue('Host')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.preview).toHaveBeenCalledWith(7, { model: 'astra', set: [{ name: 'X-Bad\nName', value: 'value\nnext' }], remove: ['Host'] })
    expect(wrapper.get('[data-testid="preview-result"]').text()).toContain('invalid header name')
    expect(wrapper.text()).toContain('未发送请求；修改未保存或应用')
    expect(wrapper.text()).toContain('Preview only')
    await wrapper.get('[data-testid="preview-model"]').setValue('luna')
    expect(wrapper.find('[data-testid="preview-result"]').exists()).toBe(false)
  })

  it('locks duplicate submits and ignores a completion after changing accounts', async () => {
    let finish!: (value: CodexTicketPreview) => void
    mocks.preview.mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const wrapper = render()
    await wrapper.get('form').trigger('submit')
    await wrapper.get('form').trigger('submit')
    expect(mocks.preview).toHaveBeenCalledTimes(1)
    expect(wrapper.get<HTMLFieldSetElement>('fieldset').element.disabled).toBe(true)
    await wrapper.setProps({ accountId: 8 })
    finish(previewResult())
    await flushPromises()
    expect(wrapper.find('[data-testid="preview-result"]').exists()).toBe(false)
  })

  it('keeps draft values after an error and renders upstream text inertly', async () => {
    mocks.preview.mockRejectedValueOnce(new Error('Temporarily unavailable')).mockResolvedValueOnce({ ...previewResult(), after_headers: { 'X-Test': ['<img src=x onerror=alert(1)>'] } })
    const wrapper = render()
    await wrapper.get('[data-testid="preview-set-name-0"]').setValue('X-Test')
    await wrapper.get('[data-testid="preview-set-value-0"]').setValue('draft')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('Temporarily unavailable')
    expect(wrapper.get<HTMLTextAreaElement>('[data-testid="preview-set-value-0"]').element.value).toBe('draft')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('<img src=x onerror=alert(1)>')
    expect(wrapper.find('img').exists()).toBe(false)
  })
})

describe('runtime read-only status', () => {
  it.each([
    ['business_active', '业务请求进行中，采票暂停'],
    ['shared_state_unavailable', '共享状态不可用'],
  ])('explains harvest admission reason %s', async (reason, label) => {
    mocks.runtime.mockResolvedValue(runtime({ reason }))
    const wrapper = mount(CodexTicketRuntimePanel, { props: { accountId: 7 } })
    wrappers.push(wrapper)
    await flushPromises()
    expect(wrapper.text()).toContain(label)
    expect(wrapper.text()).not.toContain(reason)
    expect(mocks.preview).not.toHaveBeenCalled()
  })

  it('loads account budgets and wait times and allows retrying a failed read', async () => {
    mocks.runtime.mockRejectedValueOnce(new Error('unavailable')).mockResolvedValueOnce(runtime({ harvest_half_open: true, harvest_accepted: false, rule_matched: true, policy_version: 'v2', silence_until: '2026-09-22T08:05:00Z' }))
    const wrapper = mount(CodexTicketRuntimePanel, { props: { accountId: 7 } })
    wrappers.push(wrapper)
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('状态读取失败')
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('2 / 6')
    expect(wrapper.text()).toContain('业务复验暂缓')
    expect(wrapper.text()).toContain('2026-09-22T08:30:00Z')
    expect(wrapper.text()).toContain('并不保证代理届时恢复')
    expect(wrapper.text()).toContain('本次采集曾为半开是')
    expect(wrapper.text()).toContain('采集候选通过否')
    expect(mocks.preview).not.toHaveBeenCalled()
  })
})
