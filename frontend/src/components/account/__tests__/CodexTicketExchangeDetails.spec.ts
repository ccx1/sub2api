import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CodexTicketExchangeDetails from '../CodexTicketExchangeDetails.vue'
import accounts from '@/i18n/locales/zh/admin/accounts'
import type { CodexTicketHistoryAttempt } from '@/api/admin/accounts'

const { copyToClipboard } = vi.hoisted(() => ({ copyToClipboard: vi.fn() }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard }) }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: translate }) }))

function translate(key: string, params: Record<string, unknown> = {}): string {
  const message = key.split('.').reduce<unknown>((value, part) => (value as Record<string, unknown> | undefined)?.[part], { admin: accounts })
  return typeof message === 'string' ? message.replace(/\{(\w+)\}/g, (_, name: string) => String(params[name] ?? '')) : key
}

function attempt(overrides: Partial<CodexTicketHistoryAttempt> = {}): CodexTicketHistoryAttempt {
  return {
    id: 'attempt-1', started_at: '2026-09-21T08:00:00Z', finished_at: '2026-09-21T08:00:01Z',
    model: 'requested-model', success: false, reason: 'harvest_model_mismatch',
    harvest_exchange: {
      requested_model: 'requested-model', reported_models: ['actual-model'],
      request: { method: 'POST', url: 'https://example.test/responses', headers: { Authorization: ['[REDACTED]'] }, body: '{"model":"requested-model"}', body_bytes: 27 },
      response: { status_code: 200, headers: { 'Content-Type': ['text/event-stream'] }, body: 'data: {"model":"actual-model"}', body_bytes: 30 }
    },
    ...overrides
  }
}

const wrappers: Array<{ unmount: () => void }> = []
function mountDetails(value = attempt()) {
  const wrapper = mount(CodexTicketExchangeDetails, { props: { attempt: value } })
  wrappers.push(wrapper)
  return wrapper
}

beforeEach(() => { copyToClipboard.mockReset(); copyToClipboard.mockResolvedValue(true) })
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()) })

