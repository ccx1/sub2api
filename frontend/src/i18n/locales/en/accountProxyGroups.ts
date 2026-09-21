export default {
  scope: 'Random within a proxy group',
  label: 'Proxy group',
  choose: 'Select a proxy group',
  option: '{name} ({count} active / {total} total)',
  loading: 'Loading proxy groups…',
  loadFailed: 'Failed to load proxy groups. Please retry.',
  retry: 'Retry',
  required: 'Select a proxy group',
  empty: 'No proxy groups yet. Create a group and assign proxies in IP management.',
  unavailable: 'The selected proxy group no longer exists. Select another group.',
  unavailableId: 'Proxy group #{id} (unavailable)',
  emptyPool: 'This group has no active proxies. Requests will follow the empty-pool policy below, without falling back to the global pool.',
  dynamicHint: 'Proxy selection uses the current group members. Membership changes in IP management take effect automatically.',
  listScope: 'Group random',
  listLoading: 'Proxy group #{id} (loading)',
  listLoadFailed: 'Proxy group #{id} (failed to load)'
}
