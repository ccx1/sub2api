export const PRISM_BROWSER_MODELS = ['gpt-6.1-sol', 'gpt-5.6-sol', 'gpt-5.6-terra', 'gpt-6-luna'] as const

export interface PrismBrowserOptions { models: string[] }

export function defaultPrismBrowserOptions(): PrismBrowserOptions {
  return { models: [...PRISM_BROWSER_MODELS] }
}

export function normalizePrismBrowserOptions(value?: Partial<PrismBrowserOptions> | null): PrismBrowserOptions {
  if (value?.models === undefined) return defaultPrismBrowserOptions()
  if (!Array.isArray(value.models)) return { models: [] }
  return { models: PRISM_BROWSER_MODELS.filter(model => value.models?.includes(model)) }
}
