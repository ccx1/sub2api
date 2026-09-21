import type { AccountUsageInfo, UsageProgress, WindowStats } from '@/types'

export interface OAuthUsageWindow {
  key: string
  label: string
  labelKey?: string
  utilization: number
  resetsAt: string | null
  windowStats?: WindowStats | null
  estimatedTotalCost?: number | null
  showNowWhenIdle?: boolean
  color: 'indigo' | 'emerald' | 'purple' | 'amber'
}

export const ANTIGRAVITY_USAGE_GROUPS = [
  {
    key: 'gemini3Pro', label: '3 Pro', color: 'indigo',
    models: ['gemini-3-pro-low', 'gemini-3-pro-high', 'gemini-3-pro-preview']
  },
  {
    key: 'gemini3Flash', label: '3 Flash', color: 'emerald',
    models: ['gemini-3-flash']
  },
  {
    key: 'gemini3Image', label: 'Image', color: 'purple',
    models: ['gemini-2.5-flash-image', 'gemini-3.1-flash-image', 'gemini-3-pro-image']
  },
  {
    key: 'claude', label: 'Claude', color: 'amber',
    models: [
      'claude-fable-5-1', 'claude-fable-5',
      'claude-sonnet-4-5', 'claude-opus-4-5-thinking',
      'claude-sonnet-4-6', 'claude-opus-4-6', 'claude-opus-4-6-thinking',
      'claude-opus-4-7', 'claude-opus-4-8'
    ]
  }
] as const

export function estimateUsageWindowCost(window: UsageProgress | null | undefined): number | null {
  const utilization = window?.utilization
  const cost = window?.window_stats?.cost
  if (typeof utilization !== 'number' || typeof cost !== 'number' ||
    !Number.isFinite(utilization) || !Number.isFinite(cost) || utilization <= 0 || cost <= 0) {
    return null
  }
  const estimate = cost * 100 / utilization
  return Number.isFinite(estimate) && estimate > 0 ? estimate : null
}

export function getAntigravityUsage(
  usage: AccountUsageInfo | null | undefined,
  modelNames: readonly string[]
): { utilization: number; resetTime: string | null } | null {
  const quota = usage?.antigravity_quota
  if (!quota) return null
  const matches = modelNames.map((name) => quota[name]).filter(Boolean)
  if (matches.length === 0) return null
  let utilization = 0
  let resetTime: string | null = null
  for (const model of matches) {
    if (Number.isFinite(model.utilization)) utilization = Math.max(utilization, model.utilization)
    if (model.reset_time && (!resetTime || model.reset_time < resetTime)) resetTime = model.reset_time
  }
  return { utilization, resetTime }
}

function progressWindow(
  progress: UsageProgress,
  display: Pick<OAuthUsageWindow, 'key' | 'label' | 'color'>,
  includeStats = true
): OAuthUsageWindow {
  return {
    ...display,
    utilization: progress.utilization,
    resetsAt: progress.resets_at,
    ...(includeStats ? { windowStats: progress.window_stats } : {})
  }
}

export function buildGeminiDailyUsageWindows(
  usage: AccountUsageInfo | null | undefined,
  forceShared = false
): OAuthUsageWindow[] {
  if (!usage) return []
  if (forceShared || usage.gemini_shared_daily) {
    return usage.gemini_shared_daily
      ? [progressWindow(usage.gemini_shared_daily, { key: 'shared_daily', label: '1d', color: 'indigo' })]
      : []
  }
  const windows: OAuthUsageWindow[] = []
  if (usage.gemini_pro_daily) {
    windows.push(progressWindow(usage.gemini_pro_daily, { key: 'pro_daily', label: 'pro', color: 'indigo' }))
  }
  if (usage.gemini_flash_daily) {
    windows.push(progressWindow(usage.gemini_flash_daily, { key: 'flash_daily', label: 'flash', color: 'emerald' }))
  }
  return windows
}

function buildRollingUsageWindows(platform: string, usage: AccountUsageInfo): OAuthUsageWindow[] {
  const windows: OAuthUsageWindow[] = []
  const isOpenAI = platform === 'openai'
  if (usage.five_hour) {
    windows.push({
      ...progressWindow(usage.five_hour, { key: 'five_hour', label: '5h', color: 'indigo' }),
      ...(isOpenAI ? { showNowWhenIdle: true } : {})
    })
  }
  if (usage.seven_day) {
    windows.push({
      ...progressWindow(usage.seven_day, { key: 'seven_day', label: '7d', color: 'emerald' }, isOpenAI),
      ...(isOpenAI ? { estimatedTotalCost: estimateUsageWindowCost(usage.seven_day), showNowWhenIdle: true } : {})
    })
  }
  if (!isOpenAI && usage.seven_day_sonnet) {
    windows.push(progressWindow(usage.seven_day_sonnet, { key: 'seven_day_sonnet', label: '7d S', color: 'purple' }, false))
  }
  if (!isOpenAI && usage.seven_day_fable) {
    windows.push(progressWindow(usage.seven_day_fable, { key: 'seven_day_fable', label: '7d F', color: 'amber' }, false))
  }
  return windows
}

export function buildOAuthUsageWindows(
  platform: string,
  usage: AccountUsageInfo | null | undefined
): OAuthUsageWindow[] {
  if (!usage) return []
  if (platform === 'openai' || platform === 'anthropic') return buildRollingUsageWindows(platform, usage)
  if (platform === 'gemini') return buildGeminiDailyUsageWindows(usage)
  if (platform !== 'antigravity') return []
  return ANTIGRAVITY_USAGE_GROUPS.flatMap(({ key, label, color, models }) => {
    const quota = getAntigravityUsage(usage, models)
    return quota ? [{
      key, label, color,
      labelKey: `admin.accounts.usageWindow.${key}`,
      utilization: quota.utilization,
      resetsAt: quota.resetTime
    }] : []
  })
}
