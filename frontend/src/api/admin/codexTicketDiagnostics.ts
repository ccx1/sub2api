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

// 节点采集：测试阶段后端原样返回 Cookie/STATE，确认方案后再补脱敏。
export interface CodexTicketNodeInfo {
  host: string
  recognized: boolean
  name?: string
  number?: number
  country?: string
  region?: string
  macro_region?: string
}

export interface CodexTicketNodeCookie {
  name: string
  value: string
  domain?: string
  path?: string
  expires_at?: string
  max_age?: number
  secure?: boolean
  http_only?: boolean
  same_site?: string
  excluded_by_mode?: boolean
  node?: CodexTicketNodeInfo
  claims?: Record<string, unknown>
  claim_expires_at?: string
  decode_error?: string
}

export interface CodexTicketNodeSlot {
  label: string
  business_selected: boolean
  usable: boolean
  revoked: boolean
  verified: boolean
  verification_skipped: boolean
  credential_mode?: string
  session_id?: string
  state?: string
  state_length: number
  egress_matches_current?: boolean
  harvest_proxy_id?: number
  harvest_proxy_name?: string
  harvest_country?: string
  harvest_egress_differs?: boolean
  captured_at?: string
  origin_captured_at?: string
  expires_at?: string
  hard_expires_at?: string
  state_expires_at?: string
  revalidate_at?: string
  revalidated_at?: string
  route_expires_at?: string
  attempts: number
  route_node?: CodexTicketNodeInfo
  sent_node?: CodexTicketNodeInfo
  cross_region: boolean
  route_fingerprint?: string
  invalidation?: import('./accounts').CodexTicketInvalidation | null
  cookies: CodexTicketNodeCookie[]
}

export interface CodexTicketNodePending {
  expires_at?: string
  ticket_label?: string
  ticket_node?: CodexTicketNodeInfo
  candidate_node?: CodexTicketNodeInfo
  header_changed: boolean
  candidate_captured_at?: string
  candidate_expires_at?: string
  cookies: CodexTicketNodeCookie[]
}

export interface CodexTicketNodeModel {
  model: string
  configured: boolean
  slots: CodexTicketNodeSlot[]
  pending?: CodexTicketNodePending
}

export interface CodexTicketNodeConnection {
  id: string
  model?: string
  routing_affinity?: string
  route_fingerprint?: string
  strict: boolean
  has_receipt: boolean
  slot_label?: string
  created_at?: string
  last_used_at?: string
  leased: boolean
  leased_before: boolean
  prewarmed: boolean
  closed: boolean
  unusable: boolean
  sent_node?: CodexTicketNodeInfo
  received_node?: CodexTicketNodeInfo
  sent_cookies: CodexTicketNodeCookie[]
  received_cookies: CodexTicketNodeCookie[]
  handshake_headers: Record<string, string[]> | null
  handshake_headers_truncated?: boolean
}

export interface CodexTicketNodeSnapshot {
  account_id: number
  account_name: string
  account_status: string
  schedulable: boolean
  ticket_enabled: boolean
  harvest_enabled: boolean
  random_proxy: boolean
  proxy_available: boolean
  proxy?: import('./accounts').CodexTicketHistoryProxy
  egress_country?: string
  egress_macro_region?: string
  cookie_mode: string
  config: {
    enabled: boolean
    credential_mode?: string
    session_mode?: string
    refresh_strategy?: string
    pool_capacity: number
    cookie_ttl_seconds: number
    ttl_seconds: number
    models: string[] | null
  }
  strategy: {
    enabled: boolean
    applies: boolean
    strategy?: string
    scope?: string
    route_affinity_mode?: string
    route_prewarm_connections: number
    cookie_mode?: string
  }
  models: CodexTicketNodeModel[] | null
  connections: CodexTicketNodeConnection[] | null
  server_time: string
}

export type CodexTicketNodeProbeSource = 'ticket' | 'empty_jar' | 'sticky_jar'

export interface CodexTicketNodeProbeInput {
  model: string
  source: CodexTicketNodeProbeSource
  slot?: string
  count: number
}

export interface CodexTicketNodeProbeRound {
  index: number
  status: 'ok' | 'failed' | 'skipped'
  reason?: string
  session_id?: string
  started_at?: string
  http_status?: number
  header_latency_ms: number
  first_byte_ms?: number
  first_delta_ms?: number
  total_latency_ms: number
  node_outcome: string
  sent_node?: CodexTicketNodeInfo
  received_node?: CodexTicketNodeInfo
  effective_node?: CodexTicketNodeInfo
  sent_cookies: CodexTicketNodeCookie[] | null
  received_cookies: CodexTicketNodeCookie[] | null
  turn_state_length: number
  response_id?: string
  exchange?: import('./accounts').CodexTicketExchange | null
}

export interface CodexTicketNodeProbeResult {
  account_id: number
  model: string
  source: CodexTicketNodeProbeSource
  slot?: string
  slot_node?: CodexTicketNodeInfo
  cookie_mode: string
  proxy?: import('./accounts').CodexTicketHistoryProxy
  egress_country?: string
  node_counts: Record<string, number> | null
  rounds: CodexTicketNodeProbeRound[] | null
  started_at: string
  duration_ms: number
}

export async function getCodexTicketNodes(id: number): Promise<CodexTicketNodeSnapshot> {
  const { data } = await apiClient.get<CodexTicketNodeSnapshot>(`/admin/accounts/${id}/codex-ticket/nodes`)
  return data
}

export async function probeCodexTicketNodes(id: number, input: CodexTicketNodeProbeInput): Promise<CodexTicketNodeProbeResult> {
  // 后端整批预算 55s，留出代理与 step-up 重试余量。
  const { data } = await apiClient.post<CodexTicketNodeProbeResult>(`/admin/accounts/${id}/codex-ticket/node-probe`, input, { timeout: 120_000 })
  return data
}

export async function getCodexTicketRuntimeStatus(id: number): Promise<CodexTicketRuntimeStatus> {
  const { data } = await apiClient.get<CodexTicketRuntimeStatus>(`/admin/accounts/${id}/codex-ticket/runtime-status`)
  return data
}

export async function previewCodexTicketRequest(id: number, input: CodexTicketPreviewInput): Promise<CodexTicketPreview> {
  const { data } = await apiClient.post<CodexTicketPreview>(`/admin/accounts/${id}/codex-ticket/request-preview`, input)
  return data
}
