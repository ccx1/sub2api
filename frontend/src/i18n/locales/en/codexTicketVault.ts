export default {
  codexTicketVault: {
    menu: 'Ticket vault', title: 'Account ticket vault', refresh: 'Refresh', loading: 'Loading ticket vault…', failed: 'Failed to load ticket vault',
    safeHint: 'The vault shows ticket metadata only (status, timestamps, length, cookie names and a one-way fingerprint). Raw STATE, cookie values and tokens are never shown or returned. Tickets are stored in the per-model pool of the account, sized by the ticket pool capacity setting.',
    overview: 'Vault overview', empty: 'No tickets stored for any model', noSlots: 'No tickets stored for this model', notConfigured: 'Not in the configured ticket models',
    businessSelected: 'Next for traffic', verified: 'Verified', verificationSkipped: 'Verification skipped', unverified: 'Unverified',
    revoke: 'Revoke', revokeAll: 'Revoke all', revoking: 'Revoking…', confirm: 'Confirm revoke', cancel: 'Cancel',
    revokeAllConfirm: 'Revoke every active ticket of this model? Traffic stops using them and harvesting refills the pool.',
    revoked: 'Revoked {count} ticket(s)', revokeNoop: 'Ticket was already revoked', revokeFailed: 'Failed to revoke',
    revokeRemaining: '{count} ticket(s) could not be marked revoked in the database (blocked on this instance). Please retry later.',
    seconds: '{count}s',
    summary: { total: 'Stored', available: 'Available', maturing: 'Maturing' },
    usageModes: { immediate: 'Use immediately', aged: 'Use aged tickets' },
    fields: {
      ticketEnabled: 'Account tickets', harvestEnabled: 'Account harvest', configEnabled: 'Global tickets', proxy: 'Proxy', credentialMode: 'Credential mode',
      poolCapacity: 'Pool capacity', ttl: 'Ticket TTL', cookieTtl: 'Cookie retention', usageMode: 'Usage mode', minAge: 'Minimum ticket age',
      consumeAfterUse: 'Delete after use', failClosed: 'Fail closed', serverTime: 'Server time'
    },
    values: { on: 'On', off: 'Off', available: 'Available', unavailable: 'Unavailable' },
    columns: {
      slot: 'Slot', status: 'Status', fingerprint: 'Fingerprint', credential: 'Credential', node: 'Node', age: 'Age', matureAt: 'Usable at',
      expiresAt: 'Expires at', capturedAt: 'Captured at', harvest: 'Harvest egress', invalidation: 'Invalidation', actions: 'Actions'
    },
    credentialState: 'STATE length {length}', credentialCookies: 'Cookies: {names}', remaining: '{count}s left', origin: 'First captured {time}',
    statuses: {
      available: 'Available', maturing: 'Maturing', revoked: 'Revoked', consumed: 'Used', binding: 'Binding mismatch', credential: 'Credential mode mismatch',
      cookie_missing: 'Cookie missing', expired: 'Expired', unverified: 'Unverified', unavailable: 'Unavailable'
    },
    reasons: { admin_revoked: 'Revoked by admin in ticket vault' },
    sources: { ticket_vault: 'Account ticket vault' }
  }
}
