import { apiClient, buildApiUrl } from './client'
import type { Account, AccountUsageInfo, ClaudeModel } from '@/types'
import type { OpenAIQuotaUsage, OpenAIQuotaResetResult } from '@/api/admin/accounts'
import type { ExcelBPSOptions } from '@/utils/excelBPSOptions'

export type SharedPlatform = 'openai' | 'anthropic' | 'gemini' | 'antigravity'
export interface SharedSettlementPolicy {
  settlement_multiplier?: number
}
export interface SharedDailyCooldown { enabled: boolean; start?: string; end?: string; timezone?: string }
export interface SharedPage<T> { items: T[]; total: number; page: number; page_size: number }
export interface SharedPool {
  id: number; name: string; platform: SharedPlatform; rate_multiplier: number
  available_accounts: number; description: string | null
  concurrency_capacity?: number | null; concurrency_unlimited?: boolean
  current_concurrency?: number | null
  total_accounts?: number | null
}
export interface SharedPoolCapacity {
  total_accounts: number; available_accounts: number; schedulable_accounts: number
  participating_accounts: number; participating_concurrency: number; participating_concurrency_unlimited: boolean
  concurrency_capacity: number; concurrency_unlimited: boolean; current_concurrency: number | null
}
export interface SharedPoolTier extends SharedPoolCapacity {
  platform: SharedPlatform; tier: string; available: boolean
}
export interface SharedPoolOverview extends SharedPoolCapacity {
  settlement_multiplier: number; platform_rate_bps: number; proxy_rate_bps: number
  updated_at: string; tiers: SharedPoolTier[]
}
export interface SharedAccount {
  id: number; name: string; platform: SharedPlatform; type: 'oauth' | 'apikey'
  subscription_tier?: string | null
  subscription_tier_override?: string
  concurrency: number; enabled: boolean; admin_disabled: boolean; status: string
  error_message: string | null; proxy_mode: 'random' | 'custom'; has_custom_proxy: boolean
  protection_enabled: boolean; group_ids: number[]; groups: { id: number; name: string }[]
  today_earnings: number; total_earnings: number; estimated_earnings: number | null
  platform_rate_bps: number; proxy_rate_bps: number; last_used_at: string | null; created_at: string
  owner_user_id?: number; owner_email?: string
  priority?: number
  codex_ticket_enabled?: boolean
  codex_ticket_required?: boolean
  // 仅支持 BPS 的 OpenAI OAuth 账号返回；options 仅在开启时返回。
  excel_bps_enabled?: boolean
  excel_bps_options?: ExcelBPSOptions
  daily_cooldown?: SharedDailyCooldown
  dispatch_consent?: boolean
  settlement_multiplier?: number | null
}
export interface SharedAccountInput {
  name: string; platform: SharedPlatform; type: 'oauth' | 'apikey'; concurrency: number
  proxy_url?: string; protection_enabled: boolean; enabled: boolean
  credentials?: Record<string, unknown>; confirm_disable?: boolean
  codex_ticket_enabled?: boolean
  excel_bps_enabled?: boolean
  excel_bps_options?: ExcelBPSOptions
  daily_cooldown?: SharedDailyCooldown
  dispatch_consent?: boolean
}
export interface SharedAccountAllocationInput {
  group_ids?: number[]; admin_disabled?: boolean; enabled?: boolean; subscription_tier?: string; priority?: number
}
export type SharedAccountUpdateInput = Pick<SharedAccountInput, 'name' | 'platform' | 'type' | 'concurrency' | 'proxy_url' | 'enabled' | 'protection_enabled' | 'daily_cooldown' | 'excel_bps_enabled' | 'excel_bps_options'>
export interface SharedAccountImportDefaults {
  protection_enabled: boolean; codex_ticket_enabled: boolean; excel_bps_enabled: boolean
  excel_bps_options: ExcelBPSOptions
}
export interface SharedConfig extends SharedSettlementPolicy {
  platforms: SharedPlatform[]; max_concurrency: number; platform_rate_bps: number; proxy_rate_bps: number
  import_defaults?: SharedAccountImportDefaults
}
export interface SharedImportDefaults {
  name?: string; platform?: SharedPlatform; type?: 'oauth' | 'apikey'; concurrency: number
  proxy_url?: string; enabled: boolean; protection_enabled: boolean
  codex_ticket_enabled?: boolean
  excel_bps_enabled?: boolean
  excel_bps_options?: ExcelBPSOptions
  daily_cooldown?: SharedDailyCooldown
  dispatch_consent?: boolean
}
export interface SharedImportInput {
  sources: { name: string; content: string }[]
  defaults: SharedImportDefaults
}
export interface SharedImportResult {
  total: number; created: number; failed: number; warnings: string[]
  items: { index: number; source: string; name: string; account_id?: number; message?: string }[]
}
export interface SharedSettings extends SharedSettlementPolicy {
  platform_rate_bps: number; proxy_rate_bps: number; max_concurrency: number
  default_priority?: number
  default_group_ids: Partial<Record<SharedPlatform, number | number[]>>
  /** Older servers returned one ID; current settings use ordered ID arrays. */
  subscription_group_ids?: Partial<Record<SharedPlatform, Record<string, number | number[]>>>
  subscription_settlement_multipliers?: Partial<Record<SharedPlatform, Record<string, number>>>
}
export interface SharedSummary {
  total_earned: number; available: number; pending: number; transferred: number
  platform_amount: number; billing_amount: number
}
export interface SharedAutoTransferInput {
  enabled: boolean; threshold: number; daily_time: string
}
export interface SharedAutoTransferSettings extends SharedAutoTransferInput {
  timezone: string; last_run_date?: string
}
/** Number of shared accounts contributed by a user for each subscription tier. */
export interface SharedAccountTierStat {
  tier: string
  count: number
}
export interface SharedUserEarnings {
  user_id: number; email: string; account_count: number; earnings_count: number
  account_tiers?: SharedAccountTierStat[]
  billing_amount?: number; total_earned: number; platform_amount?: number
  available: number; pending: number; transferred: number
}
export interface SharedEarningsTransfer {
  id: number; amount: number; balance: number
}
export interface SharedUserRate {
  user_id: number; email: string; platform_rate_bps: number | null; proxy_rate_bps: number | null
  settlement_multiplier?: number | null
}
export interface SharedEarning {
  id: number; account_id: number; account_name?: string; group_id: number; group_name?: string
  owner_user_id?: number; billing_amount: number; owner_amount: number; platform_amount: number
  platform_rate_bps: number; proxy_rate_bps: number; created_at: string
  settlement_amount?: number | null; settlement_multiplier?: number | null
  base_amount?: number | null; spread_amount?: number | null
}
export interface SharedTestEvent { type: string; text?: string; model?: string; success?: boolean; error?: string; image_url?: string; mime_type?: string }
export interface SharedTestInput { model_id?: string; prompt?: string; mode?: string }
export interface SharedAccountUsageInfo extends AccountUsageInfo {
  codex_turn_tickets?: Account['codex_turn_tickets']
  codex_reset_credit_snapshot?: NonNullable<Account['extra']>['codex_reset_credit_snapshot']
}
export type SharedQuotaUsage = Pick<OpenAIQuotaUsage, 'rate_limit_reset_credits' | 'fetched_at'>
export type SharedQuotaRefreshResult = SharedQuotaUsage & { cache_persisted: boolean }
export type SharedQuotaResetResult = Omit<OpenAIQuotaResetResult, 'account' | 'credit' | 'quota'> & { quota?: SharedQuotaUsage | null }

