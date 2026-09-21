import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import SharedPoolCatalog from '../SharedPoolCatalog.vue'
import type { SharedPoolOverview, SharedPoolTier } from '@/api/sharedPool'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
const tier = (overrides: Partial<SharedPoolTier> = {}): SharedPoolTier => ({
  platform: 'openai', tier: 'free', total_accounts: 10, schedulable_accounts: 7, available: true,
  participating_accounts: 9, participating_concurrency: 36, participating_concurrency_unlimited: false,
  concurrency_capacity: 30, concurrency_unlimited: false, current_concurrency: 8, ...overrides
})
const overview = (overrides: Partial<SharedPoolOverview> = {}): SharedPoolOverview => ({
  total_accounts: 10, schedulable_accounts: 7, concurrency_capacity: 30, concurrency_unlimited: false,
  participating_accounts: 9, participating_concurrency: 36, participating_concurrency_unlimited: false,
  current_concurrency: 8, settlement_multiplier: 0.5, platform_rate_bps: 500, proxy_rate_bps: 100,
  updated_at: '2026-09-20T12:00:00Z', tiers: [tier()], ...overrides
})
const render = (value: SharedPoolOverview | null = overview(), stale = false) => mount(SharedPoolCatalog, { props: { overview: value, stale } })

describe('shared account resource overview', () => {
  it('shows one card per platform and tier without the three aggregate cards', () => {
    const wrapper = render(overview({ tiers: [tier(), tier({ tier: 'pro', total_accounts: 3, schedulable_accounts: 0, available: false })] }))
    expect(wrapper.find('[data-test="overview-counts"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="overview-concurrency"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="your-settlement-multiplier"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-test="tier-card"]')).toHaveLength(2)
    expect(wrapper.find('[data-test="tier-card"] [data-test="your-settlement-multiplier"]').exists()).toBe(false)
    expect(wrapper.findAll('.card')).toHaveLength(2)
    expect(wrapper.get('[data-test="participating-accounts"]').text()).toBe('9 / 10')
    expect(wrapper.get('[data-test="concurrency-usage"]').text()).toBe('8 / 36')
    expect(wrapper.text()).toContain('2026-09-20T12:00:00Z')
    expect(wrapper.find('a').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('sharedPool.usePool')
  })

  it('retains unavailable tiers with their full account counts and distinguishes platform and account types', () => {
    const wrapper = render(overview({ tiers: [
      tier({ tier: 'free', participating_accounts: 2, schedulable_accounts: 0, available: false }),
      tier({ platform: 'antigravity', tier: 'free' }),
      tier({ tier: 'api_key' }), tier({ tier: 'unknown' })
    ] }))
    const cards = wrapper.findAll('[data-test="tier-card"]')
    expect(cards).toHaveLength(4)
    expect(cards[0].get('[data-test="participating-accounts"]').text()).toBe('2 / 10')
    expect(cards[0].get('[data-test="tier-status"]').text()).toBe('sharedPool.overviewUnavailable')
    expect(cards[1].text()).toContain('Antigravity')
    expect(cards[2].text()).toContain('API Key')
    expect(cards[3].text()).toContain('sharedPool.unknownTier')
    expect(cards[1].get('[data-test="tier-status"]').text()).toBe('sharedPool.overviewAvailable')
  })

  it('marks a stale snapshot and hides availability and concurrent use while retaining account totals', () => {
    const wrapper = render(overview(), true)
    expect(wrapper.get('[role="status"]').text()).toContain('sharedPool.overviewStale')
    expect(wrapper.get('[data-test="participating-accounts"]').text()).toBe('— / 10')
    expect(wrapper.get('[data-test="concurrency-usage"]').text()).toBe('— / —')
    expect(wrapper.get('[data-test="tier-status"]').text()).toBe('sharedPool.overviewStatusUnknown')
    expect(wrapper.get('[data-test="tier-status"]').classes().join(' ')).not.toContain('emerald')
  })

  it('uses distinct empty and unavailable states and always offers contribution', async () => {
    const wrapper = render(overview({ total_accounts: 0, schedulable_accounts: 0, tiers: [] }))
    expect(wrapper.text()).toContain('sharedPool.emptyOverview')
    expect(wrapper.find('[data-test="overview-counts"]').exists()).toBe(false)
    await wrapper.get('[data-test="contribute"]').trigger('click')
    expect(wrapper.emitted('contribute')).toEqual([[]])
    await wrapper.setProps({ overview: null, stale: true })
    expect(wrapper.text()).toContain('sharedPool.overviewUnavailableData')
    expect(wrapper.text()).not.toContain('sharedPool.emptyOverview')
    expect(wrapper.find('[data-test="tier-card"]').exists()).toBe(false)
  })

  it.each([null, undefined, -1, NaN, Infinity])('shows unknown occupancy for %s without replacing capacity', current => {
    const wrapper = render(overview({ tiers: [tier({ current_concurrency: current as number | null })] }))
    expect(wrapper.get('[data-test="concurrency-usage"]').text()).toBe('— / 36')
  })

  it.each([undefined, -1, NaN, Infinity])('does not label invalid schedulable count %s as available', value => {
    const wrapper = render(overview({ tiers: [tier({ schedulable_accounts: value as number })] }))
    expect(wrapper.get('[data-test="participating-accounts"]').text()).toBe('9 / 10')
    expect(wrapper.get('[data-test="tier-status"]').text()).toBe('sharedPool.overviewStatusUnknown')
  })

  it('preserves zero, unlimited capacity, and occupancy above a recently reduced limit', async () => {
    const wrapper = render(overview({ tiers: [tier({ current_concurrency: 0, participating_concurrency: 0 })] }))
    expect(wrapper.get('[data-test="concurrency-usage"]').text()).toBe('0 / 0')
    await wrapper.setProps({ overview: overview({ tiers: [tier({ current_concurrency: 31, participating_concurrency: 30 })] }) })
    expect(wrapper.get('[data-test="concurrency-usage"]').text()).toBe('31 / 30')
    await wrapper.setProps({ overview: overview({ tiers: [tier({ current_concurrency: null, participating_concurrency_unlimited: true })] }) })
    expect(wrapper.get('[data-test="concurrency-usage"]').text()).toBe('— / sharedPool.concurrencyUnlimited')
  })

  it('keeps large counts readable and statistic explanations keyboard accessible', () => {
    const wrapper = render(overview({ tiers: [tier({ total_accounts: 987654321, participating_accounts: 123456789 })] }))
    const value = wrapper.get('[data-test="participating-accounts"]')
    expect(value.text()).toBe('123456789 / 987654321')
    expect(value.classes()).toContain('break-all')
    expect(wrapper.findAll('[tabindex="0"]').length).toBeGreaterThan(0)
    expect(wrapper.findAll('[tabindex="0"]').every(item => item.attributes('title') && item.attributes('aria-label'))).toBe(true)
  })
})
