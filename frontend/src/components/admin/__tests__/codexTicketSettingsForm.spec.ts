import { describe, expect, it } from 'vitest'
import type { CodexTicketSettings } from '@/api/admin/codexTicketSettings'
import { defaultTicketProtection, readTicketProtection, ticketIPProtectionFields, ticketRejectionRetryFields, validateTicketSettings } from '../codexTicketSettingsForm'

const settings = (): CodexTicketSettings => ({
  enabled: true, target_length: 292, ttl_seconds: 3600, refresh_before_seconds: 600,
  harvest_probe_interval_seconds: 6, harvest_attempt_timeout_seconds: 25, fail_closed: true,
  models: ['gpt-6-astra'], tier_rules: [{ tier: 'team', aliases: ['business'], target_length: 332 }],
  rejected_lengths: [312], harvest_concurrency: 8, retry_backoff_seconds: [30, 60, 300],
  retry_max_attempts: 6, retry_exhausted_cooldown_seconds: 1800, auth_cooldown_seconds: 300,
  rate_limit_cooldown_seconds: 300, respect_retry_after: true
})

describe('ticket settings length mode validation', () => {
  it.each([undefined, 'latest_only', 'immediate', 'aged'] as const)('accepts supported request usage mode %s', usage_mode => {
    expect(validateTicketSettings({ ...settings(), usage_mode, min_ticket_age_seconds: usage_mode === 'aged' ? 300 : 0 })).toBeNull()
  })

  it.each(['unknown', null, true, 0])('rejects unsupported request usage mode %s', usage_mode => {
    expect(validateTicketSettings({ ...settings(), usage_mode } as unknown as CodexTicketSettings)).toEqual({ key: 'usageMode' })
  })

  it.each([undefined, 0, -1, 1.5, 2592001, NaN, Infinity, null, '300'])('rejects invalid historical age %s', min_ticket_age_seconds => {
    const candidate = { ...settings(), usage_mode: 'aged', min_ticket_age_seconds } as unknown as CodexTicketSettings
    expect(validateTicketSettings(candidate)).toEqual({ key: 'range', field: 'min_ticket_age_seconds', min: 1, max: 2592000 })
  })

  it.each([1, 300, 86400, 604800, 691200])('accepts historical age %s independently from ticket validity', min_ticket_age_seconds => {
    expect(validateTicketSettings({ ...settings(), usage_mode: 'aged', min_ticket_age_seconds })).toBeNull()
  })

  it('validates historical age independently from validity, STATE and Cookie intervals', () => {
    const candidate: CodexTicketSettings = {
      ...settings(), credential_mode: 'cookie', usage_mode: 'aged', min_ticket_age_seconds: 86400,
      cookie_ttl_seconds: 20, cookie_refresh_before_seconds: 5
    }
    expect(validateTicketSettings(candidate)).toBeNull()
    expect(validateTicketSettings({ ...candidate, ttl_seconds: 300, refresh_before_seconds: 10 })).toBeNull()
    expect(validateTicketSettings({ ...candidate, historical_ticket_validity_seconds: 1 })).toBeNull()
    expect(validateTicketSettings({ ...candidate, historical_ticket_validity_seconds: 2592001 }))
      .toEqual({ key: 'range', field: 'historical_ticket_validity_seconds', min: 1, max: 2592000 })
  })

  it.each([undefined, 'fresh_per_ticket', 'reuse_on_refresh'] as const)('accepts Cookie refresh mode %s', cookie_refresh_mode => {
    expect(validateTicketSettings({ ...settings(), cookie_refresh_mode })).toBeNull()
  })

  it.each([undefined, false, true])('accepts same-route host skip switch %s', skip_same_route_host => {
    expect(validateTicketSettings({ ...settings(), skip_same_route_host })).toBeNull()
  })

  it.each(['false', 0, null, []])('rejects malformed same-route host skip switch %s', skip_same_route_host => {
    expect(validateTicketSettings({ ...settings(), skip_same_route_host } as unknown as CodexTicketSettings)).toEqual({ key: 'skipSameRouteHost' })
  })

  it.each([undefined, 1, 24, 168])('accepts same-route cooldown %s hours', same_route_cooldown_hours => {
    expect(validateTicketSettings({ ...settings(), same_route_cooldown_hours })).toBeNull()
  })

  it.each([0, -1, 1.5, 169, NaN, Infinity, null, '24'])('rejects invalid same-route cooldown %s', same_route_cooldown_hours => {
    expect(validateTicketSettings({ ...settings(), same_route_cooldown_hours } as unknown as CodexTicketSettings))
      .toEqual({ key: 'range', field: 'same_route_cooldown_hours', min: 1, max: 168 })
  })

  it.each([undefined, false, true])('accepts historical quality renewal switch %s', historical_quality_enabled => {
    expect(validateTicketSettings({ ...settings(), historical_quality_enabled })).toBeNull()
  })

  it.each(['true', 1, null])('rejects malformed historical quality renewal switch %s', historical_quality_enabled => {
    expect(validateTicketSettings({ ...settings(), historical_quality_enabled } as unknown as CodexTicketSettings))
      .toEqual({ key: 'historicalQualityEnabled' })
  })

  it.each([
    ['historical_quality_check_before_seconds', 5, 86400, 120],
    ['historical_quality_check_interval_seconds', 5, 86400, 30],
    ['historical_quality_extend_seconds', 1, 86400, 30]
  ] as const)('validates historical quality timing %s', (field, min, max, fallback) => {
    const candidate = { ...settings(), usage_mode: 'aged' as const, min_ticket_age_seconds: 300, historical_quality_enabled: true }
    expect(validateTicketSettings({ ...candidate, [field]: undefined })).toBeNull()
    expect(validateTicketSettings({ ...candidate, [field]: min })).toBeNull()
    expect(validateTicketSettings({ ...candidate, [field]: max })).toBeNull()
    expect(validateTicketSettings({ ...candidate, [field]: min - 1 })).toEqual({ key: 'range', field, min, max })
    expect(validateTicketSettings({ ...candidate, [field]: max + 1 })).toEqual({ key: 'range', field, min, max })
    expect(validateTicketSettings({ ...candidate, [field]: fallback + 0.5 })).toEqual({ key: 'range', field, min, max })
  })

  it('validates account-wide capacity independently of the legacy per-model capacity', () => {
    const candidate = { ...settings(), models: ['gpt-6-astra', 'gpt-5.6-sol'] }
    expect(validateTicketSettings({ ...candidate, account_pool_capacity: 1000 })).toBeNull()
    expect(validateTicketSettings({ ...candidate, account_pool_capacity: 1 }))
      .toEqual({ key: 'range', field: 'account_pool_capacity', min: 2, max: 1000 })
    expect(validateTicketSettings({ ...candidate, account_pool_capacity: 1001 }))
      .toEqual({ key: 'range', field: 'account_pool_capacity', min: 2, max: 1000 })
  })

  it('ignores the historical threshold in default mode without mutating the draft', () => {
    const candidate: CodexTicketSettings = { ...settings(), usage_mode: 'immediate', min_ticket_age_seconds: 3600 }
    const before = structuredClone(candidate)
    expect(validateTicketSettings(candidate)).toBeNull()
    expect(candidate).toEqual(before)
  })

  it.each([undefined, true, false])('supports consumption %s in both request usage modes', consume_after_use => {
    for (const usage_mode of ['immediate', 'aged'] as const) {
      expect(validateTicketSettings({ ...settings(), usage_mode, min_ticket_age_seconds: 300, consume_after_use })).toBeNull()
    }
  })

  it.each(['false', 0, null, []])('rejects malformed ticket consumption switch %s', consume_after_use => {
    expect(validateTicketSettings({ ...settings(), consume_after_use } as unknown as CodexTicketSettings)).toEqual({ key: 'consumeAfterUse' })
  })

  it.each([undefined, 'revalidate', 'replace'] as const)('accepts supported refresh strategy %s', refresh_strategy => {
    expect(validateTicketSettings({ ...settings(), refresh_strategy })).toBeNull()
  })

  it.each(['unknown', '', null, true, 0])('rejects invalid refresh strategy %s', refresh_strategy => {
    expect(validateTicketSettings({ ...settings(), refresh_strategy } as unknown as CodexTicketSettings)).toEqual({ key: 'refreshStrategy' })
  })

  it.each([{ lengths: [] }, { lengths: null }])('keeps cleared protection lengths $lengths empty without changing the input', ({ lengths }) => {
    const protection = { ...defaultTicketProtection(), reject_and_silence_lengths: lengths } as unknown as CodexTicketSettings['protection']
    const normalized = readTicketProtection(protection)
    expect(normalized.reject_and_silence_lengths).toEqual([])
    expect(protection?.reject_and_silence_lengths).toBe(lengths)
    expect(validateTicketSettings({ ...settings(), protection })).toBeNull()
  })

  it('defaults omitted protection lengths while cloning explicit lists', () => {
    expect(readTicketProtection().reject_and_silence_lengths).toEqual([312])
    const omitted = { ...defaultTicketProtection(), reject_and_silence_lengths: undefined } as unknown as CodexTicketSettings['protection']
    expect(readTicketProtection(omitted).reject_and_silence_lengths).toEqual([312])
    const protection = defaultTicketProtection()
    readTicketProtection(protection).reject_and_silence_lengths.push(356)
    expect(protection.reject_and_silence_lengths).toEqual([312])
  })

  it.each([undefined, 'state', 'cookie_state', 'cookie'] as const)('accepts credential mode %s', credential_mode => {
    expect(validateTicketSettings({ ...settings(), credential_mode })).toBeNull()
  })

  it('validates Cookie timing separately from the old STATE lifetime', () => {
    expect(validateTicketSettings({ ...settings(), cookie_ttl_seconds: 1, cookie_refresh_before_seconds: 0 })).toBeNull()
    expect(validateTicketSettings({ ...settings(), cookie_ttl_seconds: 0 })).toMatchObject({ key: 'range', field: 'cookie_ttl_seconds' })
    expect(validateTicketSettings({ ...settings(), cookie_ttl_seconds: 3601 })).toMatchObject({ key: 'range', field: 'cookie_ttl_seconds' })
    expect(validateTicketSettings({ ...settings(), cookie_refresh_before_seconds: -1 })).toMatchObject({ key: 'range', field: 'cookie_refresh_before_seconds' })
    expect(validateTicketSettings({ ...settings(), cookie_ttl_seconds: 20, cookie_refresh_before_seconds: 20 })).toEqual({ key: 'cookieRefresh' })
    expect(validateTicketSettings({ ...settings(), credential_mode: 'unknown' } as unknown as CodexTicketSettings)).toEqual({ key: 'credentialMode' })
  })

  it.each([undefined, 1, 5, 20])('accepts legacy and supported pool capacity %s', pool_capacity => {
    expect(validateTicketSettings({ ...settings(), pool_capacity })).toBeNull()
  })

  it.each([0, -1, 1.5, 21, NaN, Infinity, null, '5'])('rejects invalid pool capacity %s', pool_capacity => {
    expect(validateTicketSettings({ ...settings(), pool_capacity } as CodexTicketSettings)).toEqual({ key: 'range', field: 'pool_capacity', min: 1, max: 20 })
  })

  it.each([undefined, true, false])('accepts business verification %s', verify_business => {
    expect(validateTicketSettings({ ...settings(), verify_business })).toBeNull()
  })

  it.each([undefined, 1, 5, 10])('accepts quality probe rounds %s', business_verification_rounds => {
    expect(validateTicketSettings({ ...settings(), business_verification_rounds })).toBeNull()
  })

  it.each([0, -1, 1.5, 11, NaN, Infinity, null, '5'])('rejects invalid quality probe rounds %s', business_verification_rounds => {
    expect(validateTicketSettings({ ...settings(), business_verification_rounds } as CodexTicketSettings)).toEqual({ key: 'range', field: 'business_verification_rounds', min: 1, max: 10 })
  })

  it.each(['false', 0, null, []])('rejects invalid business verification %s', verify_business => {
    expect(validateTicketSettings({ ...settings(), verify_business } as CodexTicketSettings)).toEqual({ key: 'verifyBusiness' })
  })

  it.each([undefined, 0])('normalizes legacy policy rejection retry values %s without mutating input', value => {
    const legacy = { ...defaultTicketProtection(), rejection_retry_interval_seconds: value,
      rejection_retry_max_attempts: value, rejection_retry_cooldown_seconds: value }
    const before = { ...legacy }
    expect(readTicketProtection(legacy)).toEqual(defaultTicketProtection())
    expect(validateTicketSettings({ ...settings(), protection: legacy })).toBeNull()
    expect(legacy).toEqual(before)
  })

  it.each(ticketRejectionRetryFields)('validates policy rejection retry field $key', field => {
    for (const value of [-1, 1.5, field.max + 1]) {
      const protection = { ...defaultTicketProtection(), [field.key]: value }
      expect(validateTicketSettings({ ...settings(), protection })).toEqual({ key: 'range', field: field.key, min: field.min, max: field.max })
    }
    for (const value of [field.min, field.max]) {
      expect(validateTicketSettings({ ...settings(), protection: { ...defaultTicketProtection(), [field.key]: value } })).toBeNull()
    }
  })

  it.each([undefined, 0])('fills legacy IP protection numeric defaults %s without enabling protection', value => {
    const legacy = { ...defaultTicketProtection(), proxy_ip_protection_enabled: undefined }
    for (const field of ticketIPProtectionFields) legacy[field.key] = value as number
    const before = structuredClone(legacy)
    expect(readTicketProtection(legacy)).toEqual(defaultTicketProtection())
    expect(validateTicketSettings({ ...settings(), protection: legacy })).toBeNull()
    expect(legacy).toEqual(before)
  })

  it.each(ticketIPProtectionFields)('validates IP protection field $key even when disabled', field => {
    for (const value of [-1, 1.5, field.max + 1, NaN, Infinity, null, '3']) {
      const protection = { ...defaultTicketProtection(), [field.key]: value }
      expect(validateTicketSettings({ ...settings(), protection })).toEqual({ key: 'range', field: field.key, min: field.min, max: field.max })
    }
    for (const value of [field.min, field.max]) {
      expect(validateTicketSettings({ ...settings(), protection: { ...defaultTicketProtection(), [field.key]: value } })).toBeNull()
    }
  })

  it.each(['false', 0, null, []])('rejects malformed IP protection switch %s', value => {
    const protection = { ...defaultTicketProtection(), proxy_ip_protection_enabled: value } as unknown as CodexTicketSettings['protection']
    expect(validateTicketSettings({ ...settings(), protection })).toEqual({ key: 'ipProtectionEnabled' })
  })

  it.each([undefined, 1, 3, 1000])('accepts the default and supported proxy failure threshold %s', proxy_failure_threshold => {
    expect(validateTicketSettings({ ...settings(), proxy_failure_threshold })).toBeNull()
  })

  it.each([0, -1, 1.5, 1001])('rejects proxy failure threshold %s instead of applying the legacy default', proxy_failure_threshold => {
    expect(validateTicketSettings({ ...settings(), proxy_failure_threshold })).toEqual({ key: 'range', field: 'proxy_failure_threshold', min: 1, max: 1000 })
  })
  it.each([undefined, 'random', 'account', 'account_model'] as const)('accepts supported Session mode %s', session_mode => {
    expect(validateTicketSettings({ ...settings(), session_mode })).toBeNull()
  })

  it.each(['invalid', '', null, true])('rejects unsupported Session mode %s', session_mode => {
    expect(validateTicketSettings({ ...settings(), session_mode } as CodexTicketSettings)).toEqual({ key: 'sessionMode' })
  })
  it('validates protection separately and rejects active strict target conflicts', () => {
    const protection = { ...defaultTicketProtection(), enabled: true, reject_and_silence_lengths: [292] }
    expect(validateTicketSettings({ ...settings(), protection })).toEqual({ key: 'protectionOverlap' })
    expect(validateTicketSettings({ ...settings(), protection, length_mode: 'auto' })).toBeNull()
    expect(validateTicketSettings({ ...settings(), protection: { ...protection, enabled: false } })).toBeNull()
    expect(validateTicketSettings({ ...settings(), protection: { ...protection, reject_and_silence_lengths: [312, 312] } })).toEqual({ key: 'protectionLengths' })
    expect(validateTicketSettings({ ...settings(), protection: { ...defaultTicketProtection(), max_account_attempts: 0 } })).toEqual(expect.objectContaining({ key: 'range', field: 'max_account_attempts' }))
  })
  it.each(['invalid', '', null, true])('rejects unsupported length mode %s', length_mode => {
    expect(validateTicketSettings({ ...settings(), length_mode } as CodexTicketSettings)).toEqual({ key: 'lengthMode' })
  })

  it.each([undefined, 'strict'] as const)('keeps strict overlap validation for legacy or strict settings', length_mode => {
    expect(validateTicketSettings({ ...settings(), length_mode, target_length: 312 })).toEqual({ key: 'overlap' })
  })

  it('allows target and tier overlaps in auto while preserving all strict configuration', () => {
    const configured = { ...settings(), length_mode: 'auto' as const, target_length: 312,
      tier_rules: [{ tier: 'team', aliases: ['business'], target_length: 312 }] }
    const before = structuredClone(configured)
    expect(validateTicketSettings(configured)).toBeNull()
    expect(configured).toEqual(before)
  })

  it.each<{ changes: Partial<CodexTicketSettings>; key: string }>([
    { changes: { target_length: 0 }, key: 'range' },
    { changes: { rejected_lengths: [0] }, key: 'rejected' },
    { changes: { retry_backoff_seconds: [60, 30] }, key: 'backoff' },
    { changes: { models: [] }, key: 'models' },
    { changes: { tier_rules: [{ tier: 'team', aliases: [], target_length: 0 }] }, key: 'tierLength' }
  ])('retains configuration safety validation in auto: $key', ({ changes, key }) => {
    expect(validateTicketSettings({ ...settings(), length_mode: 'auto', ...changes })).toEqual(expect.objectContaining({ key }))
  })

  it('matches backend model emptiness and UTF-8 byte length limits', () => {
    expect(validateTicketSettings({ ...settings(), models: [''] })).toEqual({ key: 'models' })
    expect(validateTicketSettings({ ...settings(), models: ['a'.repeat(159) + 'é'] })).toEqual({ key: 'models' })
    expect(validateTicketSettings({ ...settings(), models: ['a'.repeat(158) + 'é'] })).toBeNull()
    expect(validateTicketSettings({ ...settings(), models: [' gpt-6-astra '] })).toBeNull()
    expect(validateTicketSettings({ ...settings(), models: ['gpt-6-astra', ' gpt-6-astra '] })).toEqual({ key: 'models' })
  })

  it('matches backend tier trimming and alias count limits', () => {
    expect(validateTicketSettings({ ...settings(), tier_rules: [{ tier: ` ${'a'.repeat(80)} `, aliases: [], target_length: 332 }] })).toBeNull()
    expect(validateTicketSettings({ ...settings(), tier_rules: [{ tier: 'team', aliases: Array.from({ length: 17 }, (_, i) => `alias${i}`), target_length: 332 }] })).toEqual({ key: 'aliases' })
    expect(validateTicketSettings({ ...settings(), tier_rules: [{ tier: 'team', aliases: Array.from({ length: 16 }, (_, i) => `alias${i}`), target_length: 332 }] })).toBeNull()
  })
})
