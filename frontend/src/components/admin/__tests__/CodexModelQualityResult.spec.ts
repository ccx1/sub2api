import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { baseCompile } from '@intlify/message-compiler'
import CodexModelQualityResult from '../CodexModelQualityResult.vue'
import zh from '@/i18n/locales/zh/codexModelQuality'
import type { ModelQualityStatus } from '@/api/admin/codexModelQuality'

// The runtime-only vue-i18n build used in tests cannot compile placeholders.
const compile = (value: unknown): unknown => typeof value === 'string'
  ? new Function(`return ${baseCompile(value, { mode: 'arrow' }).code}`)()
  : Object.fromEntries(Object.entries(value as Record<string, unknown>).map(([key, item]) => [key, compile(item)]))
const messages = { zh: { codexModelQuality: compile(zh) } }

const status = (overrides: Partial<ModelQualityStatus> = {}): ModelQualityStatus => ({
  account_id: 41, model: 'gpt-6-astra', status: 'quarantined', reason: 'capability_failed',
  checked_at: '2026-09-24T00:00:00Z', sample_count: 0, requests: 2, model_identity: 'unknown',
  source: 'automatic', capability_score: 40, ...overrides
})
const render = (item: ModelQualityStatus) => mount(CodexModelQualityResult, {
  props: { item },
  global: { plugins: [createI18n({ legacy: false, locale: 'zh', messages })] }
})

describe('CodexModelQualityResult', () => {
  it('keeps the previous conclusion visible after the ticket was replaced', () => {
    const wrapper = render(status({ ticket_replaced: true, next_check_at: '2026-09-24T00:05:00Z' }))
    expect(wrapper.text()).toContain('当前票据已隔离')
    expect(wrapper.get('[data-testid="quality-ticket-replaced"]').text()).toBe('已换新票，待复测')
    expect(wrapper.text()).toContain('40 / 100')
    wrapper.unmount()
  })

  it('shows the last conclusion of a stale result', () => {
    const wrapper = render(status({ status: 'stale', reason: 'stale', previous_status: 'passed' }))
    expect(wrapper.get('[data-testid="quality-previous-status"]').text()).toBe('上次结论：行为验证通过')
    wrapper.unmount()
  })

  it('lists recent checks with result, reason and source', () => {
    const wrapper = render(status({
      history: [
        { status: 'quarantined', reason: 'capability_failed', source: 'automatic', checked_at: '2026-09-24T00:00:00Z', capability_score: 40, duration_ms: 5200 },
        { status: 'inconclusive', reason: 'upstream_rate_limited', source: 'diagnostic', checked_at: '2026-09-23T23:00:00Z' }
      ]
    }))
    expect(wrapper.get('[data-testid="quality-history"] summary').text()).toBe('最近检测记录（2 次）')
    const rows = wrapper.findAll('[data-testid="quality-history-entry"]')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('当前票据已隔离')
    expect(rows[0].text()).toContain('5.2 s')
    expect(rows[1].text()).toContain('未完成')
    expect(rows[1].text()).toContain('上游限流')
    expect(rows[1].text()).toContain('管理员诊断')
    wrapper.unmount()
  })

  it('omits the history section when no check has finished', () => {
    const wrapper = render(status({ status: 'pending', reason: 'not_checked' }))
    expect(wrapper.find('[data-testid="quality-history"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
