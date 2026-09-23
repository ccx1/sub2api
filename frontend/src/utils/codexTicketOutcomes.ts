export const codexTicketOutcomes = [
  'success', 'ticket_rejected', 'model_mismatch', 'model_failed', 'upstream_error',
  'verification_failed', 'verification_deferred', 'canceled', 'controls_changed', 'failed'
] as const

export function codexTicketOutcomeTone(outcome: string | undefined, success: boolean): string {
  if (success) return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-400'
  if (outcome === 'verification_deferred' || outcome === 'ticket_rejected') return 'bg-amber-50 text-amber-700 dark:bg-amber-500/10 dark:text-amber-300'
  if (outcome === 'canceled' || outcome === 'controls_changed') return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
  return 'bg-red-50 text-red-700 dark:bg-red-500/10 dark:text-red-400'
}
