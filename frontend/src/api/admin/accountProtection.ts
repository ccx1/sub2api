import { apiClient } from '../client'
import type { Account } from '@/types'

export interface AccountProtectionSettings {
  default_mode: string
}

export interface ProtectionSyncResult {
  updated: number
  unchanged: number
  failed: { account_id: number; error: string }[]
  error?: string
}

export interface ProtectionSettingsUpdateResult extends AccountProtectionSettings {
  sync: ProtectionSyncResult
}

export interface ProtectionStrategy {
  id: string
  name: string
  description: string
  category: string
  identity_mode: string
  tls_profile: string
  max_concurrency: number
  risk: string
  apply_supported: boolean
  requires_openai_oauth: boolean
  diagnostic_only: boolean
}

export interface ProtectionPreview {
  account_id: number
  enabled: boolean
  active_mode: string
  eligible: boolean
  reason?: string
  changes: { key: string; from?: unknown; to: unknown; note?: string }[]
}

export async function getProtectionSettings(): Promise<AccountProtectionSettings> {
  const { data } = await apiClient.get<AccountProtectionSettings>('/admin/accounts/protection/settings')
  return data
}

export async function saveProtectionSettings(settings: AccountProtectionSettings): Promise<ProtectionSettingsUpdateResult> {
  const { data } = await apiClient.put<ProtectionSettingsUpdateResult>('/admin/accounts/protection/settings', settings, { timeout: 120_000 })
  return data
}

export async function listProtectionStrategies(): Promise<ProtectionStrategy[]> {
  const { data } = await apiClient.get<{ strategies: ProtectionStrategy[] }>('/admin/accounts/anti-degrade/strategies')
  return data.strategies
}

export async function previewProtection(id: number, mode: string): Promise<ProtectionPreview> {
  const { data } = await apiClient.get<ProtectionPreview>(`/admin/accounts/${id}/anti-degrade`, { params: { mode } })
  return data
}

export async function applyProtection(id: number, mode: string): Promise<Account> {
  const { data } = await apiClient.post<Account>(`/admin/accounts/${id}/anti-degrade/apply`, undefined, { params: { mode } })
  return data
}
