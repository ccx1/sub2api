export interface DailyCooldown {
  enabled: boolean
  start: string
  end: string
  timezone: string
}

export function normalizeDailyCooldown(value?: unknown): DailyCooldown {
  const raw = value && typeof value === 'object' ? value as Partial<DailyCooldown> : {}
  return {
    enabled: raw.enabled === true,
    start: typeof raw.start === 'string' ? raw.start : '23:00',
    end: typeof raw.end === 'string' ? raw.end : '08:00',
    timezone: typeof raw.timezone === 'string' ? raw.timezone : 'Asia/Shanghai'
  }
}

export function dailyCooldownValidationError(config: DailyCooldown): string | null {
  if (!config.enabled) return null
  const validTime = /^(?:[01]\d|2[0-3]):[0-5]\d$/
  if (!validTime.test(config.start) || !validTime.test(config.end)) return 'admin.accounts.dailyCooldown.invalidTime'
  if (config.start === config.end) return 'admin.accounts.dailyCooldown.equalTimes'
  try {
    if (!config.timezone.trim()) return 'admin.accounts.dailyCooldown.invalidTimezone'
    new Intl.DateTimeFormat('en', { timeZone: config.timezone }).format()
  } catch {
    return 'admin.accounts.dailyCooldown.invalidTimezone'
  }
  return null
}

export function withDailyCooldownExtra(extra: Record<string, unknown> | undefined, config: DailyCooldown) {
  const cooldown = config.enabled ? { ...config, timezone: config.timezone.trim() } : { enabled: false }
  return { ...extra, daily_cooldown: cooldown }
}

export function isDailyCooldownActive(value: unknown, now = Date.now()): boolean {
  const config = normalizeDailyCooldown(value)
  if (!config.enabled || dailyCooldownValidationError(config)) return false
  const parts = new Intl.DateTimeFormat('en-GB', {
    timeZone: config.timezone, hour: '2-digit', minute: '2-digit', hourCycle: 'h23'
  }).formatToParts(now)
  const time = `${parts.find(part => part.type === 'hour')?.value}:${parts.find(part => part.type === 'minute')?.value}`
  return config.start < config.end
    ? time >= config.start && time < config.end
    : time >= config.start || time < config.end
}
