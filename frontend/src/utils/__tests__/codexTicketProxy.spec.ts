import { describe, expect, it } from 'vitest'
import { codexTicketProxyExtra, codexTicketProxyValidationError, readCodexTicketProxy, supportsCodexTicketProxy } from '../codexTicketProxy'

describe('account ticket proxy configuration', () => {
  it('defaults legacy accounts to the global policy and preserves missing fixed proxy IDs', () => {
    expect(readCodexTicketProxy()).toEqual({ mode: 'inherit', proxyId: null, strategy: 'affinity' })
    expect(readCodexTicketProxy({ codex_ticket_proxy_mode: 'fixed', codex_ticket_proxy_id: 99 }))
      .toEqual({ mode: 'fixed', proxyId: 99, strategy: 'affinity' })
  })

  it.each(['account', 'inherit', 'random'] as const)('clears stale fixed proxy IDs when saving %s', mode => {
    expect(codexTicketProxyExtra({ mode, proxyId: 9 })).toEqual({ codex_ticket_proxy_mode: mode, codex_ticket_proxy_id: 0, codex_ticket_proxy_strategy: 'affinity' })
  })

  it('recognizes explicit account inheritance while preserving the legacy default', () => {
    expect(readCodexTicketProxy({ codex_ticket_proxy_mode: 'account' })).toEqual({ mode: 'account', proxyId: null, strategy: 'affinity' })
    expect(readCodexTicketProxy({})).toEqual({ mode: 'inherit', proxyId: null, strategy: 'affinity' })
    expect(codexTicketProxyValidationError({ mode: 'account', proxyId: 99 }, [])).toBeNull()
  })

  it('requires an available proxy for fixed mode only', () => {
    const proxies = [{ id: 1, status: 'active' as const }, { id: 2, status: 'inactive' as const }]
    expect(codexTicketProxyValidationError({ mode: 'fixed', proxyId: null }, proxies)).toContain('required')
    for (const proxyId of [2, 99]) {
      expect(codexTicketProxyValidationError({ mode: 'fixed', proxyId }, proxies)).toContain('unavailable')
    }
    expect(codexTicketProxyValidationError({ mode: 'fixed', proxyId: 1 }, proxies)).toBeNull()
    expect(codexTicketProxyValidationError({ mode: 'random', proxyId: null }, [])).toBeNull()
    expect(codexTicketProxyValidationError({ mode: 'inherit', proxyId: 99 }, [])).toBeNull()
  })

  it('round-trips rotation without persisting a fixed or business proxy', () => {
    const selected = readCodexTicketProxy({ codex_ticket_proxy_mode: 'random', codex_ticket_proxy_strategy: 'round_robin' })
    expect(codexTicketProxyExtra(selected)).toEqual({ codex_ticket_proxy_mode: 'random', codex_ticket_proxy_id: 0, codex_ticket_proxy_strategy: 'round_robin' })
  })

  it('limits account overrides to OpenAI OAuthLike parent accounts', () => {
    expect(supportsCodexTicketProxy({ platform: 'openai', type: 'oauth' })).toBe(true)
    expect(supportsCodexTicketProxy({ platform: 'openai', type: 'setup-token' })).toBe(true)
    expect(supportsCodexTicketProxy({ platform: 'openai', type: 'oauth', parent_account_id: 5 })).toBe(false)
    expect(supportsCodexTicketProxy({ platform: 'openai', type: 'apikey' })).toBe(false)
    expect(supportsCodexTicketProxy({ platform: 'anthropic', type: 'oauth' })).toBe(false)
  })

  it('rejects expired active proxies even before their stored status is updated', () => {
    const proxies = [{ id: 1, status: 'active' as const, expires_at: '2000-01-01T00:00:00Z' }]
    expect(codexTicketProxyValidationError({ mode: 'fixed', proxyId: 1 }, proxies)).toContain('unavailable')
  })
})
