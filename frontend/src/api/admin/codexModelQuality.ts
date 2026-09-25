import { apiClient } from '../client'

export interface CodexModelQualityPolicy {
  enabled: boolean
  interval_seconds: number
  timeout_seconds: number
  reserve_seconds: number
  max_ttl_percent: number
  concurrency: number
  account_concurrency: number
  retry_interval_seconds: number
  low_quality_consecutive_threshold: number
  low_quality_cooldown_seconds: number
  replacement_check_delay_seconds: number
  quarantine_on_failure: boolean
  fingerprint_enabled: boolean
  reasoning_effort: 'low' | 'medium' | 'high'
  model_priorities?: Record<string, number>
  canary_enabled?: boolean
  canary_prompt?: string
  canary_expected?: string[]
}

export const qualityCanaryLimits = { promptBytes: 4000, expectedCount: 5, expectedBytes: 200 }

export interface ModelQualityStatus {
  account_id: number
  model: string
  status: 'pending' | 'running' | 'passed' | 'suspect' | 'inconclusive' | 'quarantined' | 'skipped' | 'stale'
  reason: string
  checked_at?: string
  next_check_at?: string
  duration_ms?: number
  ticket_captured_at?: string
  ticket_expires_at?: string
  capability_score?: number
  fingerprint_candidate?: string
  fingerprint_probability?: number
  fingerprint_similarity?: number
  sample_count: number
  requests: number
  model_identity: 'unknown' | 'match' | 'mismatch'
  source: 'automatic' | 'manual' | 'anomaly' | 'diagnostic'
  baseline_reused?: boolean
  consecutive_low_quality?: number
  quality_paused_until?: string
}

export interface CodexModelQualityDiagnosticItem {
  model: string
  scheduled: boolean
  reason: string
  current?: ModelQualityStatus
}

export interface CodexModelQualityDiagnosticResult {
  account_id: number
  items: CodexModelQualityDiagnosticItem[]
}

export const qualityNumericFields = [
  { key: 'timeout_seconds', min: 5, max: 120 },
  { key: 'reserve_seconds', min: 5, max: 600 },
  { key: 'max_ttl_percent', min: 1, max: 25 },
  { key: 'concurrency', min: 1, max: 64 },
  { key: 'account_concurrency', min: 1, max: 64 },
  { key: 'retry_interval_seconds', min: 60, max: 3600 },
  { key: 'low_quality_consecutive_threshold', min: 1, max: 100 },
  { key: 'low_quality_cooldown_seconds', min: 60, max: 86400 },
  { key: 'replacement_check_delay_seconds', min: 60, max: 86400 }
] as const

export const modelPriorityRange = { min: 0, max: 100 } as const

export async function getCodexModelQualityPolicy(): Promise<CodexModelQualityPolicy> {
  const { data } = await apiClient.get<CodexModelQualityPolicy>('/admin/settings/codex-model-quality')
  return data
}

export async function saveCodexModelQualityPolicy(policy: CodexModelQualityPolicy): Promise<CodexModelQualityPolicy> {
  const { data } = await apiClient.put<CodexModelQualityPolicy>('/admin/settings/codex-model-quality', policy)
  return data
}

export async function getCodexModelQuality(accountId: number, options?: { schedule?: boolean }): Promise<ModelQualityStatus[]> {
  const { data } = await apiClient.get<{ items: ModelQualityStatus[] }>(`/admin/accounts/${accountId}/codex-ticket/model-quality`, {
    params: options?.schedule === false ? { schedule: false } : undefined
  })
  return data.items ?? []
}

export async function scheduleCodexModelQuality(accountId: number, model: string): Promise<{ scheduled: boolean; reason: string }> {
  const { data } = await apiClient.post<{ scheduled: boolean; reason: string }>(`/admin/accounts/${accountId}/codex-ticket/model-quality`, { model })
  return data
}

export async function diagnoseCodexModelQuality(accountId: number, models?: string[]): Promise<CodexModelQualityDiagnosticResult> {
  const payload = models && models.length > 0 ? { models } : undefined
  const { data } = await apiClient.post<CodexModelQualityDiagnosticResult>(`/admin/accounts/${accountId}/codex-ticket/diagnostic`, payload)
  return data
}
