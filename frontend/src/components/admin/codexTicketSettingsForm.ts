import type { CodexTicketSettings } from '@/api/admin/codexTicketSettings'

type NumericKey = { [K in keyof CodexTicketSettings]-?: CodexTicketSettings[K] extends number ? K : never }[keyof CodexTicketSettings]
export interface TicketNumericField { key: NumericKey; min: number; max: number }
export const ticketNumericGroups: { key: string; fields: TicketNumericField[] }[] = [
  { key: 'harvest', fields: [
    { key: 'ttl_seconds', min: 60, max: 86400 },
    { key: 'refresh_before_seconds', min: 0, max: 86399 },
    { key: 'harvest_probe_interval_seconds', min: 1, max: 3600 },
    { key: 'harvest_attempt_timeout_seconds', min: 1, max: 300 },
    { key: 'harvest_concurrency', min: 1, max: 64 }
  ] },
  { key: 'retry', fields: [
    { key: 'retry_max_attempts', min: 0, max: 1000 },
    { key: 'retry_exhausted_cooldown_seconds', min: 1, max: 86400 },
    { key: 'auth_cooldown_seconds', min: 1, max: 86400 },
    { key: 'rate_limit_cooldown_seconds', min: 1, max: 86400 }
  ] }
]

export const splitTicketList = (value: string): string[] => value.split(/[,，\n]/).map(item => item.trim()).filter(Boolean)
const normalizeTier = (value: string) => value.toLowerCase().replace(/[\s_-]/g, '')
const inRange = (value: number, min: number, max: number) => Number.isInteger(value) && value >= min && value <= max
const utf8ByteLength = (value: string) => new TextEncoder().encode(value).length

export interface TicketValidationError { key: string; field?: string; min?: number; max?: number }

export function validateTicketSettings(settings: CodexTicketSettings): TicketValidationError | null {
  const mode = settings.length_mode === undefined ? 'strict' : settings.length_mode
  if (mode !== 'auto' && mode !== 'strict') return { key: 'lengthMode' }
  const fields = [{ key: 'target_length' as NumericKey, min: 16, max: 8192 }, ...ticketNumericGroups.flatMap(group => group.fields)]
  for (const field of fields) {
    if (!inRange(settings[field.key], field.min, field.max)) return { key: 'range', field: field.key, min: field.min, max: field.max }
  }
  if (settings.refresh_before_seconds >= settings.ttl_seconds) return { key: 'refresh' }
  const normalizedModels = settings.models.map(model => model.trim())
  if (normalizedModels.length < 1 || normalizedModels.length > 32 || normalizedModels.some(model => !model || utf8ByteLength(model) > 160 || /[\s\0]/.test(model))) return { key: 'models' }
  if (new Set(normalizedModels).size !== normalizedModels.length) return { key: 'models' }
  if (settings.tier_rules.length > 32) return { key: 'tiers' }
  const identifiers = new Set<string>()
  for (const rule of settings.tier_rules) {
    if (!inRange(rule.target_length, 16, 8192)) return { key: 'tierLength' }
    if ((rule.aliases ?? []).length > 16) return { key: 'aliases' }
    for (const identifier of [rule.tier, ...(rule.aliases ?? [])]) {
      const trimmed = identifier.trim()
      const normalized = normalizeTier(trimmed)
      if (!normalized || trimmed.length > 80 || !/^[a-zA-Z0-9 _-]+$/.test(trimmed)) return { key: 'tierName' }
      if (identifiers.has(normalized)) return { key: 'duplicateTier' }
      identifiers.add(normalized)
    }
  }
  const steps = settings.retry_backoff_seconds
  if (!steps.length || steps.length > 16 || steps.some((value, index) => !inRange(value, 1, 86400) || (index > 0 && value < steps[index - 1]!))) return { key: 'backoff' }
  const rejected = settings.rejected_lengths
  if (rejected.length > 32 || rejected.some(value => !inRange(value, 16, 8192)) || new Set(rejected).size !== rejected.length) return { key: 'rejected' }
  const targets = [settings.target_length, ...settings.tier_rules.map(rule => rule.target_length)]
  if (mode === 'strict' && targets.some(value => rejected.includes(value))) return { key: 'overlap' }
  return null
}
