import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import SharedAccountUsage from '../SharedAccountUsage.vue'
import UsageProgressBar from '@/components/account/UsageProgressBar.vue'
import OpenAIQuotaResetCell from '@/components/account/OpenAIQuotaResetCell.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import type { SharedAccount } from '@/api/sharedPool'
import type { AccountUsageInfo } from '@/types'

const { getUsage, refreshQuota, resetQuota } = vi.hoisted(() => ({ getUsage: vi.fn(), refreshQuota: vi.fn(), resetQuota: vi.fn() }))
vi.mock('@/api/sharedPool', () => ({ sharedPoolAPI: { getUsage, refreshQuota, resetQuota } }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

const account = { id: 7, platform: 'openai', type: 'oauth', last_used_at: null } as SharedAccount
const usage: AccountUsageInfo = {
  updated_at: null, seven_day_sonnet: null,
  five_hour: { utilization: 35, resets_at: '2099-01-01T05:00:00Z', remaining_seconds: 7200 },
  seven_day: { utilization: 50, resets_at: '2099-01-07T05:00:00Z', remaining_seconds: 7200, window_stats: { requests: 10, tokens: 1000, cost: 15 } }
}
let wrapper: VueWrapper | undefined
function render(value = account) {
  wrapper = mount(SharedAccountUsage, { props: { account: value }, global: { stubs: { ConfirmDialog: true } } })
  return wrapper
}
beforeEach(() => { vi.clearAllMocks(); getUsage.mockReset(); getUsage.mockResolvedValue(usage) })
afterEach(() => wrapper?.unmount())

describe('shared OAuth usage windows', () => {
  it('loads and displays the original progress bars with reset time, statistics, and estimate', async () => {
    const view = render()
    expect(view.text()).toContain('common.loading')
    await flushPromises()
    expect(getUsage).toHaveBeenCalledWith(7, 'active', expect.any(AbortSignal), false)
    const bars = view.findAllComponents(UsageProgressBar)
    expect(bars.map(bar => bar.props('label'))).toEqual(['5h', '7d'])
    expect(bars[0].props()).toMatchObject({ utilization: 35, resetsAt: usage.five_hour!.resets_at })
    expect(bars[1].props()).toMatchObject({ utilization: 50, estimatedTotalCost: 30, windowStats: usage.seven_day!.window_stats })
    expect(view.text()).toContain('35%')
    expect(view.text()).toContain('50%')
  })

  it('does not fetch API key accounts', async () => {
    const view = render({ ...account, type: 'apikey' })
    await flushPromises()
    expect(getUsage).not.toHaveBeenCalled()
    expect(view.find('section').exists()).toBe(false)
  })

  it('loads Anthropic passive sampling first and forces an active query on demand', async () => {
    const view = render({ ...account, platform: 'anthropic' })
    await flushPromises()
    expect(getUsage).toHaveBeenLastCalledWith(7, 'passive', expect.any(AbortSignal), false)
    await view.get('button').trigger('click')
    await flushPromises()
    expect(getUsage).toHaveBeenLastCalledWith(7, 'active', expect.any(AbortSignal), true)
    await view.setProps({ busy: true })
    expect(view.get('button').attributes('disabled')).toBeDefined()
  })

  it('keeps empty and failed data distinct and allows retry', async () => {
    getUsage.mockResolvedValueOnce({ updated_at: null, five_hour: null })
    const view = render()
    await flushPromises()
    expect(view.text()).toContain('sharedPool.usageEmpty')
    getUsage.mockRejectedValueOnce(new Error('upstream secret'))
    await view.get('button').trigger('click')
    await flushPromises()
    expect(view.text()).toContain('sharedPool.usageUnavailable')
    expect(view.text()).not.toContain('upstream secret')
    await view.get('button').trigger('click')
    await flushPromises()
    expect(view.findAllComponents(UsageProgressBar)).toHaveLength(2)
    expect(view.text()).not.toContain('sharedPool.usageUnavailable')
  })

  it('preserves available windows while showing degraded status', async () => {
    getUsage.mockResolvedValueOnce({ ...usage, error: 'unavailable' })
    const view = render()
    await flushPromises()
    expect(view.findAllComponents(UsageProgressBar)).toHaveLength(2)
    expect(view.text()).toContain('sharedPool.usageUnavailable')
  })

  it('shows ready, missing and paused ticket states, and refreshes them after the ticket toggle', async () => {
    getUsage.mockResolvedValue({ ...usage, codex_turn_tickets: [
      { model: 'gpt-6-astra', ready: true, blocked: false, remaining_seconds: 125 },
      { model: 'gpt-5.6-sol', ready: false, blocked: false, remaining_seconds: 0 },
      { model: 'gpt-5.4', ready: false, blocked: true, remaining_seconds: 0 }
    ] })
    const view = render({ ...account, codex_ticket_enabled: true })
    await flushPromises()
    const states = view.findAll('[data-testid="codex-ticket-status"]')
    expect(states.map(row => row.findAll('span').map(span => span.text()).join(' '))).toEqual([
      'astra 2m05s', 'sol admin.accounts.openai.codexTurnTicketMissing', 'gpt-5.4 admin.accounts.openai.codexTurnTicketPaused'
    ])
    await view.setProps({ account: { ...account, codex_ticket_enabled: false } })
    await flushPromises()
    expect(getUsage).toHaveBeenCalledTimes(2)
    expect(view.find('[data-testid="codex-ticket-status"]').exists()).toBe(false)
  })

  it('hydrates reset credits, confirms an owner reset, and refreshes usage without another forced query', async () => {
    const credits = { available_count: 2, credits: [{ expires_at: '2099-01-01T00:00:00Z' }, { expires_at: '2099-02-01T00:00:00Z' }] }
    getUsage.mockResolvedValue({ ...usage, codex_reset_credit_snapshot: credits })
    resetQuota.mockResolvedValue({ code: 'ok', windows_reset: 2, cache_refreshed: true, account_state_recovered: true,
      quota: { fetched_at: 1, rate_limit_reset_credits: { available_count: 1, credits: credits.credits.slice(1) } } })
    const view = render()
    await flushPromises()
    const quota = view.getComponent(OpenAIQuotaResetCell)
    const countButton = quota.findAll('button').find(button => button.text().includes('openaiQuotaReset.count'))!
    expect(countButton.text()).toContain('2')
    await quota.findAll('button').find(button => button.text() === 'admin.accounts.openaiQuotaReset.reset')!.trigger('click')
    expect(resetQuota).not.toHaveBeenCalled()
    expect(quota.getComponent(ConfirmDialog).props('show')).toBe(true)
    quota.getComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    expect(resetQuota).toHaveBeenCalledWith(7)
    expect(countButton.text()).toContain('1')
    expect(getUsage).toHaveBeenLastCalledWith(7, 'active', expect.any(AbortSignal), false)
    expect(view.emitted('usage-updated')).toHaveLength(1)
    expect(view.emitted('busy-change')).toContainEqual([true])
    expect(view.emitted('busy-change')?.at(-1)).toEqual([false])
  })

  it('uses the owner count query and blocks simultaneous usage queries while it is pending', async () => {
    let resolveQuota!: (value: unknown) => void
    refreshQuota.mockReturnValue(new Promise(resolve => { resolveQuota = resolve }))
    const view = render()
    await flushPromises()
    const buttons = view.getComponent(OpenAIQuotaResetCell).findAll('button')
    await buttons.find(button => button.text().includes('openaiQuotaReset.count'))!.trigger('click')
    expect(refreshQuota).toHaveBeenCalledWith(7)
    const queryButton = buttons.find(button => button.text() === 'admin.accounts.usageWindow.activeQuery')!
    expect(queryButton.attributes('disabled')).toBeDefined()
    await queryButton.trigger('click')
    expect(getUsage).toHaveBeenCalledTimes(1)
    resolveQuota({ fetched_at: 1, rate_limit_reset_credits: { available_count: 0 }, cache_persisted: true })
    await flushPromises()
    expect(queryButton.attributes('disabled')).toBeUndefined()
  })

  it('keeps all OpenAI actions available without usage windows, while other platforms have no reset controls', async () => {
    getUsage.mockResolvedValue({ updated_at: null })
    const view = render()
    await flushPromises()
    expect(view.text()).toContain('admin.accounts.usageWindow.activeQuery')
    expect(view.text()).toContain('admin.accounts.openaiQuotaReset.count')
    expect(view.text()).toContain('admin.accounts.openaiQuotaReset.reset')
    await view.setProps({ account: { ...account, platform: 'gemini' } })
    await flushPromises()
    expect(view.findComponent(OpenAIQuotaResetCell).exists()).toBe(false)
  })

  it('ignores a stale account response and aborts the pending request on unmount', async () => {
    let resolveOld!: (value: AccountUsageInfo) => void
    getUsage.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve }))
    const view = render()
    const oldSignal = getUsage.mock.calls[0][2] as AbortSignal
    await view.setProps({ account: { ...account, id: 8 } })
    await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    resolveOld({ ...usage, five_hour: { ...usage.five_hour!, utilization: 99 } })
    await flushPromises()
    expect(view.findAllComponents(UsageProgressBar)[0].props('utilization')).toBe(35)
    const currentSignal = getUsage.mock.calls[1][2] as AbortSignal
    view.unmount()
    wrapper = undefined
    expect(currentSignal.aborted).toBe(true)
  })
})
