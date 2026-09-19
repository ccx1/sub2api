import { apiClient } from '../client'
import type { SpendGuardEvent, SpendGuardOffender, SpendGuardSettings } from '@/types'

export async function getSpendGuardOffenders(): Promise<{ items: SpendGuardOffender[]; count: number }> {
  const { data } = await apiClient.get<{ items: SpendGuardOffender[]; count: number }>(
    '/admin/spend-guard'
  )
  return data
}

export async function getSpendGuardSettings(): Promise<SpendGuardSettings> {
  const { data } = await apiClient.get<SpendGuardSettings>('/admin/spend-guard/settings')
  return data
}

export async function updateSpendGuardSettings(
  settings: SpendGuardSettings
): Promise<SpendGuardSettings> {
  const { data } = await apiClient.put<SpendGuardSettings>(
    '/admin/spend-guard/settings',
    settings
  )
  return data
}

export async function getSpendGuardEvents(): Promise<{ items: SpendGuardEvent[]; count: number }> {
  const { data } = await apiClient.get<{ items: SpendGuardEvent[]; count: number }>(
    '/admin/spend-guard/events'
  )
  return data
}

export async function unfreezeSpendGuardKey(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.post<{ message: string }>(
    `/admin/spend-guard/keys/${id}/unfreeze`
  )
  return data
}

const spendGuardAPI = {
  offenders: getSpendGuardOffenders,
  getSettings: getSpendGuardSettings,
  updateSettings: updateSpendGuardSettings,
  events: getSpendGuardEvents,
  unfreeze: unfreezeSpendGuardKey
}

export default spendGuardAPI
