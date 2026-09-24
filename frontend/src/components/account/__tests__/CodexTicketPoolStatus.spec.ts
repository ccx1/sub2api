import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { baseCompile } from '@intlify/message-compiler'
import CodexTicketPoolStatus from '../CodexTicketPoolStatus.vue'
import zhAccounts from '@/i18n/locales/zh/admin/accounts'
import enAccounts from '@/i18n/locales/en/admin/accounts'
import type { Account } from '@/types'

type TicketStatus = NonNullable<Account['codex_turn_tickets']>[number]
const ticket = (overrides: Partial<TicketStatus> = {}): TicketStatus => ({
  model: 'gpt-6-astra', ready: true, remaining_seconds: 1800, blocked: false,
  primary_present: true, primary_ready: true, primary_remaining_seconds: 1800,
  available_count: 3, capacity: 5, reserve_count: 2, expiring_count: 1,
  next_expires_at: '2026-09-22T12:00:00Z', ...overrides
})
const compileMessages = (messages: Record<string, unknown>) => ({
  admin: { accounts: { openai: Object.fromEntries(Object.entries(messages)
    .filter(([key, value]) => /^(codexTicketPool|codexTicketPrimary|codexTicketUsing|codexTurnTicket|codexTicketQuality)/.test(key) && typeof value === 'string')
    .map(([key, value]) => [key, new Function(`return ${baseCompile(value as string, { mode: 'arrow' }).code}`)()])) } }
})
const messages = { zh: compileMessages(zhAccounts.accounts.openai), en: compileMessages(enAccounts.accounts.openai) }
const render = (tickets?: TicketStatus[] | null, locale = 'zh') => mount(CodexTicketPoolStatus, {
  props: { tickets },
  global: { plugins: [createI18n({ legacy: false, locale, messages })] }
})

