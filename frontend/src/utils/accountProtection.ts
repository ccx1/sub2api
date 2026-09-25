// 内置缺省值；实际全局策略从管理接口读取。
export const DEFAULT_ANTI_DEGRADE_MODE = 'mode2' as const

interface ProtectionAccount {
  anti_degradation?: boolean
  protection_mode?: string
  extra?: Record<string, unknown> | null
}

const managedModes = new Set([
  'legacy', 'mode1', 'mode2', 'minimal_compat', 'session_standard',
  'tls_node24', 'tls_codex_cli', 'low_concurrency'
])

export function isAccountIdentityManaged(account?: ProtectionAccount | null): boolean {
  if (!account) return false
  const value = account.extra?.anti_degrade
  const marker = value && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown> : undefined
  if (marker) {
    if (marker.enabled !== false && marker.mode !== 'mode2' && 'policy_version' in marker) return true
    return marker.enabled === true && managedModes.has(String(marker.mode || 'mode1'))
  }
  return account.anti_degradation === true && managedModes.has(account.protection_mode || '')
}
