import { describe, expect, it, vi } from 'vitest'
const { get, put } = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get, put } }))
import { defaultAccountImportSettings, getAccountImportSettings, saveAccountImportSettings } from '@/api/admin/accountImportSettings'

describe('account import settings API', () => {
  it('reads and writes the complete configuration, preserving false, null and extra fields', async () => {
    const settings = { ...defaultAccountImportSettings(), enabled: true, protection_enabled: false, codex_ticket_enabled: false, extra: { proxy_region_mode: 'off' } }
    get.mockResolvedValue({ data: settings })
    put.mockResolvedValue({ data: settings })
    expect(await getAccountImportSettings()).toEqual(settings)
    expect(get).toHaveBeenCalledWith('/admin/settings/account-import')
    expect(await saveAccountImportSettings(settings)).toEqual(settings)
    expect(put).toHaveBeenCalledWith('/admin/settings/account-import', settings)
  })
})
