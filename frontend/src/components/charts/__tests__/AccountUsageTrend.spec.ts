import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import type { AccountUsageTrendPoint } from '@/types'
import AccountUsageTrend from '../AccountUsageTrend.vue'

const messages: Record<string, string> = {
  'admin.dashboard.accountUsageTrend': 'Account Usage Trend',
  'admin.dashboard.accountUsageTrendScope': 'Top 10 accounts by tokens in the selected range; all others combined',
  'admin.dashboard.otherAccounts': 'Other accounts',
  'admin.dashboard.noDataAvailable': 'No data available',
  'admin.dashboard.tokens': 'Tokens',
  'admin.dashboard.requests': 'Requests',
  'admin.dashboard.actual': 'Actual',
  'admin.dashboard.accountCost': 'Account cost',
}

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => messages[key] ?? key }) }))
vi.mock('vue-chartjs', () => ({
  Line: {
    props: ['data', 'options'],
    template: '<div class="chart-data">{{ JSON.stringify(data) }}</div>',
  },
}))

function point(accountId: number, date: string, tokens: number): AccountUsageTrendPoint {
  return {
    account_id: accountId, account_name: accountId ? `Account ${accountId}` : '', date,
    tokens, requests: tokens / 10, cost: tokens / 100,
    actual_cost: tokens / 200, account_cost: tokens / 400,
  }
}

function render(trendData: AccountUsageTrendPoint[], range: Record<string, string> = {}) {
  const wrapper = mount(AccountUsageTrend, {
    props: { trendData, ...range },
    global: { stubs: { LoadingSpinner: { template: '<div class="loading-spinner" />' } } },
  })
  const data = () => JSON.parse(wrapper.get('.chart-data').text())
  return { wrapper, data }
}

describe('AccountUsageTrend', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2026, 8, 19, 16, 39))
  })
  afterEach(() => { vi.useRealTimers() })

  it('shows the current hour when only other accounts have usage and keeps them last', () => {
    const { wrapper, data } = render([
      point(0, '2026-09-19 16:00', 300),
      point(1, '2026-09-19 15:00', 100),
    ])
    expect(data().labels).toEqual(['2026-09-19 15:00', '2026-09-19 16:00'])
    expect(data().datasets.map((series: { label: string }) => series.label)).toEqual(['Account 1', 'Other accounts'])
    expect(data().datasets[0].data).toEqual([100, 0])
    expect(data().datasets[1].data).toEqual([0, 300])
    expect(data().datasets[1].borderDash).toEqual([6, 4])
    expect(data().datasets[1].borderColor).not.toBe(data().datasets[0].borderColor)
    expect(wrapper.text()).toContain(messages['admin.dashboard.accountUsageTrendScope'])
  })

  it('uses each returned aggregate for all four metric switches', async () => {
    const { wrapper, data } = render([point(0, '2026-09-19 16:00', 400)])
    for (const [index, expected] of [400, 40, 2, 1].entries()) {
      await wrapper.findAll('button')[index].trigger('click')
      expect(data().datasets[0].label).toBe('Other accounts')
      expect(data().datasets[0].data).toEqual([expected])
    }
  })

  it('fills missing hourly buckets through the current hour without adding future hours', () => {
    const { data } = render([
      point(1, '2026-09-19 13:00', 100), point(0, '2026-09-19 16:00', 300),
    ], { startDate: '2026-09-19', endDate: '2026-09-20', granularity: 'hour' })
    expect(data().labels).toHaveLength(17)
    expect(data().labels[0]).toBe('2026-09-19 00:00')
    expect(data().labels.at(-1)).toBe('2026-09-19 16:00')
    expect(data().datasets[0].data.slice(13)).toEqual([100, 0, 0, 0])
    expect(data().datasets[1].data.slice(13)).toEqual([0, 0, 0, 300])
  })

  it('fills daily gaps and stops at today even when the selected end is tomorrow', () => {
    const { data } = render([
      point(1, '2026-09-17', 100), point(1, '2026-09-19', 300),
    ], { startDate: '2026-09-17', endDate: '2026-09-20', granularity: 'day' })
    expect(data().labels).toEqual(['2026-09-17', '2026-09-18', '2026-09-19'])
    expect(data().datasets[0].data).toEqual([100, 0, 300])
  })

  it('fills the complete historical day and does not invent other accounts', () => {
    const { data } = render([point(1, '2026-09-18 13:00', 100)], {
      startDate: '2026-09-18', endDate: '2026-09-18', granularity: 'hour',
    })
    expect(data().labels).toHaveLength(24)
    expect(data().labels.at(-1)).toBe('2026-09-18 23:00')
    expect(data().datasets).toHaveLength(1)
    expect(data().datasets[0].label).toBe('Account 1')
  })

  it('keeps account ordering stable by range tokens when switching metrics', async () => {
    const second = { ...point(2, '2026-09-19 15:00', 200), requests: 1 }
    const first = { ...point(1, '2026-09-19 16:00', 100), requests: 100 }
    const { wrapper, data } = render([first, second, point(0, '2026-09-19 16:00', 900)])
    expect(data().datasets.map((series: { label: string }) => series.label)).toEqual(['Account 2', 'Account 1', 'Other accounts'])
    await wrapper.findAll('button')[1].trigger('click')
    expect(data().datasets.map((series: { label: string }) => series.label)).toEqual(['Account 2', 'Account 1', 'Other accounts'])
  })

  it('preserves the empty and loading states even when a date range is supplied', async () => {
    const { wrapper } = render([], {
      startDate: '2026-09-19', endDate: '2026-09-19', granularity: 'hour',
    })
    expect(wrapper.find('.chart-data').exists()).toBe(false)
    expect(wrapper.text()).toContain('No data available')
    await wrapper.setProps({ loading: true })
    expect(wrapper.find('.loading-spinner').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('No data available')
  })
})
