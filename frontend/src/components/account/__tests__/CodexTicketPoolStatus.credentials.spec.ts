import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { baseCompile } from '@intlify/message-compiler'
import CodexTicketPoolStatus from '../CodexTicketPoolStatus.vue'
import zhAccounts from '@/i18n/locales/zh/admin/accounts'
import enAccounts from '@/i18n/locales/en/admin/accounts'
import type { Account } from '@/types'

type TicketStatus = NonNullable<Account['codex_turn_tickets']>[number]
const compileMessages = (messages: Record<string, unknown>) => ({
  admin: { accounts: { openai: Object.fromEntries(Object.entries(messages)
    .filter(([key, value]) => /^(codexTicketPool|codexTicketPrimary|codexTicketUsing|codexTurnTicket)/.test(key) && typeof value === 'string')
    .map(([key, value]) => [key, new Function(`return ${baseCompile(value as string, { mode: 'arrow' }).code}`)()])) } }
})
const messages = { zh: compileMessages(zhAccounts.accounts.openai), en: compileMessages(enAccounts.accounts.openai) }
const render = (overrides: Partial<TicketStatus> = {}, locale = 'zh') => mount(CodexTicketPoolStatus, {
  props: { tickets: [{ model: 'gpt-6-astra', ready: true, remaining_seconds: 1800, blocked: false,
    primary_present: true, primary_ready: true, available_count: 2, capacity: 5, ...overrides }] },
  global: { plugins: [createI18n({ legacy: false, locale, messages })] }
})

describe('CodexTicketPoolStatus credential state', () => {
  it.each([
    ['zh', '需复验 · 仍可用', '可用 2/5 张', '复验时间'],
    ['en', 'Revalidation due · Usable', '2/5 available', 'Revalidate at']
  ])('keeps soft-expired tickets in usable inventory (%s)', (locale, label, available, title) => {
    const wrapper = render({ credential_state: 'revalidation_required', revalidation_required: true,
      revalidate_at: '2026-09-22T12:00:00Z' }, locale)
    expect(wrapper.get('[data-testid="credential-status"]').text()).toBe(label)
    expect(wrapper.get('[data-testid="credential-status"]').attributes('title')).toContain(title)
    expect(wrapper.get('[data-testid="pool-available"]').text()).toBe(available)
    expect(wrapper.find('[data-testid="primary-status"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it.each([
    ['available', '凭据可用'], ['expired', '凭据过期'], ['revoked', '凭据撤销'], ['missing', '暂无凭据']
  ] as const)('shows explicit %s state independently of blocked', (credential_state, label) => {
    const wrapper = render({ credential_state, blocked: false })
    expect(wrapper.get('[data-testid="credential-status"]').text()).toBe(label)
    wrapper.unmount()
  })

  it('preserves legacy API rendering and rejects unknown state or invalid date', async () => {
    const wrapper = render()
    expect(wrapper.find('[data-testid="credential-status"]').exists()).toBe(false)
    await wrapper.setProps({ tickets: [{ model: 'custom', ready: true, remaining_seconds: 60, blocked: false,
      credential_state: 'available', revalidate_at: 'invalid' }] })
    expect(wrapper.get('[data-testid="credential-status"]').attributes('title')).toBeUndefined()
    await wrapper.setProps({ tickets: [{ model: 'custom', ready: false, remaining_seconds: 0, blocked: true,
      credential_state: 'unknown' as TicketStatus['credential_state'] }] })
    expect(wrapper.find('[data-testid="credential-status"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
