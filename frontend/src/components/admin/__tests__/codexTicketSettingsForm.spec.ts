import { describe, expect, it } from 'vitest'
import type { CodexTicketSettings } from '@/api/admin/codexTicketSettings'
import { validateTicketSettings } from '../codexTicketSettingsForm'

const settings = (): CodexTicketSettings => ({
  enabled: true, target_length: 292, ttl_seconds: 3600, refresh_before_seconds: 600,
  harvest_probe_interval_seconds: 6, harvest_attempt_timeout_seconds: 25, fail_closed: true,
  models: ['gpt-6-astra'], tier_rules: [{ tier: 'team', aliases: ['business'], target_length: 332 }],
  rejected_lengths: [312], harvest_concurrency: 8, retry_backoff_seconds: [30, 60, 300],
  retry_max_attempts: 6, retry_exhausted_cooldown_seconds: 1800, auth_cooldown_seconds: 300,
  rate_limit_cooldown_seconds: 300, respect_retry_after: true
})

describe('ticket settings length mode validation', () => {
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
