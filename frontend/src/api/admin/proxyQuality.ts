import { apiClient } from '../client'
import type {
  ProxyQualityGuardEvent,
  ProxyQualityGuardOverview,
  ProxyQualityGuardRunResult,
  ProxyQualityGuardSettings
} from '@/types'

export async function getProxyQualityOverview(): Promise<ProxyQualityGuardOverview> {
  const { data } = await apiClient.get<ProxyQualityGuardOverview>('/admin/proxy-quality')
  return data
}

export async function getProxyQualitySettings(): Promise<ProxyQualityGuardSettings> {
  const { data } = await apiClient.get<ProxyQualityGuardSettings>('/admin/proxy-quality/settings')
  return data
}

export async function updateProxyQualitySettings(
  settings: ProxyQualityGuardSettings
): Promise<ProxyQualityGuardSettings> {
  const { data } = await apiClient.put<ProxyQualityGuardSettings>(
    '/admin/proxy-quality/settings',
    settings
  )
  return data
}

export async function getProxyQualityEvents(): Promise<{
  items: ProxyQualityGuardEvent[]
  count: number
}> {
  const { data } = await apiClient.get<{ items: ProxyQualityGuardEvent[]; count: number }>(
    '/admin/proxy-quality/events'
  )
  return data
}

export async function runProxyQualityCheck(): Promise<ProxyQualityGuardRunResult> {
  const { data } = await apiClient.post<ProxyQualityGuardRunResult>('/admin/proxy-quality/run')
  return data
}

export async function resetProxyQualityState(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.post<{ message: string }>(
    `/admin/proxy-quality/proxies/${id}/reset`
  )
  return data
}

const proxyQualityAPI = {
  overview: getProxyQualityOverview,
  getSettings: getProxyQualitySettings,
  updateSettings: updateProxyQualitySettings,
  events: getProxyQualityEvents,
  run: runProxyQualityCheck,
  reset: resetProxyQualityState
}

export default proxyQualityAPI
