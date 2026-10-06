import type { AstraGatewaySettings } from '@/api/admin/astraGateway'

export interface AstraSelectableAccount { id: number; name: string; group_ids?: number[] }
export interface AstraSelectionOptions {
  accounts: AstraSelectableAccount[]
  groups: { id: number; name: string }[]
  accountsLoaded: boolean
  groupsLoaded: boolean
}
export type AstraSelectionSide = 'source' | 'target'

export function astraSelectedAccounts(value: AstraGatewaySettings, accounts: AstraSelectableAccount[], side: AstraSelectionSide): number[] {
  const pool = value.cookie_pool
  const groups = pool[`${side}_group_ids`] || []
  const selected = pool[`${side}_selection`] === 'groups'
    ? accounts.filter(account => account.group_ids?.some(id => groups.includes(id))).map(account => account.id)
    : pool[`${side}_account_ids`]
  const sources = side === 'target' ? new Set(astraSelectedAccounts(value, accounts, 'source')) : new Set<number>()
  return [...new Set(selected)].filter(id => !sources.has(id))
}

export function astraSelectionValidation(value: AstraGatewaySettings, options: AstraSelectionOptions): string {
  if (!value.cookie_pool.enabled && !value.ws_session.enabled) return ''
  if (value.account_scheduling && value.scheduling_mode === 'groups' && value.cookie_pool.target_selection === 'groups' && value.scheduling_group_ids?.some(id => value.cookie_pool.target_group_ids?.includes(id))) return 'selectionSchedulingConflict'
  if (!options.accountsLoaded) return 'accountsLoadError'
  for (const side of ['source', 'target'] as const) {
    if (value.cookie_pool[`${side}_selection`] !== 'groups') continue
    if (!options.groupsLoaded) return 'groupsLoadError'
    const groups = value.cookie_pool[`${side}_group_ids`] || []
    if (!groups.length) return 'chooseBorrowingGroups'
    if (groups.length > 100) return 'groupLimit'
    if (groups.some(id => !options.groups.some(group => group.id === id))) return 'missingGroups'
  }
  const sources = astraSelectedAccounts(value, options.accounts, 'source')
  const targets = astraSelectedAccounts(value, options.accounts, 'target')
  if (value.cookie_pool.source_selection === 'groups' && !sources.length) return 'groupAccountsEmpty'
  if (!sources.length) return 'chooseBoth'
  if (!targets.length) return 'targetAccountsEmpty'
  if (sources.length > 64 || targets.length > 64) return 'accountLimit'
  const enabled = [...sources, ...targets, ...(value.ws_session.enabled ? value.ws_session.account_ids : [])]
  if (enabled.some(id => !options.accounts.some(account => account.id === id))) return 'missingAccounts'
  if (value.ws_session.enabled && value.cookie_pool.target_selection === 'groups' && value.ws_session.account_ids.some(id => !sources.includes(id) && !targets.includes(id))) return 'wsOutsideTargets'
  return ''
}

export function astraSelectionErrorKey(error: string): string {
  const code = error.split(/[:\s]/)[0]
  const keys: Record<string, string> = {
    astra_selection_invalid: 'selectionInvalid', astra_group_selection_invalid: 'selectionInvalid',
    astra_source_required: 'chooseBoth', astra_target_required: 'targetAccountsEmpty',
    astra_global_ws_disabled: 'globalBlocked', astra_account_unavailable: 'missingAccounts',
    astra_group_unavailable: 'missingGroups', astra_group_accounts_empty: 'groupAccountsEmpty',
    astra_group_accounts_limit: 'accountLimit', astra_account_overlap: 'overlap',
    astra_group_resolution_unavailable: 'groupsLoadError', astra_group_membership_changed: 'membershipChanged',
    astra_ws_outside_targets: 'wsOutsideTargets', astra_selection_scheduling_conflict: 'selectionSchedulingConflict'
  }
  return keys[code] || 'selectionInvalid'
}