const userPath = '/shared-pool'
const adminPath = '/admin/shared-pool'
export const sharedPoolAPI = {
  overview: async () => (await apiClient.get<SharedPoolOverview>(`${userPath}/overview`)).data,
  pools: async () => (await apiClient.get<SharedPool[]>(`${userPath}/pools`)).data,
  config: async () => (await apiClient.get<SharedConfig>(`${userPath}/config`)).data,
  accounts: async (page = 1) => (await apiClient.get<SharedPage<SharedAccount>>(`${userPath}/accounts`, { params: { page, page_size: 12 } })).data,
  create: async (input: SharedAccountInput) => (await apiClient.post<SharedAccount>(`${userPath}/accounts`, input)).data,
  importAccounts: async (input: SharedImportInput, idempotencyKey: string) => (await apiClient.post<SharedImportResult>(`${userPath}/accounts/import`, input, {
    timeout: 300000, headers: { 'Idempotency-Key': idempotencyKey }
  })).data,
  update: async (id: number, input: SharedAccountUpdateInput) => (await apiClient.put<SharedAccount>(`${userPath}/accounts/${id}`, input)).data,
  getAvailableModels: async (id: number) => (await apiClient.get<ClaudeModel[]>(`${userPath}/accounts/${id}/models`)).data,
  getUsage: async (id: number, source: 'passive' | 'active' = 'active', signal?: AbortSignal, force = false) => (await apiClient.get<SharedAccountUsageInfo>(`${userPath}/accounts/${id}/usage`, { params: { source, ...(force ? { force: true } : {}) }, signal })).data,
  refreshQuota: async (id: number) => (await apiClient.post<SharedQuotaRefreshResult>(`${userPath}/accounts/${id}/quota/refresh`)).data,
  resetQuota: async (id: number) => (await apiClient.post<SharedQuotaResetResult>(`${userPath}/accounts/${id}/reset-quota`, undefined, { timeout: 90_000 })).data,
  remove: async (id: number) => { await apiClient.delete(`${userPath}/accounts/${id}`) },
  enable: async (id: number, enabled: boolean, dispatchConsent?: boolean) => (await apiClient.post<SharedAccount>(`${userPath}/accounts/${id}/enabled`, { enabled, ...(dispatchConsent !== undefined ? { dispatch_consent: dispatchConsent } : {}) })).data,
  protection: async (id: number, enabled: boolean) => (await apiClient.post<SharedAccount>(`${userPath}/accounts/${id}/protection`, { enabled, confirm_disable: !enabled })).data,
  summary: async () => (await apiClient.get<SharedSummary>(`${userPath}/summary`)).data,
  autoTransferSettings: async () => (await apiClient.get<SharedAutoTransferSettings>(`${userPath}/auto-transfer`)).data,
  saveAutoTransferSettings: async ({ enabled, threshold, daily_time }: SharedAutoTransferInput) => (await apiClient.put<SharedAutoTransferSettings>(`${userPath}/auto-transfer`, { enabled, threshold, daily_time })).data,
  earnings: async (page = 1) => (await apiClient.get<SharedPage<SharedEarning>>(`${userPath}/earnings`, { params: { page, page_size: 20 } })).data,
  transfer: async () => (await apiClient.post<SharedEarningsTransfer>(`${userPath}/transfer`)).data,
  oauthStart: async (platform: SharedPlatform, proxy_url?: string, account_id?: number) => (await apiClient.post<{ auth_url: string; session_id: string }>(`${userPath}/oauth/${platform}/start`, { proxy_url, account_id })).data,
  oauthFinish: async (platform: SharedPlatform, input: { session_id: string; code: string; state?: string }) => (await apiClient.post<{ credentials: Record<string, unknown> }>(`${userPath}/oauth/${platform}/finish`, input)).data
}

