import { afterEach, describe, expect, it, vi } from 'vitest'
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
    .filter(([key, value]) => /^(codexTicketPool|codexTicketPrimary|codexTicketUsing|codexTurnTicket|codexTicketQuality|codexTicketHistory|codexTicketHarvest)/.test(key) && typeof value === 'string')
    .map(([key, value]) => [key, new Function(`return ${baseCompile(value as string, { mode: 'arrow' }).code}`)()])) } }
})
const messages = { zh: compileMessages(zhAccounts.accounts.openai), en: compileMessages(enAccounts.accounts.openai) }
const render = (tickets?: TicketStatus[] | null, locale = 'zh') => mount(CodexTicketPoolStatus, {
  props: { tickets },
  global: { plugins: [createI18n({ legacy: false, locale, messages })] }
})

describe('CodexTicketPoolStatus', () => {
  it.each([
    ['zh', '历史票首次采集：2026-10-03 04:05:06', '历史票有效期剩余：倒计时 170:00:00', '最近打票：成功'],
    ['en', 'History ticket first captured: 2026-10-03 04:05:06', 'History ticket validity left: Countdown 170:00:00', 'Last harvest: Succeeded']
  ])('shows historical ticket time, long validity and actual harvest result (%s)', (locale, captureText, remainingText, harvestText) => {
    const captured = new Date(2026, 9, 3, 4, 5, 6).toISOString()
    const expires = new Date(Date.now() + (7 * 86400 + 2 * 3600) * 1000).toISOString()
    const wrapper = render([ticket({
      usage_mode: 'aged', origin_captured_at: captured, expires_at: expires,
      historical_used_at: new Date(Date.now() - 10 * 1000).toISOString(),
      route_host: 'chat.gateway.unified-88.api.openai.com',
      remaining_seconds: 7 * 86400 + 2 * 3600, primary_remaining_seconds: 7 * 86400 + 2 * 3600,
      last_attempt_at: captured, last_attempt_success: true
    })], locale)
    expect(wrapper.get('[data-testid="history-ticket-status"]').text()).toContain(captureText)
    expect(wrapper.get('[data-testid="history-ticket-status"]').text()).toContain(remainingText)
    expect(wrapper.get('[data-testid="history-ticket-host"]').text()).toContain('chat.gateway.unified-88.api.openai.com')
    expect(wrapper.get('[data-testid="primary-status"]').text()).toContain('170:00:00')
    expect(wrapper.get('[data-testid="ticket-harvest-status"]').text()).toContain(harvestText)
    expect(wrapper.get('[data-testid="ticket-harvest-status"] time').attributes('datetime')).toBe(captured)
    wrapper.unmount()
  })

  it('decrements the historical countdown every second', async () => {
    vi.useFakeTimers()
    const captured = new Date('2026-10-03T04:05:06Z')
    vi.setSystemTime(captured)
    const expires = new Date(captured.getTime() + 3665 * 1000).toISOString()
    const wrapper = render([ticket({ usage_mode: 'aged', origin_captured_at: captured.toISOString(), historical_used_at: captured.toISOString(), expires_at: expires, remaining_seconds: 3665 })])
    expect(wrapper.get('[data-testid="history-ticket-status"]').text()).toContain('倒计时 01:01:05')
    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper.get('[data-testid="history-ticket-status"]').text()).toContain('倒计时 01:01:03')
    wrapper.unmount()
  })

  it('shows unknown historical details without a selected ticket and never infers harvest health from enablement', () => {
    const wrapper = render([ticket({ usage_mode: 'aged', ready: false, remaining_seconds: 0,
      origin_captured_at: undefined, expires_at: undefined, last_attempt_at: undefined, last_attempt_success: undefined })])
    expect(wrapper.get('[data-testid="history-ticket-status"]').text()).toContain('历史票首次采集：未知')
    expect(wrapper.get('[data-testid="history-ticket-status"]').text()).toContain('历史票有效期剩余：尚未使用')
    expect(wrapper.get('[data-testid="ticket-harvest-status"]').text()).toContain('最近打票：暂无记录')
    wrapper.unmount()
  })

  it('shows expired history and the last failed harvest independently', () => {
    const usedAt = new Date(Date.now() - 10 * 1000).toISOString()
    const wrapper = render([ticket({ usage_mode: 'aged', historical_used_at: usedAt, expires_at: new Date(Date.now() - 1000).toISOString(),
      remaining_seconds: 0, last_attempt_at: new Date(2026, 9, 3, 4, 5, 6).toISOString(),
      last_attempt_success: false, last_attempt_reason: 'upstream timeout' })])
    expect(wrapper.get('[data-testid="history-ticket-status"]').text()).toContain('已过期')
    expect(wrapper.get('[data-testid="ticket-harvest-status"]').text()).toContain('最近打票：失败')
    expect(wrapper.get('[data-testid="ticket-harvest-status"] span').attributes('title')).toBe('upstream timeout')
    wrapper.unmount()
  })

  it.each(['revoked', 'expired', 'credential'])('keeps an unavailable historical primary reason when the ticket was not used (%s)', primary_reason => {
    const wrapper = render([ticket({ usage_mode: 'aged', historical_used_at: undefined, primary_ready: false, primary_reason, ready: false })])
    expect(wrapper.get('[data-testid="primary-status"]').text()).not.toContain('尚未使用')
    wrapper.unmount()
  })

  it('hides history details in immediate mode while retaining the most recent harvest outcome', () => {
    const wrapper = render([ticket({ usage_mode: 'immediate', last_attempt_at: 'bad date', last_attempt_success: true })])
    expect(wrapper.find('[data-testid="history-ticket-status"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="ticket-harvest-status"]').text()).toContain('最近打票：暂无记录')
    expect(wrapper.find('[data-testid="ticket-harvest-status"] time').exists()).toBe(false)
    wrapper.unmount()
  })

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

  it('labels a cooled primary ticket', () => {
    const wrapper = render([ticket({ ready: true, primary_ready: false, primary_reason: 'route_cooldown', using_standby: true })])
    expect(wrapper.get('[data-testid="primary-status"]').text()).toContain('主票同路由冷却中')
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

  it.each([
    ['inconclusive', '模型质量检测：未完成'],
    ['stale', '模型质量检测：结果已过期'],
    ['pending', '模型质量检测：排队中'],
    ['running', '模型质量检测：检测中']
  ] as const)('distinguishes unfinished quality states (%s)', (quality_status, label) => {
    const wrapper = render([ticket({ quality_status })])
    const text = wrapper.get('[data-testid="ticket-quality-status"]').text()
    expect(text).toContain(label)
    expect(text).not.toContain('待确认')
    wrapper.unmount()
  })

  it('shows when a queued check is expected to start', () => {
    const next = new Date(Date.now() + 4 * 60000 + 10000).toISOString()
    const wrapper = render([ticket({ quality_status: 'pending', quality_next_check_at: next })])
    expect(wrapper.get('[data-testid="ticket-quality-status"]').text()).toContain('排队中，约 5 分钟后检测')
    wrapper.unmount()
  })

  it('keeps the previous conclusion and marks a replaced ticket', () => {
    const wrapper = render([ticket({ quality_status: 'quarantined', quality_reason: 'capability_failed', quality_ticket_replaced: true })])
    const text = wrapper.get('[data-testid="ticket-quality-status"]').text()
    expect(text).toContain('模型质量检测：不通过')
    expect(wrapper.get('[data-testid="ticket-quality-replaced"]').text()).toContain('已换新票，待复测')
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

  it.each([undefined, 'off'] as const)('hides route affinity and node for legacy or disabled settings (%s)', route_affinity_status => {
    const wrapper = render([ticket({ route_affinity_status, route_affinity_connections: 0, route_node: 'unified-39' })])
    expect(wrapper.find('[data-testid="ticket-route-affinity"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="ticket-route-node"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it.each([
    ['zh', 'DE', false, '采集出口 DE'],
    ['zh', 'US', true, '采集出口 US'],
    ['en', 'GB', false, 'Harvest egress GB'],
    ['en', 'JP', true, 'Harvest egress JP'],
  ] as const)('shows the compute node and flags only cross macro-region routes (%s, %s)', (locale, route_egress_country, route_cross_region, egress) => {
    const wrapper = render([ticket({ route_affinity_status: 'available', route_node: 'unified-39', route_node_country: 'ES', route_node_region: 'Madrid',
      route_macro_region: 'EU', route_egress_country, route_cross_region })], locale)
    const row = wrapper.get('[data-testid="ticket-route-node"]')
    expect(row.text()).toContain('unified-39')
    expect(row.text()).toContain('ES Madrid · EU')
    expect(row.get('[data-testid="route-egress-country"]').text()).toBe(egress)
    expect(row.find('[data-testid="route-cross-region"]').exists()).toBe(route_cross_region)
    wrapper.unmount()
  })

  it('hides the compute node row when __oailb carries no node', () => {
    const wrapper = render([ticket({ route_affinity_status: 'available' })])
    expect(wrapper.find('[data-testid="ticket-route-node"]').exists()).toBe(false)
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

afterEach(() => {
  vi.useRealTimers()
})
