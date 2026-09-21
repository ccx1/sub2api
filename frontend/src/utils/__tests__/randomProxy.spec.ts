import { describe, expect, it } from 'vitest'
import { isValidRandomProxyReuseMinutes, normalizeRandomProxyReuseMinutes, normalizeRandomProxyPoolIds, normalizeRandomProxyPoolScope, normalizeRandomProxyGroupId, randomProxyExtra } from '../randomProxy'

describe('random proxy settings serialization', () => {
  it('defaults legacy accounts to the full pool', () => {
    expect(normalizeRandomProxyPoolScope(undefined)).toBe('all')
    expect(normalizeRandomProxyPoolIds(undefined)).toEqual([])
  })

  it('preserves selected IDs without checking the currently available pool', () => {
    expect(randomProxyExtra('selected', [1, 99, 1], 'disable')).toEqual({
      proxy_mode: 'random', random_proxy_pool_scope: 'selected', random_proxy_pool_ids: [1, 99], random_proxy_group_id: null, random_proxy_empty_pool_policy: 'disable', random_proxy_max_reuse_minutes: 0
    })
  })

  it('drops stale selected IDs when explicitly switching to all', () => {
    expect(randomProxyExtra('all', [99], 'direct').random_proxy_pool_ids).toEqual([])
  })

  it('preserves group scope independently of the current member list', () => {
    expect(normalizeRandomProxyPoolScope('group')).toBe('group')
    expect(randomProxyExtra('group', [99], { policy: 'reject', maxReuseMinutes: 0, groupId: 7 })).toMatchObject({
      random_proxy_pool_scope: 'group', random_proxy_group_id: 7, random_proxy_pool_ids: []
    })
  })

  it.each(['all', 'selected'] as const)('clears the stale group when switching to %s', scope => {
    expect(randomProxyExtra(scope, [99], { policy: 'reject', maxReuseMinutes: 0, groupId: 7 }).random_proxy_group_id).toBeNull()
  })

  it.each([0, -1, 1.5, '7', undefined, null, NaN, Infinity])('keeps invalid group IDs invalid instead of selecting all: %s', groupId => {
    expect(normalizeRandomProxyGroupId(groupId)).toBeNull()
    expect(randomProxyExtra('group', [], { policy: 'reject', maxReuseMinutes: 0 }).random_proxy_pool_scope).toBe('group')
  })

  it('rejects malformed persisted IDs', () => {
    expect(normalizeRandomProxyPoolIds([1, 0, -1, 1.2, '2', null, 2])).toEqual([1, 2])
  })

  it('defaults legacy proxy reuse to unlimited and serializes a configured interval', () => {
    expect(normalizeRandomProxyReuseMinutes(undefined)).toBe(0)
    expect(randomProxyExtra('all', [], { policy: 'reject', maxReuseMinutes: 1440 }).random_proxy_max_reuse_minutes).toBe(1440)
    expect(isValidRandomProxyReuseMinutes(0)).toBe(true)
    expect(isValidRandomProxyReuseMinutes(525600)).toBe(true)
  })

  it.each([-1, 0.5, 525601, NaN, Infinity, '', '60', null])('rejects an invalid reuse interval %s', value => {
    expect(isValidRandomProxyReuseMinutes(value)).toBe(false)
    expect(normalizeRandomProxyReuseMinutes(value)).toBe(0)
  })
})
