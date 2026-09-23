<template>
  <div v-if="tickets.length" class="mb-1 max-w-64 space-y-1" data-testid="codex-ticket-pool">
    <div class="text-[10px] font-medium text-gray-500 dark:text-gray-400" :title="t('admin.accounts.openai.codexTicketPoolHint')">
      {{ t('admin.accounts.openai.codexTicketPool') }}
    </div>
    <div v-for="ticket in tickets" :key="ticket.model" class="min-w-0 text-[10px] leading-4" data-testid="ticket-model">
      <div class="flex flex-wrap items-center gap-x-1.5 gap-y-0.5">
        <span class="max-w-28 truncate font-medium text-gray-600 dark:text-gray-300" :title="ticket.model">{{ shortModel(ticket.model) }}</span>
        <template v-if="hasCount(ticket)">
          <span
            v-if="ticket.using_standby && ticket.ready"
            data-testid="current-status"
            class="font-medium text-amber-600 dark:text-amber-400"
          >{{ t('admin.accounts.openai.codexTicketUsingStandbyCompact', { time: formatRemaining(ticket.remaining_seconds) }) }}</span>
          <span
            v-if="hasPrimarySummary(ticket)"
            data-testid="primary-status"
            class="font-medium"
            :class="primaryStatusClass(ticket)"
            :title="primaryStatusTitle(ticket)"
          >{{ ticket.using_standby ? primaryUnavailableLabel(ticket) : primaryStatusLabel(ticket) }}</span>
          <span class="font-medium tabular-nums" :class="ticket.available_count! > 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-gray-500 dark:text-gray-400'" data-testid="pool-available">
            {{ t(availabilityKey(ticket), { count: ticket.available_count, capacity: ticket.capacity }) }}
          </span>
          <span v-if="ticket.available_count === 0" :class="ticket.blocked ? 'text-amber-600 dark:text-amber-400' : 'text-gray-500 dark:text-gray-400'">
            {{ t(ticket.blocked ? 'admin.accounts.openai.codexTicketPoolPaused' : 'admin.accounts.openai.codexTicketPoolEmpty') }}
          </span>
        </template>
        <template v-else>
          <span v-if="ticket.ready" class="text-emerald-600 dark:text-emerald-400" :title="ticket.using_standby ? standbyExpiry(ticket) : undefined">
            <span v-if="ticket.using_standby !== undefined">{{ t(ticket.using_standby ? 'admin.accounts.openai.codexTurnTicketUsingStandby' : 'admin.accounts.openai.codexTurnTicketPrimary') }} </span>
            {{ formatRemaining(ticket.remaining_seconds) }}
          </span>
          <span v-else-if="ticket.blocked" class="text-amber-600 dark:text-amber-400">{{ t('admin.accounts.openai.codexTurnTicketPaused') }}</span>
          <span v-else class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.codexTurnTicketMissing') }}</span>
          <span v-if="ticket.standby_ready && !ticket.using_standby" class="text-cyan-600 dark:text-cyan-400" :title="standbyExpiry(ticket)">{{ t('admin.accounts.openai.codexTurnTicketStandbyReady') }}</span>
        </template>
        <span
          v-if="credentialStatusLabel(ticket)"
          data-testid="credential-status"
          class="rounded border border-current/20 px-1 font-medium"
          :class="credentialStatusClass(ticket)"
          :title="credentialStatusTitle(ticket)"
        >{{ credentialStatusLabel(ticket) }}</span>
      </div>
      <div v-if="hasCount(ticket) && ticket.available_count! > 0" class="flex flex-wrap gap-x-2 text-gray-500 dark:text-gray-400">
        <span v-if="validCount(ticket.reserve_count)" class="text-cyan-600 dark:text-cyan-400" data-testid="pool-reserve">{{ t('admin.accounts.openai.codexTicketPoolReserve', { count: ticket.reserve_count }) }}</span>
        <span v-if="validCount(ticket.expiring_count) && ticket.expiring_count! > 0" class="text-amber-600 dark:text-amber-400" data-testid="pool-expiring">{{ t('admin.accounts.openai.codexTicketPoolExpiring', { count: ticket.expiring_count }) }}</span>
        <span v-if="validExpiry(ticket.next_expires_at)" class="break-words" :title="formatDateTime(ticket.next_expires_at)" data-testid="pool-expiry">{{ t('admin.accounts.openai.codexTicketPoolNextExpiry', { time: shortExpiry(ticket.next_expires_at!) }) }}</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Account } from '@/types'
import { formatDateTime } from '@/utils/format'

