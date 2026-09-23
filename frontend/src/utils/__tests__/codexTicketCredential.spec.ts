import { describe, expect, it } from 'vitest'
import { codexTicketCredentialExtra, codexTicketCredentialValidationError, readCodexTicketCredential } from '../codexTicketCredential'

describe('account ticket credential policy', () => {
  it('defaults old accounts to global settings and round-trips an explicit override', () => {
    expect(readCodexTicketCredential()).toEqual({ mode: 'inherit' })
    const policy = { mode: 'cookie_state' as const, ttl_seconds: 21, refresh_before_seconds: 0 }
    expect(readCodexTicketCredential(codexTicketCredentialExtra(policy))).toEqual(policy)
  })

  it('clears all stale overrides when returning to global or STATE mode', () => {
    expect(codexTicketCredentialExtra({ mode: 'inherit', ttl_seconds: 21, refresh_before_seconds: 5 }))
      .toEqual({ codex_ticket_credential_policy: { mode: 'inherit' } })
    expect(codexTicketCredentialExtra({ mode: 'state', ttl_seconds: 21 }))
      .toEqual({ codex_ticket_credential_policy: { mode: 'state' } })
  })

  it('leaves omitted Cookie timings inherited and preserves explicit zero refresh', () => {
    expect(codexTicketCredentialExtra({ mode: 'cookie' })).toEqual({ codex_ticket_credential_policy: { mode: 'cookie' } })
    expect(codexTicketCredentialExtra({ mode: 'cookie', refresh_before_seconds: 0 }))
      .toEqual({ codex_ticket_credential_policy: { mode: 'cookie', refresh_before_seconds: 0 } })
    expect(codexTicketCredentialValidationError({ mode: 'cookie', ttl_seconds: 1, refresh_before_seconds: 0 })).toBeNull()
  })

  it.each([0, 3601, 1.5, NaN, Infinity])('rejects invalid TTL %s', ttl_seconds => {
    expect(codexTicketCredentialValidationError({ mode: 'cookie', ttl_seconds })).toContain('ttlInvalid')
  })

  it.each([-1, 3600, 1.5, NaN, Infinity])('rejects invalid refresh %s', refresh_before_seconds => {
    expect(codexTicketCredentialValidationError({ mode: 'cookie_state', refresh_before_seconds })).toContain('refreshInvalid')
  })

  it('rejects a refresh that reaches or exceeds a configured TTL', () => {
    expect(codexTicketCredentialValidationError({ mode: 'cookie', ttl_seconds: 20, refresh_before_seconds: 20 })).toContain('refreshOrder')
    expect(codexTicketCredentialValidationError({ mode: 'cookie_state', ttl_seconds: 20, refresh_before_seconds: 21 })).toContain('refreshOrder')
  })
})
