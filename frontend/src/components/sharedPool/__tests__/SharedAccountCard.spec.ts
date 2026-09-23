import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import SharedAccountCard from '../SharedAccountCard.vue'
import Toggle from '@/components/common/Toggle.vue'
import SharedAccountUsage from '../SharedAccountUsage.vue'
import type { SharedAccount } from '@/api/sharedPool'

vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({
  t: (key: string, params?: Record<string, unknown>) => params ? `${key}: ${Object.values(params).join(', ')}` : key,
  te: () => true
}) }))

const account: SharedAccount = {
  id: 7, name: '共享账号', platform: 'openai', type: 'oauth', concurrency: 2,
  enabled: true, dispatch_consent: true, settlement_multiplier: 0.5, protection_enabled: true, codex_ticket_enabled: true, admin_disabled: false, status: 'active', error_message: null,
  proxy_mode: 'random', has_custom_proxy: false, group_ids: [1], groups: [{ id: 1, name: '共享 A 组' }],
  today_earnings: 1.2345, total_earnings: 56.789, estimated_earnings: 8.5,
  platform_rate_bps: 500, proxy_rate_bps: 100, last_used_at: null, created_at: '2026-09-20T00:00:00Z'
}
function render(busy = false) {
  return mount(SharedAccountCard, { props: { account, busy }, global: { stubs: { SharedAccountUsage: true } } })
}

