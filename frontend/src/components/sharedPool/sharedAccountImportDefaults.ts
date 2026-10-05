import type { SharedAccountImportDefaults, SharedImportDefaults } from '@/api/sharedPool'
import { defaultExcelBPSOptions, sharedExcelBPSOptions } from '@/utils/excelBPSOptions'
import { normalizePrismBrowserOptions } from '@/utils/prismBrowserOptions'

export function resolveSharedAccountImportDefaults(
  globalDefaults?: SharedAccountImportDefaults,
  overrides: Partial<SharedImportDefaults> = {}
): Omit<SharedAccountImportDefaults, 'codex_ticket_enabled'> {
  // BPS 开关与子选项整组覆盖，避免当次设置混入全局模型范围或 403 策略。
  const hasBPSOverride = overrides.excel_bps_enabled !== undefined || overrides.excel_bps_options !== undefined
  const bps = hasBPSOverride ? overrides : globalDefaults
  const hasPrismOverride = overrides.prism_browser_enabled !== undefined || overrides.prism_browser_models !== undefined
  const prism = hasPrismOverride ? overrides : globalDefaults
  return {
    protection_enabled: overrides.protection_enabled ?? globalDefaults?.protection_enabled ?? true,
    excel_bps_enabled: bps?.excel_bps_enabled ?? false,
    excel_bps_options: sharedExcelBPSOptions(bps?.excel_bps_options ?? defaultExcelBPSOptions()),
    prism_browser_enabled: prism?.prism_browser_enabled ?? false,
    prism_browser_models: normalizePrismBrowserOptions({ models: prism?.prism_browser_models }).models
  }
}