describe('CodexTicketExchangeDetails', () => {
  it('displays and copies raw bodies and header values unchanged, while keeping HTML inert', async () => {
    const rawBody = '  {"ticket":"test-ticket-value","html":"<img src=x onerror=alert(1)>"}\r\n\t'
    const data = attempt()
    Object.assign(data.harvest_exchange!, { capture_mode: 'raw' })
    Object.assign(data.harvest_exchange!.request!, {
      headers: { Authorization: ['Bearer test-credential'], Cookie: ['test-session=sample'], 'X-Ticket': ['test-ticket-value'] },
      body: rawBody
    })
    const wrapper = mountDetails(data)
    const request = wrapper.get('[data-testid="harvest-request"]')
    expect(request.text()).toContain('原始请求')
    expect(request.text()).toContain('Bearer test-credential')
    expect(request.findAll('pre')[1]!.element.textContent).toBe(rawBody)
    expect(wrapper.find('img').exists()).toBe(false)
    await request.get('button').trigger('click')
    await flushPromises()
    const copied = copyToClipboard.mock.calls[0]![0] as string
    expect(copied).toBe('POST https://example.test/responses\nAuthorization: Bearer test-credential\nCookie: test-session=sample\nX-Ticket: test-ticket-value\n\n' + rawBody)
    expect(wrapper.get('[role="status"]').text()).toBe('已复制原始报文')
    expect(wrapper.get('[data-testid="harvest-exchange"]').text()).not.toContain('已脱敏')
  })

  it('treats missing capture_mode as old redacted data and supports mixed-stage capture modes', () => {
    const data = attempt({ business_exchange: { capture_mode: 'raw', requested_model: 'requested-model', response: { status_code: 200, body: 'test-ticket-value', body_bytes: 17 } } })
    const wrapper = mountDetails(data)
    expect(wrapper.get('[data-testid="harvest-request"]').text()).toContain('脱敏请求（旧记录）')
    expect(wrapper.get('[data-testid="business-response"]').text()).toContain('原始响应')
    expect(wrapper.get('[data-testid="business-exchange"]').text()).not.toContain('已脱敏')
  })

  it('explains unavailable messages for legacy attempts without inventing observed models', () => {
    const wrapper = mountDetails(attempt({ harvest_exchange: undefined }))
    expect(wrapper.findAll('[data-testid="exchange-unavailable"]')).toHaveLength(2)
    expect(wrapper.text()).toContain('旧记录无法补回')
    expect(wrapper.text()).toContain('未记录/阶段未开始')
    expect(wrapper.find('button').exists()).toBe(false)
  })

  it('preserves model summaries when retained request and response details have been cleared', () => {
    const wrapper = mountDetails(attempt({ harvest_exchange: { requested_model: 'requested-model', reported_models: ['actual-model', 'actual-model-dated'] } }))
    const harvest = wrapper.get('[data-testid="harvest-exchange"]')
    expect(harvest.text()).toContain('actual-model, actual-model-dated')
    expect(harvest.text()).toContain('超出保留范围的详情已清理')
    expect(harvest.find('button').exists()).toBe(false)
  })

  it('keeps harvest and business messages distinct and displays HTTP response evidence', () => {
    const wrapper = mountDetails(attempt({
      business_exchange: { requested_model: 'requested-model', reported_models: ['business-model'], request: { body_bytes: 0 }, response: { status_code: 403, body: 'denied', body_bytes: 6 } }
    }))
    const harvest = wrapper.get('[data-testid="harvest-exchange"]')
    const business = wrapper.get('[data-testid="business-exchange"]')
    expect(harvest.text()).toContain('actual-model')
    expect(harvest.text()).toContain('HTTP 200')
    expect(business.text()).toContain('business-model')
    expect(business.text()).toContain('HTTP 403')
    expect(business.text()).not.toContain('actual-model')
    expect(wrapper.text()).toContain('认证信息、Cookie 和完整票据不会显示')
  })

  it('renders upstream HTML and long bodies as wrapping plain text', () => {
    const payload = '<img src=x onerror="alert(1)"><script>bad()</script>' + 'x'.repeat(2000)
    const data = attempt()
    data.harvest_exchange!.response!.body = payload
    data.harvest_exchange!.response!.headers = { 'X-Debug': [payload] }
    const wrapper = mountDetails(data)
    const response = wrapper.get('[data-testid="harvest-response"]')
    expect(response.text()).toContain(payload)
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.find('script').exists()).toBe(false)
    for (const pre of wrapper.findAll('pre')) {
      expect(pre.classes()).toContain('whitespace-pre-wrap')
      expect(pre.classes()).toContain('break-all')
    }
  })

  it('distinguishes a stage without a response from a response without model metadata', () => {
    const data = attempt({ business_exchange: { requested_model: 'requested-model', request: { body_bytes: 0 } } })
    data.harvest_exchange!.reported_models = []
    const wrapper = mountDetails(data)
    expect(wrapper.get('[data-testid="harvest-exchange"]').text()).toContain('响应未报告模型')
    expect(wrapper.get('[data-testid="business-exchange"]').text()).toContain('未收到响应')
    expect(wrapper.get('[data-testid="business-response"]').find('button').exists()).toBe(false)
  })

  it('marks omitted bodies, header truncation and model truncation without claiming the response was empty', () => {
    const data = attempt()
    Object.assign(data.harvest_exchange!, { models_truncated: true })
    Object.assign(data.harvest_exchange!.response!, { body: '', body_bytes: 22000, body_truncated: true, headers_truncated: true })
    const wrapper = mountDetails(data)
    const response = wrapper.get('[data-testid="harvest-response"]')
    expect(response.text()).toContain('原始正文：22000 字节')
    expect(response.text()).toContain('正文未完整保存或已省略')
    expect(response.text()).toContain('报文头已截断')
    expect(response.text()).not.toContain('正文为空')
    expect(wrapper.text()).toContain('模型列表已截断')
  })

  it('copies the displayed redacted HTTP message with headers, body and omission notices', async () => {
    const data = attempt()
    data.harvest_exchange!.request!.headers_truncated = true
    const wrapper = mountDetails(data)
    await wrapper.get('[data-testid="harvest-request"] button').trigger('click')
    await flushPromises()
    expect(copyToClipboard).toHaveBeenCalledTimes(1)
    const copied = copyToClipboard.mock.calls[0]![0] as string
    expect(copied).toContain('POST https://example.test/responses')
    expect(copied).toContain('Authorization: [REDACTED]')
    expect(copied).toContain('{"model":"requested-model"}')
    expect(copied).toContain('报文头已截断')
    expect(copied).not.toContain('actual-model')
    expect(wrapper.get('[role="status"]').text()).toBe('已复制脱敏报文')
  })

  it.each(['false', 'throws'])('reports clipboard failure when copying %s', async outcome => {
    if (outcome === 'false') copyToClipboard.mockResolvedValue(false)
    else copyToClipboard.mockRejectedValue(new Error('denied'))
    const wrapper = mountDetails()
    await wrapper.get('[data-testid="harvest-response"] button').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('复制失败')
    expect(wrapper.get('[data-testid="harvest-response"] button').attributes('disabled')).toBeUndefined()
  })
})
