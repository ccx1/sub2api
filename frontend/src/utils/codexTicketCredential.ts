export type CodexTicketCredentialMode = 'inherit' | 'state' | 'cookie_state' | 'cookie'

export interface CodexTicketCredentialPolicy {
  mode: CodexTicketCredentialMode
  ttl_seconds?: number
  refresh_before_seconds?: number
}

export const codexTicketCredentialModes: CodexTicketCredentialMode[] = ['inherit', 'state', 'cookie_state', 'cookie']

export function readCodexTicketCredential(extra?: Record<string, unknown> | null): CodexTicketCredentialPolicy {
  const value = extra?.codex_ticket_credential_policy
  if (!value || typeof value !== 'object' || Array.isArray(value)) return { mode: 'inherit' }
  const policy = value as Record<string, unknown>
  const mode = codexTicketCredentialModes.includes(policy.mode as CodexTicketCredentialMode)
    ? policy.mode as CodexTicketCredentialMode : 'inherit'
  if (mode === 'inherit' || mode === 'state') return { mode }
  return {
    mode,
    ...(typeof policy.ttl_seconds === 'number' ? { ttl_seconds: policy.ttl_seconds } : {}),
    ...(typeof policy.refresh_before_seconds === 'number' ? { refresh_before_seconds: policy.refresh_before_seconds } : {})
  }
}

export function codexTicketCredentialValidationError(policy: CodexTicketCredentialPolicy): string | null {
  const key = 'admin.accounts.codexTicketCredential.'
  if (!codexTicketCredentialModes.includes(policy.mode)) return key + 'modeInvalid'
  if (policy.mode === 'inherit' || policy.mode === 'state') return null
  const { ttl_seconds: ttl, refresh_before_seconds: refresh } = policy
  if (ttl !== undefined && (!Number.isInteger(ttl) || ttl < 1 || ttl > 3600)) return key + 'ttlInvalid'
  if (refresh !== undefined && (!Number.isInteger(refresh) || refresh < 0 || refresh > 3599)) return key + 'refreshInvalid'
  if (ttl !== undefined && refresh !== undefined && refresh >= ttl) return key + 'refreshOrder'
  return null
}

export function codexTicketCredentialExtra(policy: CodexTicketCredentialPolicy): Record<string, unknown> {
  const value: CodexTicketCredentialPolicy = { mode: policy.mode }
  if (policy.mode === 'cookie_state' || policy.mode === 'cookie') {
    if (policy.ttl_seconds !== undefined) value.ttl_seconds = policy.ttl_seconds
    if (policy.refresh_before_seconds !== undefined) value.refresh_before_seconds = policy.refresh_before_seconds
  }
  return { codex_ticket_credential_policy: value }
}