type TicketStatus = NonNullable<Account['codex_turn_tickets']>[number]
const props = defineProps<{ tickets?: TicketStatus[] | null }>()
const tickets = computed(() => props.tickets ?? [])
const { t } = useI18n()
const validCount = (count: unknown): count is number => typeof count === 'number' && Number.isInteger(count) && count >= 0
const hasCount = (ticket: TicketStatus) => validCount(ticket.available_count)
const hasPrimarySummary = (ticket: TicketStatus) => ticket.primary_present !== undefined || ticket.primary_ready !== undefined || ticket.primary_reason !== undefined
const hasCapacity = (ticket: TicketStatus) => validCount(ticket.capacity) && ticket.capacity > 0
function availabilityKey(ticket: TicketStatus) {
  if (!hasCapacity(ticket)) return 'admin.accounts.openai.codexTicketPoolCount'
  return ticket.available_count! > ticket.capacity!
    ? 'admin.accounts.openai.codexTicketPoolAboveCapacity'
    : 'admin.accounts.openai.codexTicketPoolAvailable'
}
const validExpiry = (value?: string) => !!value && Number.isFinite(Date.parse(value))
const shortExpiry = (value: string) => formatDateTime(value, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false })
const shortModel = (model: string) => ({ 'gpt-6-astra': 'astra', 'gpt-5.6-sol': 'sol' })[model] ?? model
const standbyExpiry = (ticket: TicketStatus) => validExpiry(ticket.standby_expires_at)
  ? t('admin.accounts.openai.codexTurnTicketStandbyExpires', { time: formatDateTime(ticket.standby_expires_at) }) : undefined

function credentialStatusLabel(ticket: TicketStatus) {
  switch (ticket.credential_state) {
    case 'available': return t('admin.accounts.openai.codexTicketPoolCredentialAvailable')
    case 'revalidation_required': return t('admin.accounts.openai.codexTicketPoolCredentialRevalidation')
    case 'expired': return t('admin.accounts.openai.codexTicketPoolCredentialExpired')
    case 'revoked': return t('admin.accounts.openai.codexTicketPoolCredentialRevoked')
    case 'missing': return t('admin.accounts.openai.codexTicketPoolCredentialMissing')
    default: return undefined
  }
}

function credentialStatusClass(ticket: TicketStatus) {
  switch (ticket.credential_state) {
    case 'available': return 'text-emerald-600 dark:text-emerald-400'
    case 'revalidation_required': return 'text-amber-600 dark:text-amber-400'
    case 'expired':
    case 'revoked': return 'text-rose-600 dark:text-rose-400'
    default: return 'text-gray-500 dark:text-gray-400'
  }
}

function credentialStatusTitle(ticket: TicketStatus) {
  const parts = []
  if (ticket.revalidation_required) parts.push(t('admin.accounts.openai.codexTicketPoolRevalidationHint'))
  if (validExpiry(ticket.revalidate_at)) {
    parts.push(t('admin.accounts.openai.codexTicketPoolRevalidateAt', { time: formatDateTime(ticket.revalidate_at) }))
  }
  return parts.join(' ') || undefined
}

function primaryStatusLabel(ticket: TicketStatus) {
  if (ticket.primary_ready) {
    const seconds = ticket.primary_remaining_seconds ?? ticket.remaining_seconds
    return ticket.using_standby
      ? t('admin.accounts.openai.codexTicketUsingStandbyCompact', { time: formatRemaining(seconds) })
      : t('admin.accounts.openai.codexTicketPrimaryReady', { time: formatRemaining(seconds) })
  }
  return primaryUnavailableLabel(ticket)
}

function primaryUnavailableLabel(ticket: TicketStatus) {
  switch (ticket.primary_reason) {
    case 'revoked': return t('admin.accounts.openai.codexTicketPrimaryRevoked')
    case 'expired': return t('admin.accounts.openai.codexTicketPrimaryExpired')
    case 'credential': return t('admin.accounts.openai.codexTicketPrimaryCredential')
    case 'cookie_missing': return t('admin.accounts.openai.codexTicketPrimaryCookieMissing')
    case 'missing': return t('admin.accounts.openai.codexTicketPrimaryMissing')
    default: return t('admin.accounts.openai.codexTicketPrimaryUnavailable')
  }
}

function primaryStatusClass(ticket: TicketStatus) {
  if (ticket.primary_ready && !ticket.using_standby) return 'text-emerald-600 dark:text-emerald-400'
  if (ticket.primary_ready || ticket.primary_reason === 'expired') return 'text-amber-600 dark:text-amber-400'
  return 'text-gray-500 dark:text-gray-400'
}

function primaryStatusTitle(ticket: TicketStatus) {
  if (!validExpiry(ticket.primary_expires_at)) return undefined
  return t('admin.accounts.openai.codexTicketPrimaryExpires', { time: formatDateTime(ticket.primary_expires_at) })
}

function formatRemaining(seconds: number) {
  const total = Number.isFinite(seconds) ? Math.max(0, Math.floor(seconds)) : 0
  return `${Math.floor(total / 60)}m${String(total % 60).padStart(2, '0')}s`
}
</script>
