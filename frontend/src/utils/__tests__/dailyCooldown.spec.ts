import { describe, expect, it } from 'vitest'
import { dailyCooldownValidationError, isDailyCooldownActive, normalizeDailyCooldown, withDailyCooldownExtra } from '../dailyCooldown'

const config = { enabled: true, start: '23:00', end: '08:00', timezone: 'Asia/Shanghai' }

describe('daily account cooldown', () => {
  it('keeps legacy accounts disabled with a Beijing overnight default', () => {
    expect(normalizeDailyCooldown()).toEqual({ ...config, enabled: false })
    expect(isDailyCooldownActive(undefined)).toBe(false)
  })

  it.each([
    ['2026-09-19T14:59:00Z', false], ['2026-09-19T15:00:00Z', true],
    ['2026-09-19T23:59:00Z', true], ['2026-09-20T00:00:00Z', false]
  ])('uses the configured timezone and excludes the end boundary at %s', (time, active) => {
    expect(isDailyCooldownActive(config, Date.parse(time))).toBe(active)
  })

  it('supports same-day windows and DST using IANA time zones', () => {
    const daytime = { ...config, start: '09:00', end: '17:00', timezone: 'America/New_York' }
    expect(isDailyCooldownActive(daytime, Date.parse('2026-07-01T13:00:00Z'))).toBe(true)
    expect(isDailyCooldownActive(daytime, Date.parse('2026-01-01T13:00:00Z'))).toBe(false)
    expect(isDailyCooldownActive(daytime, Date.parse('2026-01-01T14:00:00Z'))).toBe(true)
  })

  it.each(['', '24:00', '23:60', '9:00'])('rejects invalid clock time %s', start => {
    expect(dailyCooldownValidationError({ ...config, start })).toBe('admin.accounts.dailyCooldown.invalidTime')
  })

  it('rejects equal bounds and invalid timezones, but permits disabling stale configuration', () => {
    expect(dailyCooldownValidationError({ ...config, end: '23:00' })).toBe('admin.accounts.dailyCooldown.equalTimes')
    expect(dailyCooldownValidationError({ ...config, timezone: 'GMT+8' })).toBe('admin.accounts.dailyCooldown.invalidTimezone')
    expect(dailyCooldownValidationError({ ...config, timezone: '', enabled: false })).toBeNull()
  })

  it('preserves unrelated extra and does not mutate the source object', () => {
    const extra = { custom_flag: true, daily_cooldown: { ...config } }
    const saved = withDailyCooldownExtra(extra, { ...config, enabled: false })
    expect(saved).toEqual({ custom_flag: true, daily_cooldown: { enabled: false } })
    expect(extra.daily_cooldown.enabled).toBe(true)
  })

  it('drops invalid hidden fields when disabling and restores defaults when reopened', () => {
    const saved = withDailyCooldownExtra(undefined, { enabled: false, start: '23:00', end: '23:00', timezone: 'Invalid/Zone' })
    expect(saved.daily_cooldown).toEqual({ enabled: false })
    expect(normalizeDailyCooldown(saved.daily_cooldown)).toEqual({ ...config, enabled: false })
  })
})
