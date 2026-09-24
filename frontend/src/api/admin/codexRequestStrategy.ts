import { apiClient } from '../client'

export type CodexRequestStrategyMode = 'native' | 'cookie_previous_ws'
export type CodexRequestStrategyScope = 'dedicated' | 'passthrough' | 'all'
export type CodexRequestStrategyFailureMode = 'fallback' | 'reject'
export type CodexRequestCookieMode = 'preserve' | 'strip_routing' | 'strip_cloudflare' | 'strip_infrastructure'
export type CodexRouteAffinityMode = 'off' | 'prefer' | 'strict'
export type CodexRequestRegionMode = 'preserve' | 'strip' | 'override'
export type CodexRequestTimeContextMode = 'preserve' | 'strip' | 'override'
export type CodexRequestComplianceMode = 'account' | 'preserve' | 'strip'

export interface CodexRequestStrategyPolicy {
  enabled: boolean
  strategy: CodexRequestStrategyMode
  scope: CodexRequestStrategyScope
  failure_mode: CodexRequestStrategyFailureMode
  probe_timeout_seconds: number
  cookie_mode: CodexRequestCookieMode
  route_affinity_mode: CodexRouteAffinityMode
  route_prewarm_connections: number
  route_failure_cooldown_seconds: number
  region_mode: CodexRequestRegionMode
  account_routing_override: string
  residency: string
  time_context_mode: CodexRequestTimeContextMode
  timezone: string
  compliance_mode: CodexRequestComplianceMode
}

export async function getCodexRequestStrategyPolicy(): Promise<CodexRequestStrategyPolicy> {
  const { data } = await apiClient.get<CodexRequestStrategyPolicy>('/admin/settings/codex-request-strategy')
  return data
}

export async function saveCodexRequestStrategyPolicy(policy: CodexRequestStrategyPolicy): Promise<CodexRequestStrategyPolicy> {
  const { data } = await apiClient.put<CodexRequestStrategyPolicy>('/admin/settings/codex-request-strategy', policy)
  return data
}
