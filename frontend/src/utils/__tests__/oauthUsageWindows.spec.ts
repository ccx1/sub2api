import { describe, expect, it } from 'vitest'
import type { AccountUsageInfo, UsageProgress } from '@/types'
import {
  buildGeminiDailyUsageWindows,
  buildOAuthUsageWindows,
  estimateUsageWindowCost,
  getAntigravityUsage
} from '../oauthUsageWindows'

const stats = { requests: 12, tokens: 1200, cost: 4 }
const reset = '2026-09-21T00:00:00Z'
function progress(utilization = 20, overrides: Partial<UsageProgress> = {}): UsageProgress {
  return { utilization, resets_at: reset, remaining_seconds: 3600, window_stats: stats, ...overrides }
}
function usage(overrides: Partial<AccountUsageInfo> = {}): AccountUsageInfo {
  return { updated_at: null, five_hour: null, seven_day: null, seven_day_sonnet: null, ...overrides }
}

describe('estimateUsageWindowCost', () => {
  it('uses account cost and utilization rather than standard or user cost', () => {
    expect(estimateUsageWindowCost(progress(20, {
      window_stats: { ...stats, standard_cost: 40, user_cost: 80 }
    }))).toBe(20)
  })

  it.each([0, -1, Number.NaN, Number.POSITIVE_INFINITY])('does not estimate at utilization %s', (value) => {
    expect(estimateUsageWindowCost(progress(value))).toBeNull()
  })

  it.each([0, -1, Number.NaN, Number.POSITIVE_INFINITY])('does not estimate from cost %s', (cost) => {
    expect(estimateUsageWindowCost(progress(20, { window_stats: { ...stats, cost } }))).toBeNull()
  })

  it('handles absent stats and overflow without showing a made-up total', () => {
    expect(estimateUsageWindowCost(null)).toBeNull()
    expect(estimateUsageWindowCost(undefined)).toBeNull()
    expect(estimateUsageWindowCost(progress(20, { window_stats: null }))).toBeNull()
    expect(estimateUsageWindowCost(progress(Number.MIN_VALUE))).toBeNull()
  })
})

describe('buildOAuthUsageWindows', () => {
  it('matches OpenAI windows, stats, estimation and idle-reset behavior', () => {
    const windows = buildOAuthUsageWindows('openai', usage({ five_hour: progress(0), seven_day: progress() }))
    expect(windows).toEqual([
      { key: 'five_hour', label: '5h', color: 'indigo', utilization: 0, resetsAt: reset, windowStats: stats, showNowWhenIdle: true },
      { key: 'seven_day', label: '7d', color: 'emerald', utilization: 20, resetsAt: reset, windowStats: stats, estimatedTotalCost: 20, showNowWhenIdle: true }
    ])
  })

  it('preserves Anthropic model-specific windows while stats remain 5h-only', () => {
    const windows = buildOAuthUsageWindows('anthropic', usage({
      five_hour: progress(), seven_day: progress(), seven_day_sonnet: progress(), seven_day_fable: progress()
    }))
    expect(windows.map(({ key, label }) => ({ key, label }))).toEqual([
      { key: 'five_hour', label: '5h' }, { key: 'seven_day', label: '7d' },
      { key: 'seven_day_sonnet', label: '7d S' }, { key: 'seven_day_fable', label: '7d F' }
    ])
    expect(windows[0].windowStats).toEqual(stats)
    expect(windows.slice(1).every((window) => !('windowStats' in window))).toBe(true)
    expect(windows.every((window) => !('estimatedTotalCost' in window))).toBe(true)
  })

  it('preserves expired reset times and over-limit usage for the progress bar', () => {
    const expired = '2020-01-01T00:00:00Z'
    expect(buildOAuthUsageWindows('openai', usage({ five_hour: progress(120, { resets_at: expired }) }))[0])
      .toMatchObject({ utilization: 120, resetsAt: expired })
  })

  it('keeps a real idle window with a null reset while omitting missing windows', () => {
    expect(buildOAuthUsageWindows('anthropic', usage({ seven_day: progress(0, { resets_at: null }) })))
      .toEqual([{ key: 'seven_day', label: '7d', color: 'emerald', utilization: 0, resetsAt: null }])
    expect(buildOAuthUsageWindows('openai', null)).toEqual([])
    expect(buildOAuthUsageWindows('openai', usage())).toEqual([])
    expect(buildOAuthUsageWindows('unknown', usage({ five_hour: progress() }))).toEqual([])
  })

  it('maps Antigravity groups with matching translated labels and no window stats', () => {
    const windows = buildOAuthUsageWindows('antigravity', usage({ antigravity_quota: {
      'gemini-3-pro-high': { utilization: 25, reset_time: reset },
      'gemini-3-flash': { utilization: 35, reset_time: reset },
      'gemini-3.1-flash-image': { utilization: 45, reset_time: reset },
      'claude-fable-5-1': { utilization: 55, reset_time: reset },
      'other-model': { utilization: 99, reset_time: reset }
    } }))
    expect(windows.map(({ key, utilization }) => [key, utilization])).toEqual([
      ['gemini3Pro', 25], ['gemini3Flash', 35], ['gemini3Image', 45], ['claude', 55]
    ])
    expect(windows.every((window) => window.labelKey === `admin.accounts.usageWindow.${window.key}`)).toBe(true)
    expect(windows.every((window) => !('windowStats' in window))).toBe(true)
  })
})

