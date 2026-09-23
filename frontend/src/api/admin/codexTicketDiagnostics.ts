import { apiClient } from '../client'

export interface CodexTicketNetwork {
  response_header_ms?: number
  peer_addr?: string
  http_version?: string
  final_origin?: string
}

export interface CodexTicketModelDeclaration {
  first_model?: string
  first_event?: string
  terminal_model?: string
  terminal_event?: string
  conflict?: boolean
  truncated?: boolean
}

export interface CodexTicketUpstreamError {
  wire_http_status: number
  effective_status: number
  type?: string
  code?: string
  scope?: string
  retry_at?: string
  classification_source?: string
}

export interface CodexTicketSignals {
  safety_buffering_enabled?: boolean
  faster_model?: string
  active_limit?: string
  plan_type?: string
  quota?: Array<{ id: string; used_percent?: number; reset_at?: string; reset_after_seconds?: number; window_minutes?: number; limit_reached?: boolean }>
}

export interface CodexTicketRuntimeStatus {
  state: string
  reason?: string
  model?: string
  proxy_id?: number
  retry_at?: string
  cooldown_until?: string
  attempts_used: number
  max_attempts: number
  round: number
  max_rounds: number
  half_open: boolean
  generation: number
  last_model?: string
  rule_matched?: boolean
  silence_until?: string
  policy_version?: string
  harvest_half_open?: boolean
  harvest_accepted?: boolean
}

export interface CodexTicketPreviewInput {
  model: string
  set: Array<{ name: string; value: string }>
  remove: string[]
}

export interface CodexTicketPreview {
  before_headers: Record<string, string[]>
  after_headers: Record<string, string[]>
  changes: Array<{ name: string; before: string[]; after: string[]; kind: string }>
  validation_errors: Array<{ field: string; message: string }>
  header_sources: Array<{ name: string; source: string; reason: string }>
  sent: false
}

export async function getCodexTicketRuntimeStatus(id: number): Promise<CodexTicketRuntimeStatus> {
  const { data } = await apiClient.get<CodexTicketRuntimeStatus>(`/admin/accounts/${id}/codex-ticket/runtime-status`)
  return data
}

export async function previewCodexTicketRequest(id: number, input: CodexTicketPreviewInput): Promise<CodexTicketPreview> {
  const { data } = await apiClient.post<CodexTicketPreview>(`/admin/accounts/${id}/codex-ticket/request-preview`, input)
  return data
}
