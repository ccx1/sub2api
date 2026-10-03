import { apiClient } from '../client'
import type { CodexTicketNodeInfo } from './codexTicketDiagnostics'

// 账号票库只含元数据：后端不返回 STATE、Cookie 值、会话 ID 或账号凭据。
export type CodexTicketVaultStatus =
  | 'available'
  | 'maturing'
  | 'revoked'
  | 'consumed'
  | 'binding'
  | 'credential'
  | 'cookie_missing'
  | 'expired'
  | 'unverified'
  | 'unavailable'

export interface CodexTicketVaultSlot {
  label: string
  fingerprint: string
  status: CodexTicketVaultStatus | string
  business_selected: boolean
  credential_mode?: string
  length: number
  cookie_names: string[] | null
  verified: boolean
  verification_skipped: boolean
  captured_at?: string
  origin_captured_at?: string
  age_seconds: number
  mature_at?: string
  expires_at?: string
  remaining_seconds: number
  state_expires_at?: string
  revalidate_at?: string
  revalidated_at?: string
  harvest_proxy_name?: string
  harvest_country?: string
  route_node?: CodexTicketNodeInfo
  attempts: number
  invalidation?: { reason: string; source: string; invalidated_at?: string }
}

export interface CodexTicketVaultModel {
  model: string
  configured: boolean
  capacity?: number
  total: number
  available: number
  maturing: number
  slots: CodexTicketVaultSlot[] | null
}

export interface CodexTicketVault {
  account_id: number
  account_name: string
  ticket_enabled: boolean
  harvest_enabled: boolean
  config_enabled: boolean
  proxy_available: boolean
  credential_mode?: string
  pool_capacity: number
  account_pool_capacity?: number
  ttl_seconds: number
  cookie_ttl_seconds: number
  policy: {
    usage_mode: 'immediate' | 'aged' | string
    min_ticket_age_seconds: number
    historical_ticket_validity_seconds?: number
    consume_after_use: boolean
    fail_closed: boolean
  }
  models: CodexTicketVaultModel[] | null
  server_time: string
}

export type CodexTicketVaultRevokeInput =
  | { model: string; fingerprint: string; all?: never }
  | { model: string; all: true; fingerprint?: never }

export interface CodexTicketVaultRevokeResult {
  account_id: number
  model: string
  revoked: number
  remaining: number
}

export async function getCodexTicketVault(id: number): Promise<CodexTicketVault> {
  const { data } = await apiClient.get<CodexTicketVault>(`/admin/accounts/${id}/codex-ticket/vault`)
  return data
}

export async function revokeCodexTicketVault(id: number, input: CodexTicketVaultRevokeInput): Promise<CodexTicketVaultRevokeResult> {
  const { data } = await apiClient.post<CodexTicketVaultRevokeResult>(`/admin/accounts/${id}/codex-ticket/vault/revoke`, input)
  return data
}
