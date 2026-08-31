import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import UsageStatsCards from '../UsageStatsCards.vue'

const messages: Record<string, string> = {
  'usage.totalRequests': 'Total Requests',
  'usage.inSelectedRange': 'in selected range',
  'usage.totalTokens': 'Total Tokens',
  'usage.in': 'In',
  'usage.out': 'Out',
  'usage.cacheTotal': 'Cache',
  'usage.cacheBreakdown': 'Cache Token Breakdown',
  'usage.cacheCreationTokensLabel': 'Cache Creation',
  'usage.cacheReadTokensLabel': 'Cache Read',
  'usage.totalCost': 'Total Cost',
  'usage.accountCost': 'Cost',
  'usage.standardCost': 'Standard',
  'usage.avgDuration': 'Avg Duration',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

const stats = {
  total_requests: 1,
  total_input_tokens: 100,
  total_output_tokens: 50,
  total_cache_tokens: 34,
  total_cache_creation_tokens: 12,
  total_cache_read_tokens: 22,
  total_tokens: 184,
  total_cost: 0.001,
  total_actual_cost: 0.001,
  total_account_cost: 0.001,
  average_duration_ms: 250,
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('UsageStatsCards', () => {
  it('renders cache token breakdown as a fixed body tooltip', async () => {
    const wrapper = mount(UsageStatsCards, {
      props: {
        stats,
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    })

    expect(wrapper.text()).toContain('Cache: 34')

    Object.defineProperty(window, 'innerWidth', {
      configurable: true,
      writable: true,
      value: 320,
    })

    const trigger = wrapper.get('[data-test="usage-cache-breakdown-trigger"]')
    vi.spyOn(trigger.element, 'getBoundingClientRect').mockReturnValue({
      left: 100,
      right: 160,
      top: 50,
      bottom: 70,
      width: 60,
      height: 20,
      x: 100,
      y: 50,
      toJSON: () => {},
    } as DOMRect)

    await trigger.trigger('mouseenter')

    const tooltip = document.body.querySelector('[data-test="usage-cache-breakdown-tooltip"]') as HTMLElement | null
    expect(tooltip).not.toBeNull()
    expect(tooltip?.style.position).toBe('fixed')
    expect(tooltip?.style.zIndex).toBe('100000020')
    expect(tooltip?.style.top).toBe('78px')
    expect(tooltip?.style.left).toBe('18px')
    expect(tooltip?.style.width).toBe('224px')
    expect(tooltip?.textContent).toContain('Cache Token Breakdown')
    expect(tooltip?.textContent).toContain('Cache Creation')
    expect(tooltip?.textContent).toContain('12')
    expect(tooltip?.textContent).toContain('Cache Read')
    expect(tooltip?.textContent).toContain('22')

    await trigger.trigger('mouseleave')

    expect(document.body.querySelector('[data-test="usage-cache-breakdown-tooltip"]')).toBeNull()
  })

  it('keeps the cache tooltip out of the layout while it is hidden', () => {
    const wrapper = mount(UsageStatsCards, {
      props: {
        stats,
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    })

    const tooltip = document.body.querySelector('[data-test="usage-cache-breakdown-tooltip"]')

    expect(tooltip).toBeNull()
  })
})
