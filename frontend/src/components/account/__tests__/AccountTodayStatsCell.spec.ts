import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AccountTodayStatsCell from '../AccountTodayStatsCell.vue'
import type { WindowStats } from '@/types'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/utils/format', () => ({ formatNumber: (value: number) => String(value), formatCurrency: (value: number) => '$' + value }))
const stats = { requests: 7, tokens: 1200, cost: 2.5, user_cost: 3.5 } as WindowStats

describe('account today and lifetime statistics', () => {
  it('keeps daily requests and user billing alongside the lifetime totals', () => {
    const wrapper = mount(AccountTodayStatsCell, { props: { stats: { ...stats, lifetime_tokens: 1_000_000, lifetime_cost: 50 } } })
    const text = wrapper.text()
    expect(text).toContain('admin.accounts.stats.requests:7')
    expect(text).toContain('admin.accounts.stats.todayTokens:1.2K')
    expect(text).toContain('admin.accounts.stats.lifetimeTokens:1.00M')
    expect(text).toContain('admin.accounts.stats.todayCost:$2.5')
    expect(text).toContain('admin.accounts.stats.lifetimeCost:$50')
    expect(text).toContain('usage.userBilled:$3.5')
  })
  it('does not present missing lifetime totals from an older response as zero', () => {
    const wrapper = mount(AccountTodayStatsCell, { props: { stats } })
    expect(wrapper.text()).toContain('admin.accounts.stats.lifetimeTokens:—')
    expect(wrapper.text()).toContain('admin.accounts.stats.lifetimeCost:—')
  })
})