export const adminSharedPoolAPI = {
  settings: async () => (await apiClient.get<SharedSettings>(`${adminPath}/settings`)).data,
  saveSettings: async (settings: SharedSettings) => (await apiClient.put<SharedSettings>(`${adminPath}/settings`, settings)).data,
  accounts: async (page = 1, search = '') => (await apiClient.get<SharedPage<SharedAccount>>(`${adminPath}/accounts`, { params: { page, page_size: 20, search } })).data,
  allocate: async (id: number, input: SharedAccountAllocationInput) => (await apiClient.put<SharedAccount>(`${adminPath}/accounts/${id}`, input)).data,
  userRates: async () => (await apiClient.get<SharedUserRate[]>(`${adminPath}/user-rates`)).data,
  saveUserRate: async (id: number, input: { platform_rate_bps: number | null; proxy_rate_bps: number | null; settlement_multiplier?: number | null }) => (await apiClient.put<SharedUserRate>(`${adminPath}/user-rates/${id}`, input)).data,
  earnings: async (page = 1, owner_user_id?: number) => (await apiClient.get<SharedPage<SharedEarning>>(`${adminPath}/earnings`, { params: { page, page_size: 20, owner_user_id } })).data,
  userEarnings: async () => (await apiClient.get<SharedUserEarnings[]>(`${adminPath}/user-earnings`)).data,
  transferUserEarnings: async (userId: number) => (await apiClient.post<SharedEarningsTransfer>(`${adminPath}/users/${userId}/transfer`)).data
}

// 必须收到明确的 test_complete；流中断不能被当作连接成功。
export async function testSharedAccount(id: number, onEvent: (event: SharedTestEvent) => void, signal: AbortSignal, input: SharedTestInput = {}): Promise<void> {
  const response = await fetch(buildApiUrl(`${userPath}/accounts/${id}/test`), {
    method: 'POST', credentials: 'include', signal,
    headers: { Authorization: `Bearer ${localStorage.getItem('auth_token') || ''}`, 'Content-Type': 'application/json' },
    body: JSON.stringify(input)
  })
  if (!response.ok) {
    const body = await response.json().catch(() => null)
    throw new Error(body?.message || `HTTP ${response.status}`)
  }
  const reader = response.body?.getReader()
  if (!reader) throw new Error('Empty test response')
  const decoder = new TextDecoder()
  let buffer = ''
  let completed = false
  const consume = (line: string) => {
    if (!line.startsWith('data:')) return
    const raw = line.slice(5).trim()
    if (!raw || raw === '[DONE]') return
    const event: SharedTestEvent = JSON.parse(raw)
    if (event.type === 'test_complete' || event.type === 'error') completed = true
    onEvent(event)
  }
  try {
    while (true) {
      const { done, value } = await reader.read()
      buffer += done ? decoder.decode() : decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''
      lines.forEach(consume)
      if (done) break
    }
    if (buffer) consume(buffer)
    if (!completed) throw new Error('Connection closed before the test completed')
  } finally { reader.releaseLock() }
}
