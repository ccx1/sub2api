// Excel / BPS 子选项；models 为 null 表示对所有模型启用（兼容原设置），空数组表示不对任何模型启用。
export interface ExcelBPSOptions {
  models: string[] | null
  auto_disable_on_403: boolean
  cache_creation_as_input: boolean
  ignore_encrypted_content: boolean
  omit_unsupported_tools: boolean
}

export const EXCEL_BPS_DEFAULT_MODELS = ['gpt-6-astra']

export function defaultExcelBPSOptions(): ExcelBPSOptions {
  return { models: null, auto_disable_on_403: false, cache_creation_as_input: false, ignore_encrypted_content: false, omit_unsupported_tools: false }
}

export function normalizeExcelBPSOptions(options?: Partial<ExcelBPSOptions> | null): ExcelBPSOptions {
  const models = Array.isArray(options?.models)
    ? [...new Set(options.models.filter((model): model is string => typeof model === 'string').map(model => model.trim()).filter(Boolean))]
    : null
  return {
    models,
    auto_disable_on_403: options?.auto_disable_on_403 === true,
    cache_creation_as_input: options?.cache_creation_as_input === true,
    ignore_encrypted_content: options?.ignore_encrypted_content === true,
    omit_unsupported_tools: options?.omit_unsupported_tools === true
  }
}
