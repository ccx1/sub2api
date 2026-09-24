import { apiClient } from '../client'

export interface AccountImportSettings {
  enabled: boolean
  protection_enabled: boolean
  codex_ticket_enabled: boolean
  proxy_mode: 'preserve' | 'direct' | 'fixed' | 'random'
  proxy_id: number | null
  extra: Record<string, unknown>
}

export function defaultAccountImportSettings(): AccountImportSettings {
  return { enabled: false, protection_enabled: true, codex_ticket_enabled: true, proxy_mode: 'preserve', proxy_id: null, extra: {} }
}

export async function getAccountImportSettings(): Promise<AccountImportSettings> {
  const { data } = await apiClient.get<AccountImportSettings>('/admin/settings/account-import')
  return data
}

export async function saveAccountImportSettings(settings: AccountImportSettings): Promise<AccountImportSettings> {
  const { data } = await apiClient.put<AccountImportSettings>('/admin/settings/account-import', settings)
  return data
}
