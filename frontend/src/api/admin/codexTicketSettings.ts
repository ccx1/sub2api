import { apiClient } from '../client'

export interface CodexTicketTierRule {
  tier: string
  aliases: string[]
  target_length: number
}

export interface CodexTicketSettings {
  enabled: boolean
  length_mode?: 'auto' | 'strict'
  target_length: number
  ttl_seconds: number
  refresh_before_seconds: number
  harvest_probe_interval_seconds: number
  harvest_attempt_timeout_seconds: number
  fail_closed: boolean
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
}

export async function getCodexTicketSettings(): Promise<CodexTicketSettings> {
  const { data } = await apiClient.get<CodexTicketSettings>('/admin/settings/codex-tickets')
  return data
}

export async function saveCodexTicketSettings(settings: CodexTicketSettings): Promise<CodexTicketSettings> {
  const { data } = await apiClient.put<CodexTicketSettings>('/admin/settings/codex-tickets', settings)
  return data
}