describe('CodexTicketPoolStatus', () => {
  it.each([
    ['zh', '可用 5 张 · 目标容量 2 张', '备用 4 张'],
    ['en', '5 available · Target capacity 2', '4 in reserve']
  ])('preserves actual inventory when capacity was reduced (%s)', (locale, available, reserve) => {
    const wrapper = render([ticket({ available_count: 5, capacity: 2, reserve_count: 4 })], locale)
    expect(wrapper.get('[data-testid="pool-available"]').text()).toBe(available)
    expect(wrapper.get('[data-testid="pool-reserve"]').text()).toBe(reserve)
    expect(wrapper.text()).not.toContain('5/2')
    wrapper.unmount()
  })

  it('shows reusable inventory and nearest expiry separately for each model', () => {
    const wrapper = render([ticket(), ticket({ model: 'gpt-5.6-sol', available_count: 5, reserve_count: 4, expiring_count: 0 })])
    const rows = wrapper.findAll('[data-testid="ticket-model"]')
    expect(rows[0].text()).toContain('astra')
    expect(rows[0].get('[data-testid="pool-available"]').text()).toBe('可用 3/5 张')
    expect(rows[0].get('[data-testid="pool-reserve"]').text()).toBe('备用 2 张')
    expect(rows[0].get('[data-testid="pool-expiring"]').text()).toBe('1 张即将到期')
    expect(rows[0].get('[data-testid="pool-expiry"]').text()).toContain('最早到期')
    expect(rows[1].text()).toContain('可用 5/5 张')
    expect(rows[1].find('[data-testid="pool-expiring"]').exists()).toBe(false)
    expect(wrapper.get('[title*="剩余调用次数"]').exists()).toBe(true)
    expect(rows[0].get('[data-testid="primary-status"]').text()).toContain('主票')
    wrapper.unmount()
  })

  it.each([false, true])('shows zero inventory with the correct scheduling state (blocked=%s)', blocked => {
    const wrapper = render([ticket({ ready: false, primary_ready: false, primary_reason: 'revoked', available_count: 0, reserve_count: 0, expiring_count: 0, blocked })])
    expect(wrapper.text()).toContain('可用 0/5 张')
    expect(wrapper.get('[data-testid="primary-status"]').text()).toContain('主票已撤销')
    expect(wrapper.text()).toContain(blocked ? '无票暂停' : '无可用票')
    expect(wrapper.find('[data-testid="pool-reserve"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="pool-expiry"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows the active standby and primary state on one row', () => {
    const wrapper = render([ticket({
      ready: true, remaining_seconds: 720, using_standby: true,
      primary_ready: false, primary_reason: 'expired', available_count: 2, reserve_count: 1,
    })])
    expect(wrapper.get('[data-testid="current-status"]').text()).toContain('备用 12m00s')
    expect(wrapper.get('[data-testid="primary-status"]').text()).toContain('主票已过期')
    expect(wrapper.get('[data-testid="pool-available"]').text()).toContain('可用 2/5 张')
    wrapper.unmount()
  })

  it.each([
    ['passed', '模型质量检测：通过', false, false],
    ['quarantined', '模型质量检测：不通过', false, false],
    ['quarantined', '模型质量检测：不通过', true, true],
    ['suspect', '模型质量检测：不通过', false, false]
  ] as const)('shows model quality below each ticket (%s, paused=%s)', (quality_status, label, quality_paused, paused) => {
    const wrapper = render([ticket({ quality_status, quality_paused })])
    expect(wrapper.get('[data-testid="ticket-quality-status"]').text()).toContain(label)
    if (paused) expect(wrapper.get('[data-testid="ticket-quality-status"]').text()).toContain('模型已暂停')
    else expect(wrapper.get('[data-testid="ticket-quality-status"]').text()).not.toContain('模型已暂停')
    wrapper.unmount()
  })

  it.each([undefined, null, -1, 1.5, NaN, Infinity])('does not invent inventory for an invalid or absent count %s', available_count => {
    const wrapper = render([{ model: 'gpt-6-astra', ready: true, blocked: false, remaining_seconds: 2520, using_standby: false, standby_ready: true, available_count } as TicketStatus])
    expect(wrapper.text()).toContain('主票')
    expect(wrapper.text()).toContain('42m00s')
    expect(wrapper.text()).toContain('备用就绪')
    expect(wrapper.find('[data-testid="pool-available"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="pool-reserve"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it.each([
    ['zh', 'available', '路由亲和：可用', '当前 4 条连接', 'text-emerald-600'],
    ['zh', 'unavailable', '路由亲和：不可用', '当前 0 条连接', 'text-amber-600'],
    ['zh', 'unknown', '路由亲和：待探测', '当前 0 条连接', 'text-gray-500'],
    ['en', 'available', 'Route affinity: available', 'Current connections: 4', 'text-emerald-600'],
    ['en', 'unavailable', 'Route affinity: unavailable', 'Current connections: 0', 'text-amber-600'],
    ['en', 'unknown', 'Route affinity: not yet checked', 'Current connections: 0', 'text-gray-500'],
  ] as const)('shows route affinity independently of ticket quality (%s, %s)', (locale, route_affinity_status, label, connections, color) => {
    const route_affinity_connections = route_affinity_status === 'available' ? 4 : 0
    const wrapper = render([ticket({ route_affinity_status, route_affinity_connections, quality_status: 'pending' })], locale)
    const row = wrapper.get('[data-testid="ticket-route-affinity"]')
    expect(row.text()).toContain(label)
    expect(row.classes()).toContain(color)
    expect(row.get('[data-testid="route-affinity-connections"]').text()).toBe(connections)
    expect(wrapper.get('[data-testid="ticket-quality-status"]').classes()).not.toContain('text-emerald-600')
    if (route_affinity_status !== 'available') expect(row.classes()).not.toContain('text-emerald-600')
    wrapper.unmount()
  })

  it.each([undefined, 'off'] as const)('hides route affinity for legacy or disabled settings (%s)', route_affinity_status => {
    const wrapper = render([ticket({ route_affinity_status, route_affinity_connections: 0 })])
    expect(wrapper.find('[data-testid="ticket-route-affinity"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('updates route availability and preserves a zero connection count', async () => {
    const wrapper = render([ticket({ route_affinity_status: 'available', route_affinity_connections: 4 })])
    await wrapper.setProps({ tickets: [ticket({ route_affinity_status: 'unavailable', route_affinity_connections: 0 })] })
    expect(wrapper.get('[data-testid="ticket-route-affinity"]').text()).toContain('路由亲和：不可用')
    expect(wrapper.get('[data-testid="route-affinity-connections"]').text()).toBe('当前 0 条连接')
    wrapper.unmount()
  })

  it('handles partial counts without guessing capacity or reserves and hides invalid timestamps', () => {
    const wrapper = render([ticket({ capacity: undefined, reserve_count: undefined, expiring_count: undefined, next_expires_at: 'bad timestamp' })])
    expect(wrapper.get('[data-testid="pool-available"]').text()).toBe('可用 3 张')
    expect(wrapper.find('[data-testid="pool-reserve"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="pool-expiring"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="pool-expiry"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('updates quantities after replacement and keeps the full custom model available as a title', async () => {
    const model = 'custom-' + 'long-model-name-'.repeat(8)
    const wrapper = render([ticket({ model })])
    expect(wrapper.get(`[title="${model}"]`).text()).toBe(model)
    await wrapper.setProps({ tickets: [ticket({ model, available_count: 2, reserve_count: 1, expiring_count: 0 })] })
    expect(wrapper.text()).toContain('可用 2/5 张')
    expect(wrapper.text()).toContain('备用 1 张')
    expect(wrapper.text()).not.toContain('可用 3/5 张')
    wrapper.unmount()
  })

  it('localizes inventory in English', () => {
    const wrapper = render([ticket()], 'en')
    expect(wrapper.text()).toContain('3/5 available')
    expect(wrapper.text()).toContain('2 in reserve')
    expect(wrapper.text()).toContain('1 expiring soon')
    expect(wrapper.text()).toContain('Next expiry')
    expect(wrapper.get('[data-testid="primary-status"]').text()).toContain('Primary')
    wrapper.unmount()
  })

  it.each([undefined, null, []])('hides the pool when no model information was supplied: %s', tickets => {
    const wrapper = render(tickets)
    expect(wrapper.find('[data-testid="codex-ticket-pool"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
