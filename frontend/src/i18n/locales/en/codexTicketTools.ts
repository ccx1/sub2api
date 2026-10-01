export default {
  codexTicketAlerts: {
    title: 'Codex credential alert', enable: 'Ticket alerts', enabled: 'Ticket alerts on',
    scopeHint: 'Alerts cover observed credential failures in the current list. Keep this account page open with auto-refresh enabled.',
    desktopHint: 'Desktop and panel alerts are enabled. Click to turn off.',
    panelHint: 'Panel alerts remain available when enabled. Desktop notifications require browser support and permission.',
    message: 'Credentials are unavailable for {count} account models ({models}). Check their ticket status.'
  },
  codexTicketDiagnostics: {
    invalidationSignals: 'Upstream signals at invalidation',
    notRecorded: 'Not recorded', yes: 'Yes', no: 'No',
    response_header_ms: 'Response headers (ms)', peer_addr: 'Connection peer', http_version: 'HTTP version', final_origin: 'Final origin',
    peerHint: 'The peer may be a proxy or CDN. It is not the public egress IP.',
    first_model: 'First declared model', first_event: 'First declaration event', terminal_model: 'Terminal model', terminal_event: 'Terminal event', conflict: 'Model declaration conflict',
    truncated: 'Displayed model declarations were truncated; acceptance checks are unchanged.',
    wire_http_status: 'Wire HTTP status', effective_status: 'Effective error status', type: 'Error type', code: 'Error code', scope: 'Error scope', retry_at: 'Retry time', classification_source: 'Classification source',
    safety_buffering_enabled: 'Safety Buffering', faster_model: 'Faster Model', active_limit: 'Active limit', plan_type: 'Plan type',
    used_percent: 'Used (%)', reset_at: 'Reset time', reset_after_seconds: 'Reset after (seconds)', window_minutes: 'Window (minutes)', limit_reached: 'Limit reached',
    signalsHint: 'Upstream signals are diagnostic evidence, not proof of model downgrade or proxy failure.'
  },
  codexTicketRuntime: {
    title: 'Current harvest status', snapshot: 'Protection at this attempt', loading: 'Loading status…', loadFailed: 'Failed to load status. Refresh to retry.',
    rule_matched: 'Reject-and-silence rule matched', silence_until: 'Harvest proxy silent until', policy_version: 'Protection policy version', harvest_half_open: 'Harvest used a half-open probe', harvest_accepted: 'Harvest candidate accepted',
    state: 'State', reason: 'Reason', attempts: 'Account attempts / limit', round: 'Pool round / limit', model: 'Model', proxy: 'Proxy ID',
    halfOpen: 'Half-open harvest', retryAt: 'Earliest probe time', cooldownUntil: 'Account cooldown until', generation: 'Cycle', yes: 'Yes', no: 'No',
    retryHint: 'This is only the earliest reevaluation or probe opportunity, not a guaranteed recovery time.',
    states: {
      protection_draining: 'Protection draining', lease_expired: 'Attempt lease expired', stale_completion: 'Stale completion ignored',
      half_open_busy: 'Half-open harvest in progress', reservation_expired: 'Reservation expired', controls_changed: 'Configuration changed',
      attempt_not_started: 'Harvest not started', account_retry: 'Account upstream cooldown',
      business_active: 'Business request active; harvesting paused', shared_state_unavailable: 'Shared state unavailable',
      rejection_retry: 'Waiting to retry after policy rejection', rejection_cooldown: 'Consecutive policy rejection limit reached; cooling down',
      idle: 'Idle', disabled: 'Protection disabled', available: 'Available', waiting: 'Waiting for admission', reserved: 'Reserved', running: 'Harvesting',
      half_open: 'Half-open probe', cooldown: 'Account cooldown', account_cooldown: 'Account cooldown', account_busy: 'Account harvest in progress',
      all_proxies_silent: 'All proxies silent', proxy_silent: 'Proxy silent', proxy_half_open: 'Proxy probe already in progress', capacity: 'Waiting for proxy capacity',
      ip_cooling: 'Egress IP cooling down', ip_disabled: 'Egress IP disabled',
      verification_deferred: 'Verification deferred', budget_exhausted: 'Account budget exhausted', unavailable: 'Shared state unavailable',
      recovery_waiting_for_harvest: 'Waiting for an eligible harvest to recover the proxy', model_backoff: 'Model failure backoff', retry_after: 'Upstream retry delay'
    }
  },
  codexTicketPreview: {
    title: 'Request header preview', hint: 'Preview application headers only. No upstream request, token refresh, proxy allocation, budget use, or saved changes. Authentication uses placeholders. Header names and values are validated by the server.',
    model: 'Target model', set: 'Temporarily set headers', remove: 'Temporarily remove headers', name: 'Header name', value: 'Header value', add: 'Add row', delete: 'Remove row',
    submit: 'Generate preview', loading: 'Generating…', failed: 'Preview failed. Please retry.', invalidResponse: 'The response is not a valid unsent preview.',
    notSent: 'No request sent. Changes were not saved or applied.', before_headers: 'Before', after_headers: 'After', changes: 'Changes', noChanges: 'No header changes.', sources: 'Header construction sources'
  },
  codexTicketOutcome: {
    success: 'Ticket issued', ticket_rejected: 'Ticket rejected by policy', model_mismatch: 'Target model mismatch', model_failed: 'Model response failed',
    upstream_error: 'Upstream error', verification_failed: 'Business verification failed', verification_deferred: 'Harvested, verification deferred',
    canceled: 'Canceled', controls_changed: 'Stopped after configuration change', failed: 'Ticket not issued'
  },
  codexTicketNodes: {
    menu: 'Node capture', title: 'Node capture', refresh: 'Refresh', copyJson: 'Copy all JSON', copied: 'Node capture JSON copied',
    rawWarning: 'Testing only: cookies, STATE, Authorization and messages below are shown raw. Do not share screenshots. Probe results are only returned in this response and are never written to the ticket pool, harvest history or quality records.',
    loading: 'Loading node information…', failed: 'Node capture failed', overview: 'Account and egress',
    proxyUnavailable: 'Proxy unavailable', notConfigured: 'Not configured as a ticket model', noSlots: 'No tickets', businessSelected: 'Selected for business',
    usable: 'Usable', unusable: 'Unusable', crossRegion: 'Cross-region node', pending: 'Pending cookies',
    connections: 'WS connection pool', noConnections: 'No WS connections', handshakeHeaders: 'Handshake response headers', truncated: 'truncated',
    sentCookies: 'Sent cookies', receivedCookies: 'Returned cookies (Set-Cookie)', noCookies: 'No cookies',
    excludedByMode: 'Not sent under current cookie mode', decodeError: 'Decode failed',
    probeTitle: 'Node probe', probeHint: 'Sends real probe requests upstream from the selected source and records the sent/returned __oailb node and latency per round to check whether the node is pinned.',
    runProbe: 'Run probe', probing: 'Probing…', businessSlot: 'Current business ticket', roundDetail: 'Round {index} details',
    request: 'Raw request', response: 'Raw response', notRecorded: 'Not recorded', rawJson: 'Raw JSON',
    fields: {
      accountStatus: 'Account status', cookieMode: 'Cookie mode', proxy: 'Current egress', egressCountry: 'Egress country/region', ticketEnabled: 'Ticket / harvest / random proxy',
      credentialMode: 'Credential/session/refresh', pool: 'Ticket pool', strategy: 'Request strategy', routeAffinity: 'Route affinity', serverTime: 'Server time',
      model: 'Model', source: 'Source', slot: 'Ticket slot', count: 'Rounds (1–5)', slotNode: 'Ticket node', duration: 'Total duration', startedAt: 'Started at'
    },
    sources: { ticket: 'Replay ticket cookies', sticky_jar: 'Start empty, keep cookies', empty_jar: 'Empty cookies each round' },
    sourceHints: {
      ticket: 'Uses the selected ticket session and cookies (filtered by the current cookie mode) to see whether the node is kept.',
      sticky_jar: 'Round 1 sends no cookies; later rounds reuse returned cookies to see whether upstream pins one session to one node.',
      empty_jar: 'Every round uses a new session without cookies to observe the natural node distribution.'
    },
    rounds: { index: 'Round', status: 'Status', http: 'HTTP', latency: 'Header / first byte / first delta / total (ms)', node: 'Sent → returned node', outcome: 'Node change', effective: 'Effective node' },
    outcomes: {
      no_route: 'No node cookie', assigned: 'Newly assigned', kept: 'Not returned (kept)', cleared: 'Cleared', unparsed: 'Unparseable',
      refreshed: 'Same node refreshed', changed: 'Node changed', no_response: 'No response', skipped: 'Skipped'
    }
  }
}
