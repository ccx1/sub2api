import { apiClient } from '../client'

export interface CodexTicketTierRule {
  tier: string
  aliases: string[]
  target_length: number
}

export interface CodexTicketProtectionSettings {
  enabled: boolean
  reject_and_silence_lengths: number[]
  proxy_silence_seconds: number
  max_pool_rounds: number
  max_account_attempts: number
  account_cooldown_seconds: number
  rejection_retry_interval_seconds?: number
  rejection_retry_max_attempts?: number
  rejection_retry_cooldown_seconds?: number
}

export interface CodexTicketSettings {
  enabled: boolean
  credential_mode?: 'state' | 'cookie_state' | 'cookie'
  cookie_ttl_seconds?: number
  cookie_refresh_before_seconds?: number
  verify_business?: boolean
  business_verification_rounds?: number
  length_mode?: 'auto' | 'strict'
  target_length: number
  ttl_seconds: number
  pool_capacity?: number
  refresh_before_seconds: number
  harvest_probe_interval_seconds: number
  harvest_attempt_timeout_seconds: number
  fail_closed: boolean
  session_mode?: 'random' | 'account' | 'account_model'
  refresh_strategy?: 'revalidate' | 'replace'
  proxy_failure_threshold?: number
  models: string[]
  tier_rules: CodexTicketTierRule[]
  rejected_lengths: number[]
  harvest_concurrency: number
  retry_backoff_seconds: number[]
  retry_max_attempts: number
  retry_exhausted_cooldown_seconds: number
  auth_cooldown_seconds: number
  rate_limit_cooldown_seconds: number
  respect_retry_after: boolean
  protection?: CodexTicketProtectionSettings
}

export async function getCodexTicketSettings(): Promise<CodexTicketSettings> {
  const { data } = await apiClient.get<CodexTicketSettings>('/admin/settings/codex-tickets')
  return data
}

export async function saveCodexTicketSettings(settings: CodexTicketSettings): Promise<CodexTicketSettings> {
  const { data } = await apiClient.put<CodexTicketSettings>('/admin/settings/codex-tickets', settings)
  return data
}
