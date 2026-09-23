import type { CodexTicketProtectionSettings, CodexTicketSettings } from '@/api/admin/codexTicketSettings'

export const defaultTicketProtection = (): Required<CodexTicketProtectionSettings> => ({
  enabled: false, reject_and_silence_lengths: [312], proxy_silence_seconds: 300,
  max_pool_rounds: 2, max_account_attempts: 6, account_cooldown_seconds: 1800,
  rejection_retry_interval_seconds: 30, rejection_retry_max_attempts: 6, rejection_retry_cooldown_seconds: 300
})
export const ticketProtectionFields = [
  { key: 'proxy_silence_seconds', min: 1, max: 86400 },
  { key: 'max_pool_rounds', min: 1, max: 100 },
  { key: 'max_account_attempts', min: 1, max: 1000 },
  { key: 'account_cooldown_seconds', min: 1, max: 86400 }
] as const
export const ticketRejectionRetryFields = [
  { key: 'rejection_retry_interval_seconds', min: 1, max: 86400 },
  { key: 'rejection_retry_max_attempts', min: 1, max: 1000 },
  { key: 'rejection_retry_cooldown_seconds', min: 1, max: 86400 }
] as const

export function readTicketProtection(settings?: CodexTicketProtectionSettings): Required<CodexTicketProtectionSettings> {
  const defaults = defaultTicketProtection()
  const result = { ...defaults, ...settings }
  const lengths = settings?.reject_and_silence_lengths
  result.reject_and_silence_lengths = lengths === undefined ? defaults.reject_and_silence_lengths : [...(lengths ?? [])]
  for (const { key } of ticketRejectionRetryFields) {
    if (result[key] === undefined || result[key] === 0) result[key] = defaults[key]
  }
  return result
}

type NumericKey = { [K in keyof CodexTicketSettings]-?: NonNullable<CodexTicketSettings[K]> extends number ? K : never }[keyof CodexTicketSettings]
export interface TicketNumericField { key: NumericKey; min: number; max: number }
export const ticketCookieFields: TicketNumericField[] = [
  { key: 'cookie_ttl_seconds', min: 1, max: 3600 },
  { key: 'cookie_refresh_before_seconds', min: 0, max: 3599 }
]
export const ticketNumericGroups: { key: string; fields: TicketNumericField[] }[] = [
  { key: 'harvest', fields: [
    { key: 'pool_capacity', min: 1, max: 20 },
    { key: 'ttl_seconds', min: 60, max: 86400 },
    { key: 'refresh_before_seconds', min: 0, max: 86399 },
    { key: 'harvest_probe_interval_seconds', min: 1, max: 3600 },
    { key: 'harvest_attempt_timeout_seconds', min: 1, max: 300 },
    { key: 'harvest_concurrency', min: 1, max: 64 },
    { key: 'proxy_failure_threshold', min: 1, max: 1000 }
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
const inRange = (value: number | undefined, min: number, max: number) => typeof value === 'number' && Number.isInteger(value) && value >= min && value <= max
const utf8ByteLength = (value: string) => new TextEncoder().encode(value).length

export interface TicketValidationError { key: string; field?: string; min?: number; max?: number }

export function validateTicketSettings(settings: CodexTicketSettings): TicketValidationError | null {
  const refreshStrategy = settings.refresh_strategy === undefined ? 'revalidate' : settings.refresh_strategy
  if (refreshStrategy !== 'revalidate' && refreshStrategy !== 'replace') return { key: 'refreshStrategy' }
  const credentialMode = settings.credential_mode === undefined ? 'state' : settings.credential_mode
  if (!['state', 'cookie_state', 'cookie'].includes(credentialMode)) return { key: 'credentialMode' }
  if (settings.verify_business !== undefined && typeof settings.verify_business !== 'boolean') return { key: 'verifyBusiness' }
  const sessionMode = settings.session_mode === undefined ? 'random' : settings.session_mode
  if (sessionMode !== 'random' && sessionMode !== 'account' && sessionMode !== 'account_model') return { key: 'sessionMode' }
  const mode = settings.length_mode === undefined ? 'strict' : settings.length_mode
  if (mode !== 'auto' && mode !== 'strict') return { key: 'lengthMode' }
  const fields = [{ key: 'target_length' as NumericKey, min: 16, max: 8192 }, { key: 'business_verification_rounds' as NumericKey, min: 1, max: 10 }, ...ticketCookieFields, ...ticketNumericGroups.flatMap(group => group.fields)]
  const defaults: Partial<Record<NumericKey, number>> = { business_verification_rounds: 1, proxy_failure_threshold: 3, pool_capacity: 5, cookie_ttl_seconds: 20, cookie_refresh_before_seconds: 5 }
  for (const field of fields) {
    const value = settings[field.key] === undefined ? defaults[field.key] : settings[field.key]
    if (!inRange(value, field.min, field.max)) return { key: 'range', field: field.key, min: field.min, max: field.max }
  }
  if (settings.refresh_before_seconds >= settings.ttl_seconds) return { key: 'refresh' }
  if ((settings.cookie_refresh_before_seconds ?? 5) >= (settings.cookie_ttl_seconds ?? 20)) return { key: 'cookieRefresh' }
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
  const protection = settings.protection
  if (protection) {
    const effective = readTicketProtection(protection)
    for (const field of [...ticketProtectionFields, ...ticketRejectionRetryFields]) {
      if (!inRange(effective[field.key], field.min, field.max)) return { key: 'range', field: field.key, min: field.min, max: field.max }
    }
    const lengths = effective.reject_and_silence_lengths
    if (lengths.length > 32 || lengths.some(value => !inRange(value, 16, 8192)) || new Set(lengths).size !== lengths.length) return { key: 'protectionLengths' }
    if (protection.enabled && mode === 'strict' && targets.some(value => lengths.includes(value))) return { key: 'protectionOverlap' }
  }
  return null
}
