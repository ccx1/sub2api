import type { Proxy } from '@/types'

export type CodexTicketProxyMode = 'account' | 'inherit' | 'random' | 'fixed'
export interface CodexTicketProxySelection {
  mode: CodexTicketProxyMode
  proxyId: number | null
}

type TicketProxyAvailability = Pick<Proxy, 'id' | 'status'> & Partial<Pick<Proxy, 'expires_at'>>

export function isAvailableCodexTicketProxy(proxy: TicketProxyAvailability): boolean {
  if (proxy.status && proxy.status !== 'active') return false
  return !proxy.expires_at || Date.parse(proxy.expires_at) > Date.now()
}

export function supportsCodexTicketProxy(account?: {
  platform?: string
  type?: string
  parent_account_id?: number | null
} | null): boolean {
  return account?.platform === 'openai' && account.parent_account_id == null &&
    (account.type === 'oauth' || account.type === 'setup-token')
}

export function readCodexTicketProxy(extra?: Record<string, unknown> | null): CodexTicketProxySelection {
  const mode = extra?.codex_ticket_proxy_mode
  const proxyId = extra?.codex_ticket_proxy_id
  return {
    mode: mode === 'account' || mode === 'fixed' || mode === 'random' ? mode : 'inherit',
    proxyId: typeof proxyId === 'number' && Number.isSafeInteger(proxyId) && proxyId > 0 ? proxyId : null
  }
}

export function codexTicketProxyValidationError(
  selection: CodexTicketProxySelection,
  proxies: TicketProxyAvailability[]
): string | null {
  if (selection.mode !== 'fixed') return null
  if (!selection.proxyId) return 'admin.accounts.codexTicketProxy.required'
  const proxy = proxies.find(item => item.id === selection.proxyId)
  return proxy && isAvailableCodexTicketProxy(proxy)
    ? null
    : 'admin.accounts.codexTicketProxy.unavailable'
}

export function codexTicketProxyExtra(selection: CodexTicketProxySelection): Record<string, unknown> {
  return {
    codex_ticket_proxy_mode: selection.mode,
    codex_ticket_proxy_id: selection.mode === 'fixed' ? selection.proxyId : 0
  }
}