describe('shared account card controls', () => {
  it('hides assigned group names and assignment wording from the owner while preserving administrator access', async () => {
    const wrapper = render()
    expect(wrapper.find('[data-test="account-groups"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('共享 A 组')
    await wrapper.setProps({ account: { ...account, group_ids: [], groups: [] } })
    expect(wrapper.text()).not.toContain('sharedPool.noGroup')
    expect(wrapper.text()).not.toContain('sharedPool.waitingDispatch')
    expect(wrapper.text()).toContain('sharedPool.status.active')
    await wrapper.setProps({ account, admin: true })
    expect(wrapper.get('[data-test="account-groups"]').text()).toContain('共享 A 组')
  })

  it.each(['active', 'error', 'unavailable'])('shows the owner account status %s when group data is redacted', async status => {
    const wrapper = render()
    await wrapper.setProps({ account: { ...account, group_ids: [], groups: [], status } })
    expect(wrapper.text()).toContain(`sharedPool.status.${status}`)
    expect(wrapper.text()).not.toContain('sharedPool.waiting')
    expect(wrapper.find('[data-test="account-groups"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('retains administrative waiting status when no assignment exists', async () => {
    const wrapper = render()
    await wrapper.setProps({ admin: true, account: { ...account, group_ids: [], groups: [] } })
    expect(wrapper.text()).toContain('sharedPool.waiting')
    wrapper.unmount()
  })
  it('keeps long administrator group lists compact and exposes all names on hover', async () => {
    const groups = [1, 2, 3, 4, 5].map(id => ({ id, name: `共享 ${id} 组` }))
    const wrapper = render()
    await wrapper.setProps({ admin: true, account: { ...account, group_ids: groups.map(group => group.id), groups } })
    const list = wrapper.get('[data-test="account-groups"]')
    expect(list.text()).toContain('共享 1 组')
    expect(list.text()).toContain('共享 3 组')
    expect(list.text()).not.toContain('共享 4 组')
    const more = wrapper.get('[data-test="account-groups-more"]')
    expect(more.text()).toBe('+2')
    expect(more.attributes('title')).toContain('共享 5 组')
    expect(more.attributes('aria-label')).toContain('共享 4 组')
    wrapper.unmount()
  })
  it('removes scheduling switches while preserving explicit legacy dispatch authorization', async () => {
    const wrapper = render()
    expect(wrapper.get('[data-test="account-settlement"]').text()).toContain('0.5x')
    expect(wrapper.find('[data-test="authorize-dispatch"]').exists()).toBe(false)
    expect(wrapper.find('[role="switch"][aria-label="sharedPool.sharing"]').exists()).toBe(false)
    await wrapper.setProps({ account: { ...account, dispatch_consent: false, settlement_multiplier: null } })
    expect(wrapper.get('[data-test="account-settlement"]').text()).toContain('sharedPool.legacySettlement')
    expect(wrapper.find('[role="switch"][aria-label="sharedPool.legacySharing"]').exists()).toBe(false)
    expect(wrapper.emitted('enable')).toBeUndefined()
    expect(wrapper.emitted('authorize')).toBeUndefined()
    await wrapper.get('[data-test="authorize-dispatch"]').trigger('click')
    expect(wrapper.emitted('authorize')).toHaveLength(1)
  })
  it('shows usage windows only on the owner OAuth card', async () => {
    const wrapper = render()
    expect(wrapper.findComponent(SharedAccountUsage).exists()).toBe(true)
    await wrapper.setProps({ account: { ...account, type: 'apikey' } })
    expect(wrapper.findComponent(SharedAccountUsage).exists()).toBe(false)
    await wrapper.setProps({ account, admin: true })
    expect(wrapper.findComponent(SharedAccountUsage).exists()).toBe(false)
  })

  it('shows the account subscription tier next to its effective multiplier', async () => {
    const wrapper = render()
    await wrapper.setProps({ account: { ...account, subscription_tier: 'pro', settlement_multiplier: 1.5 } })
    expect(wrapper.text()).toContain('Pro 20x')
    expect(wrapper.get('[data-test="account-settlement"]').text()).toContain('1.5x')
  })
  it('emits switch requests and waits for the parent to update the account', async () => {
    const wrapper = render()
    const protection = wrapper.get('[role="switch"][aria-label="sharedPool.protection"]')
    const ticket = wrapper.get('[role="switch"][aria-label="sharedPool.codexTicket"]')
    await protection.trigger('click')
    await ticket.trigger('click')
    expect(wrapper.emitted('protection')).toEqual([[false]])
    expect(wrapper.emitted('codexTicket')).toEqual([[false]])
    expect(ticket.attributes('aria-checked')).toBe('true')
    expect(protection.attributes('aria-checked')).toBe('true')
    await wrapper.setProps({ account: { ...account, enabled: false, protection_enabled: false, codex_ticket_enabled: false } })
    expect(protection.attributes('aria-checked')).toBe('false')
    expect(ticket.attributes('aria-checked')).toBe('false')
  })

  it('shows the ticket switch only for supported owner accounts with returned state', async () => {
    const wrapper = render()
    for (const patch of [{ type: 'apikey' as const }, { platform: 'gemini' as const }, { codex_ticket_enabled: undefined }]) {
      await wrapper.setProps({ account: { ...account, ...patch } })
      expect(wrapper.find('[aria-label="sharedPool.codexTicket"]').exists()).toBe(false)
    }
    await wrapper.setProps({ account, admin: true })
    expect(wrapper.find('[aria-label="sharedPool.codexTicket"]').exists()).toBe(false)
  })

  it('keeps required Pro tickets on and blocks attempts to disable them', async () => {
    const wrapper = render()
    await wrapper.setProps({ account: { ...account, subscription_tier: 'pro', codex_ticket_required: true, codex_ticket_enabled: false } })
    const ticket = wrapper.get('[role="switch"][aria-label="sharedPool.codexTicket"]')
    expect(ticket.attributes('aria-checked')).toBe('true')
    expect(ticket.attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="ticket-required"]').text()).toBe('sharedPool.codexTicketRequired')
    await ticket.trigger('click')
    wrapper.findAllComponents(Toggle).find(toggle => toggle.attributes('aria-label') === 'sharedPool.codexTicket')!.vm.$emit('update:modelValue', false)
    expect(wrapper.emitted('codexTicket')).toBeUndefined()
  })

  it('locks switches and actions while an account operation is pending', async () => {
    const wrapper = render(true)
    for (const button of wrapper.findAll('button')) {
      expect(button.attributes('disabled')).toBeDefined()
      await button.trigger('click')
    }
    for (const toggle of wrapper.findAllComponents(Toggle)) toggle.vm.$emit('update:modelValue', false)
    expect(wrapper.emitted('enable')).toBeUndefined()
    expect(wrapper.emitted('protection')).toBeUndefined()
    expect(wrapper.emitted('codexTicket')).toBeUndefined()
    expect(wrapper.emitted('edit')).toBeUndefined()
    expect(wrapper.emitted('test')).toBeUndefined()
    expect(wrapper.emitted('remove')).toBeUndefined()
  })

  it('locks account controls during quota operations and forwards a completed reset refresh', async () => {
    const wrapper = render()
    const usage = wrapper.getComponent(SharedAccountUsage)
    usage.vm.$emit('busy-change', true)
    await wrapper.vm.$nextTick()
    expect(wrapper.findAll('button').every(button => button.attributes('disabled') !== undefined)).toBe(true)
    expect(usage.props('busy')).toBe(false)
    usage.vm.$emit('busy-change', false)
    usage.vm.$emit('usage-updated')
    await wrapper.vm.$nextTick()
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.emitted('usageUpdated')).toHaveLength(1)
  })

  it('retains account actions, earnings, and the active proxy revenue split', async () => {
    const wrapper = render()
    expect(wrapper.text()).toContain('$1.2345')
    expect(wrapper.text()).toContain('$56.7890')
    expect(wrapper.text()).toContain('$8.5000')
    expect(wrapper.get('[data-test="revenue-split-inline"]').text()).toBe('sharedPool.revenueSplitInline: 5%, 1%, 94%')
    for (const [label, event] of [['common.edit', 'edit'], ['sharedPool.test', 'test'], ['sharedPool.remove', 'remove']]) {
      await wrapper.findAll('button').find(button => button.text() === label)!.trigger('click')
      expect(wrapper.emitted(event)).toHaveLength(1)
    }
    await wrapper.setProps({ account: { ...account, proxy_mode: 'custom' } })
    expect(wrapper.get('[data-test="revenue-split-inline"]').text()).toBe('sharedPool.revenueSplitInline: 5%, 0%, 95%')
  })

  it('shows a compact cooldown schedule without overriding a manually disabled state', async () => {
    const wrapper = render()
    await wrapper.setProps({ account: { ...account, enabled: false, daily_cooldown: { enabled: true, start: '23:00', end: '08:00', timezone: 'Asia/Shanghai' } } })
    const cooldown = wrapper.get('[data-test="daily-cooldown-summary"]')
    expect(cooldown.text()).toBe('sharedPool.dailyCooldownSummary: 23:00, 08:00, Asia/Shanghai')
    expect(cooldown.attributes('title')).toBe('sharedPool.dailyCooldownHint')
    expect(wrapper.text()).toContain('sharedPool.disabled')
    expect(wrapper.find('[role="switch"][aria-label="sharedPool.sharing"]').exists()).toBe(false)
    expect(wrapper.emitted('enable')).toBeUndefined()
    wrapper.unmount()
  })

  it('hides the schedule for legacy accounts and explicitly disabled cooldowns', async () => {
    const wrapper = render()
    expect(wrapper.find('[data-test="daily-cooldown-summary"]').exists()).toBe(false)
    await wrapper.setProps({ account: { ...account, daily_cooldown: { enabled: false } } })
    expect(wrapper.find('[data-test="daily-cooldown-summary"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
