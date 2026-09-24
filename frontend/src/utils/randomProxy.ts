import type { Proxy } from '@/types'

export type RandomProxyPoolScope = 'all' | 'selected' | 'group'
export type RandomProxyEmptyPoolPolicy = 'reject' | 'disable' | 'direct'
export type RandomProxyRegionFallback = 'none' | 'pool'

export const normalizeRandomProxyPoolScope = (value: unknown): RandomProxyPoolScope =>
  value === 'selected' || value === 'group' ? value : 'all'

export const normalizeRandomProxyGroupId = (value: unknown): number | null =>
  typeof value === 'number' && Number.isSafeInteger(value) && value > 0 ? value : null

export const normalizeRandomProxyEmptyPoolPolicy = (value: unknown): RandomProxyEmptyPoolPolicy =>
  value === 'disable' || value === 'direct' ? value : 'reject'

export const normalizeRandomProxyRegionFallback = (value: unknown): RandomProxyRegionFallback =>
  value === 'none' ? 'none' : 'pool'

export const normalizeRandomProxyPoolIds = (value: unknown): number[] =>
  Array.isArray(value) ? [...new Set(value.filter((id): id is number => Number.isSafeInteger(id) && id > 0))] : []

export const randomProxyAddress = (proxy: Pick<Proxy, 'host' | 'port'>): string =>
  `${proxy.host.includes(':') ? `[${proxy.host}]` : proxy.host}:${proxy.port}`

export const isValidRandomProxyReuseMinutes = (value: unknown): value is number =>
  typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 && value <= 525600

export const normalizeRandomProxyReuseMinutes = (value: unknown): number =>
  isValidRandomProxyReuseMinutes(value) ? value : 0

export function randomProxyExtra(scope: RandomProxyPoolScope, ids: number[], options: RandomProxyEmptyPoolPolicy | { policy: RandomProxyEmptyPoolPolicy; maxReuseMinutes: number; groupId?: number | null; regionFallback?: RandomProxyRegionFallback }) {
  const { policy, maxReuseMinutes, regionFallback = 'pool' } = typeof options === 'string' ? { policy: options, maxReuseMinutes: 0, regionFallback: 'pool' as RandomProxyRegionFallback } : options
  const groupId = typeof options === 'string' ? null : normalizeRandomProxyGroupId(options.groupId)
  return {
    proxy_mode: 'random',
    random_proxy_pool_scope: scope,
    random_proxy_pool_ids: scope === 'selected' ? normalizeRandomProxyPoolIds(ids) : [],
    random_proxy_group_id: scope === 'group' ? groupId : null,
    random_proxy_empty_pool_policy: policy,
    random_proxy_max_reuse_minutes: maxReuseMinutes,
    random_proxy_region_fallback: normalizeRandomProxyRegionFallback(regionFallback)
  }
}
