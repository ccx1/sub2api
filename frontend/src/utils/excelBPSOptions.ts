// Excel / BPS 子选项；models 为 null 表示对所有模型启用（兼容原设置），空数组表示不对任何模型启用。
export interface ExcelBPSOptions {
  models: string[] | null
  auto_disable_on_403: boolean
  cache_creation_as_input: boolean
  ignore_images: boolean
  ignore_encrypted_content: boolean
  omit_unsupported_tools: boolean
  auto_move_on_403: boolean
  target_group_id: number | null
}

export const EXCEL_BPS_DEFAULT_MODELS = ['gpt-6-astra']

export function defaultExcelBPSOptions(): ExcelBPSOptions {
  return {
    models: null, auto_disable_on_403: false, cache_creation_as_input: false,
    ignore_images: false, ignore_encrypted_content: false, omit_unsupported_tools: false,
    auto_move_on_403: false, target_group_id: null
  }
}

export function normalizeExcelBPSOptions(options?: Partial<ExcelBPSOptions> | null): ExcelBPSOptions {
  const models = Array.isArray(options?.models)
    ? [...new Set(options.models.filter((model): model is string => typeof model === 'string').map(model => model.trim()).filter(Boolean))]
    : null
  const target = options?.target_group_id
  const autoMove = options?.auto_move_on_403 === true
  return {
    models, auto_disable_on_403: options?.auto_disable_on_403 === true,
    cache_creation_as_input: options?.cache_creation_as_input === true,
    ignore_images: options?.ignore_images === true,
    ignore_encrypted_content: options?.ignore_encrypted_content === true,
    omit_unsupported_tools: options?.omit_unsupported_tools === true,
    auto_move_on_403: autoMove,
    target_group_id: autoMove && typeof target === 'number' && Number.isSafeInteger(target) && target >= 0 ? target : null
  }
}

export function sharedExcelBPSOptions(options?: Partial<ExcelBPSOptions> | null): ExcelBPSOptions {
  return { ...normalizeExcelBPSOptions(options), auto_move_on_403: false, target_group_id: null }
}

export function isKnownFreePlan(credentials?: Record<string, unknown> | null): boolean {
  return typeof credentials?.plan_type === 'string' && credentials.plan_type.trim().toLowerCase() === 'free'
}
