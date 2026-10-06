import { describe, expect, it } from 'vitest'
import { astraGatewayPayload, normalizeAstraGateway, resolveAstraDependencies, type AstraGatewaySettings } from '@/api/admin/astraGateway'
import { astraSelectedAccounts, astraSelectionValidation, type AstraSelectionOptions } from '../astraBorrowingSelection'

function settings(): AstraGatewaySettings {
  return normalizeAstraGateway({ cookie_pool: { enabled: true, source_account_ids: [1], target_account_ids: [2] }, ws_session: { enabled: false, account_ids: [] }, revision: 'one' })
}
function options(): AstraSelectionOptions {
  return { accounts: [{ id: 1, name: 'source', group_ids: [7] }, { id: 2, name: 'target', group_ids: [8, 9] }], groups: [7, 8, 9].map(id => ({ id, name: `group-${id}` })), accountsLoaded: true, groupsLoaded: true }
}

describe('Astra borrowing selections', () => {
  it('keeps legacy settings in account mode and clones selection arrays', () => {
    const value = settings()
    expect(value.cookie_pool.source_selection).toBe('accounts')
    expect(value.cookie_pool.target_selection).toBe('accounts')
    value.cookie_pool.source_group_ids = [7]
    const copy = normalizeAstraGateway(value)
    copy.cookie_pool.source_group_ids!.push(8)
    copy.cookie_pool.source_account_ids.push(3)
    expect(value.cookie_pool.source_group_ids).toEqual([7])
    expect(value.cookie_pool.source_account_ids).toEqual([1])
  })

  it('deduplicates members across selected groups and ignores old resolved IDs', () => {
    const value = settings()
    Object.assign(value.cookie_pool, { target_selection: 'groups', target_group_ids: [8, 9], target_account_ids: [999] })
    expect(astraSelectedAccounts(value, options().accounts, 'target')).toEqual([2])
    expect(astraSelectionValidation(value, options())).toBe('')
    expect(astraGatewayPayload(value).cookie_pool).toMatchObject({ target_selection: 'groups', target_group_ids: [8, 9], target_account_ids: [] })
    expect(value.cookie_pool.target_account_ids).toEqual([999])
  })

  it('requires a selected group and eligible members without falling back to all accounts', () => {
    const value = settings()
    value.cookie_pool.target_selection = 'groups'
    expect(astraSelectionValidation(value, options())).toBe('chooseBorrowingGroups')
    value.cookie_pool.target_group_ids = [9]
    const empty = options(); empty.accounts[1].group_ids = [8]
    expect(astraSelectionValidation(value, empty)).toBe('targetAccountsEmpty')
    value.cookie_pool.enabled = false
    expect(astraSelectionValidation(value, empty)).toBe('')
  })

  it('excludes sources before validating target availability and selection limits', () => {
    const value = settings()
    Object.assign(value.cookie_pool, { target_selection: 'groups', target_group_ids: [7] })
    expect(astraSelectedAccounts(value, options().accounts, 'target')).toEqual([])
    expect(astraSelectionValidation(value, options())).toBe('targetAccountsEmpty')
    value.cookie_pool.target_group_ids = [10]
    expect(astraSelectionValidation(value, options())).toBe('missingGroups')
    value.cookie_pool.target_group_ids = Array.from({ length: 101 }, (_, index) => index + 1)
    expect(astraSelectionValidation(value, options())).toBe('groupLimit')
    value.cookie_pool.target_group_ids = [8]
    const large = options(); large.accounts = [large.accounts[0], ...Array.from({ length: 65 }, (_, index) => ({ id: index + 2, name: 'target', group_ids: [8] }))]
    expect(astraSelectionValidation(value, large)).toBe('accountLimit')
  })

  it('subtracts overlapping group members before checking target size and serializing account selections', () => {
    const value = settings()
    Object.assign(value.cookie_pool, { source_selection: 'groups', source_group_ids: [7], target_selection: 'groups', target_group_ids: [8] })
    const list = options()
    list.accounts = Array.from({ length: 70 }, (_, index) => ({ id: index + 1, name: `account-${index}`, group_ids: index < 10 ? [7, 8] : [8] }))
    expect(astraSelectedAccounts(value, list.accounts, 'target')).toHaveLength(60)
    expect(astraSelectionValidation(value, list)).toBe('')
    Object.assign(value.cookie_pool, { source_selection: 'accounts', source_account_ids: [1, 2], target_selection: 'accounts', target_account_ids: [1, 2, 3] })
    value.ws_session = { enabled: true, account_ids: [1, 3] }
    expect(astraGatewayPayload(value).cookie_pool.target_account_ids).toEqual([3])
  })

  it('does not expand target groups for WS but preserves source participants', () => {
    const value = settings()
    Object.assign(value.cookie_pool, { target_selection: 'groups', target_group_ids: [8] })
    value.ws_session = { enabled: true, account_ids: [1], ttl_seconds: 3600 }
    expect(astraSelectionValidation(resolveAstraDependencies(value), options())).toBe('')
    value.ws_session.account_ids = [3]
    const list = options(); list.accounts.push({ id: 3, name: 'outside', group_ids: [] })
    const resolved = resolveAstraDependencies(value)
    expect(resolved.cookie_pool.target_account_ids).toEqual([2])
    expect(astraSelectionValidation(resolved, list)).toBe('wsOutsideTargets')
    value.cookie_pool.target_selection = 'accounts'
    expect(resolveAstraDependencies(value).cookie_pool.target_account_ids).toEqual([2, 3])
  })

  it('blocks self-removal through target scheduling groups', () => {
    const value = settings()
    Object.assign(value.cookie_pool, { target_selection: 'groups', target_group_ids: [8] })
    Object.assign(value, { account_scheduling: true, scheduling_mode: 'groups', scheduling_group_ids: [8] })
    expect(astraSelectionValidation(value, options())).toBe('selectionSchedulingConflict')
    value.scheduling_group_ids = [9]
    expect(astraSelectionValidation(value, options())).toBe('')
  })

  it('blocks incomplete option lists while allowing a disabled policy to be saved', () => {
    const value = settings()
    const incomplete = options(); incomplete.accountsLoaded = false
    expect(astraSelectionValidation(value, incomplete)).toBe('accountsLoadError')
    value.cookie_pool.enabled = false
    expect(astraSelectionValidation(value, incomplete)).toBe('')
    value.selection_error = 'astra_group_resolution_unavailable'
    expect(astraGatewayPayload(value)).not.toHaveProperty('selection_error')
  })
})