describe('buildGeminiDailyUsageWindows', () => {
  it('prefers shared daily quota over per-model quotas and omits minute windows', () => {
    const data = usage({
      gemini_shared_daily: progress(10), gemini_pro_daily: progress(20), gemini_flash_daily: progress(30),
      gemini_shared_minute: progress(80), gemini_pro_minute: progress(90)
    })
    expect(buildGeminiDailyUsageWindows(data)).toEqual([
      { key: 'shared_daily', label: '1d', color: 'indigo', utilization: 10, resetsAt: reset, windowStats: stats }
    ])
    expect(buildOAuthUsageWindows('gemini', data)).toEqual(buildGeminiDailyUsageWindows(data))
  })

  it('uses pro and flash daily windows when there is no shared pool', () => {
    expect(buildGeminiDailyUsageWindows(usage({ gemini_pro_daily: progress(20), gemini_flash_daily: progress(30) })))
      .toEqual([
        { key: 'pro_daily', label: 'pro', color: 'indigo', utilization: 20, resetsAt: reset, windowStats: stats },
        { key: 'flash_daily', label: 'flash', color: 'emerald', utilization: 30, resetsAt: reset, windowStats: stats }
      ])
  })

  it('preserves administrator credential-based shared-pool detection', () => {
    expect(buildGeminiDailyUsageWindows(usage({ gemini_pro_daily: progress() }), true)).toEqual([])
    expect(buildGeminiDailyUsageWindows(usage({ gemini_pro_daily: progress(), gemini_shared_minute: progress() }), true)).toEqual([])
    expect(buildGeminiDailyUsageWindows(usage({ gemini_pro_daily: progress(), gemini_shared_minute: progress() })))
      .toHaveLength(1)
    expect(buildGeminiDailyUsageWindows(usage({ gemini_pro_minute: progress() }))).toEqual([])
    expect(buildGeminiDailyUsageWindows(null)).toEqual([])
  })
})

describe('getAntigravityUsage', () => {
  it('takes highest utilization and earliest reset independently across requested models', () => {
    expect(getAntigravityUsage(usage({ antigravity_quota: {
      first: { utilization: 40, reset_time: '2026-09-23T00:00:00Z' },
      second: { utilization: 10, reset_time: '2020-01-01T00:00:00Z' },
      other: { utilization: 100, reset_time: '2019-01-01T00:00:00Z' }
    } }), ['first', 'second'])).toEqual({ utilization: 40, resetTime: '2020-01-01T00:00:00Z' })
  })

  it('distinguishes a zero-utilization quota from a missing model', () => {
    const data = usage({ antigravity_quota: { zero: { utilization: 0, reset_time: '' } } })
    expect(getAntigravityUsage(data, ['zero'])).toEqual({ utilization: 0, resetTime: null })
    expect(getAntigravityUsage(data, ['missing'])).toBeNull()
    expect(getAntigravityUsage(null, ['zero'])).toBeNull()
  })

  it('ignores non-finite utilization when aggregating model quotas', () => {
    expect(getAntigravityUsage(usage({ antigravity_quota: {
      invalid: { utilization: Number.POSITIVE_INFINITY, reset_time: '' },
      valid: { utilization: 12, reset_time: reset }
    } }), ['invalid', 'valid'])).toEqual({ utilization: 12, resetTime: reset })
  })
})
