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
  }
}
