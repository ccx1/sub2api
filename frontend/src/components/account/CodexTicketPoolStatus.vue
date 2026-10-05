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
          >{{ t('admin.accounts.openai.codexTicketUsingStandbyCompact', { time: formatRemaining(remainingSeconds(ticket) ?? ticket.remaining_seconds) }) }}</span>
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
            {{ formatRemaining(remainingSeconds(ticket) ?? ticket.remaining_seconds) }}
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
      <div v-if="ticket.usage_mode === 'aged'" class="flex flex-col text-gray-500 dark:text-gray-400" data-testid="history-ticket-status">
        <span>{{ t('admin.accounts.openai.codexTicketHistoryCapturedAt', { time: strictDateTime(ticket.origin_captured_at) ?? t('admin.accounts.openai.codexTicketHistoryUnknown') }) }}</span>
        <span>{{ t('admin.accounts.openai.codexTicketHistoryRemaining', { time: historyRemaining(ticket) }) }}</span>
        <span v-if="ticket.route_host" data-testid="history-ticket-host">{{ t('admin.accounts.openai.codexTicketHistoryHost', { host: ticket.route_host }) }}</span>
      </div>
      <div class="flex flex-wrap gap-x-1.5 text-gray-500 dark:text-gray-400" data-testid="ticket-harvest-status">
        <span :class="harvestStatusClass(ticket)" :title="ticket.last_attempt_success === false ? ticket.last_attempt_reason : undefined">{{ t('admin.accounts.openai.codexTicketHarvestLast', { status: harvestStatusLabel(ticket) }) }}</span>
        <time v-if="strictDateTime(ticket.last_attempt_at)" :datetime="ticket.last_attempt_at">{{ strictDateTime(ticket.last_attempt_at) }}</time>
      </div>
      <div
        v-if="routeAffinityLabel(ticket)"
        class="flex flex-wrap items-center gap-x-1.5 text-[10px]"
        data-testid="ticket-route-affinity"
        :class="routeAffinityClass(ticket)"
        :title="t('admin.accounts.openai.codexTicketPoolRouteAffinityHint')"
      >
        <span>{{ routeAffinityLabel(ticket) }}</span>
        <span v-if="validCount(ticket.route_affinity_connections)" class="tabular-nums" data-testid="route-affinity-connections">{{ t('admin.accounts.openai.codexTicketPoolRouteAffinityConnections', { count: ticket.route_affinity_connections }) }}</span>
        <span v-if="validExpiry(ticket.route_expires_at)" class="tabular-nums" :title="formatDateTime(ticket.route_expires_at)" data-testid="route-expires">{{ t('admin.accounts.openai.codexTicketPoolRouteExpires', { time: shortExpiry(ticket.route_expires_at!) }) }}</span>
      </div>
      <div
        v-if="routeAffinityLabel(ticket) && ticket.route_node"
        class="flex flex-wrap items-center gap-x-1.5 text-[10px] text-gray-500 dark:text-gray-400"
        data-testid="ticket-route-node"
        :title="t('admin.accounts.openai.codexTicketPoolRouteNodeHint')"
      >
        <span class="font-mono">{{ ticket.route_node }}</span>
        <span v-if="routeNodeLocation(ticket)">{{ routeNodeLocation(ticket) }}</span>
        <span v-if="ticket.route_egress_country" data-testid="route-egress-country">{{ t('admin.accounts.openai.codexTicketPoolRouteEgress', { country: ticket.route_egress_country }) }}</span>
        <span v-if="ticket.route_cross_region" class="text-amber-600 dark:text-amber-400" data-testid="route-cross-region">{{ t('admin.accounts.openai.codexTicketPoolRouteCrossRegion') }}</span>
      </div>
      <div
        v-if="ticket.quality_status"
        class="flex flex-wrap items-center gap-1 text-[10px]"
        data-testid="ticket-quality-status"
        :class="qualityStatusClass(ticket.quality_status)"
        :title="qualityReasonText(ticket.quality_reason)"
      >
        <span>{{ qualityStatusLabel(ticket) }}</span>
        <span v-if="ticket.quality_ticket_replaced" class="text-cyan-600 dark:text-cyan-400" data-testid="ticket-quality-replaced">· {{ t('admin.accounts.openai.codexTicketQualityReplaced') }}</span>
        <span v-if="qualityReasonText(ticket.quality_reason)" class="text-gray-500 dark:text-gray-400" data-testid="ticket-quality-reason">· {{ qualityReasonText(ticket.quality_reason) }}</span>
        <span v-if="ticket.quality_paused">· {{ t('admin.accounts.openai.codexTicketQualityPaused') }}</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Account } from '@/types'
import { formatDateTime } from '@/utils/format'

type TicketStatus = NonNullable<Account['codex_turn_tickets']>[number]
const props = defineProps<{ tickets?: TicketStatus[] | null }>()
const tickets = computed(() => props.tickets ?? [])
const { t } = useI18n()
const now = ref(Date.now())
let countdownTimer: ReturnType<typeof setInterval> | undefined

onMounted(() => {
  countdownTimer = setInterval(() => { now.value = Date.now() }, 1000)
})
onUnmounted(() => {
  if (countdownTimer) clearInterval(countdownTimer)
})

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
const padTimePart = (value: number) => String(value).padStart(2, '0')
function strictDateTime(value?: string) {
  if (!validExpiry(value)) return undefined
  const date = new Date(value!)
  return `${date.getFullYear()}-${padTimePart(date.getMonth() + 1)}-${padTimePart(date.getDate())} ${padTimePart(date.getHours())}:${padTimePart(date.getMinutes())}:${padTimePart(date.getSeconds())}`
}

function historyRemaining(ticket: TicketStatus) {
  if (!validExpiry(ticket.historical_used_at)) {
    return t('admin.accounts.openai.codexTicketHistoryNotUsed')
  }
	const total = remainingSeconds(ticket, ticket.expires_at ?? ticket.primary_expires_at)
  if (total === undefined) {
    return t('admin.accounts.openai.codexTicketHistoryUnknown')
  }
  if (total === 0) return t('admin.accounts.openai.codexTicketHistoryExpired')
  return t('admin.accounts.openai.codexTicketHistoryCountdown', { time: formatCountdown(total) })
}

function remainingSeconds(ticket: TicketStatus, expiresAt = ticket.expires_at): number | undefined {
  if (validExpiry(expiresAt)) return Math.max(0, Math.ceil((Date.parse(expiresAt!) - now.value) / 1000))
  if (!Number.isFinite(ticket.remaining_seconds) || ticket.remaining_seconds <= 0) return undefined
  return Math.floor(ticket.remaining_seconds)
}

function harvestStatusLabel(ticket: TicketStatus) {
  if (!strictDateTime(ticket.last_attempt_at)) return t('admin.accounts.openai.codexTicketHarvestUnknown')
  if (ticket.last_attempt_success === true) return t('admin.accounts.openai.codexTicketHarvestSucceeded')
  if (ticket.last_attempt_success === false) return t('admin.accounts.openai.codexTicketHarvestFailed')
  return t('admin.accounts.openai.codexTicketHarvestUnknown')
}

function harvestStatusClass(ticket: TicketStatus) {
  if (!strictDateTime(ticket.last_attempt_at)) return ''
  if (ticket.last_attempt_success === true) return 'text-emerald-600 dark:text-emerald-400'
  if (ticket.last_attempt_success === false) return 'text-amber-600 dark:text-amber-400'
  return ''
}
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
  if (ticket.usage_mode === 'aged' && !validExpiry(ticket.historical_used_at) &&
    (!ticket.primary_reason || ticket.primary_reason === 'maturing')) {
    return t('admin.accounts.openai.codexTicketHistoryNotUsed')
  }
  if (ticket.primary_ready) {
    const seconds = remainingSeconds(ticket, ticket.primary_expires_at) ?? ticket.primary_remaining_seconds ?? ticket.remaining_seconds
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
    case 'route_cooldown': return t('admin.accounts.openai.codexTicketPrimaryRouteCooldown')
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

function routeAffinityLabel(ticket: TicketStatus) {
  switch (ticket.route_affinity_status) {
    case 'available': return t('admin.accounts.openai.codexTicketPoolRouteAffinityAvailable')
    case 'unavailable': return t('admin.accounts.openai.codexTicketPoolRouteAffinityUnavailable')
    case 'unknown': return t('admin.accounts.openai.codexTicketPoolRouteAffinityUnknown')
    default: return undefined
  }
}

function routeNodeLocation(ticket: TicketStatus) {
  const place = [ticket.route_node_country, ticket.route_node_region].filter(Boolean).join(' ')
  if (!place) return ''
  return ticket.route_macro_region ? `${place} · ${ticket.route_macro_region}` : place
}

function routeAffinityClass(ticket: TicketStatus) {
  if (ticket.route_affinity_status === 'available') return 'text-emerald-600 dark:text-emerald-400'
  if (ticket.route_affinity_status === 'unavailable') return 'text-amber-600 dark:text-amber-400'
  return 'text-gray-500 dark:text-gray-400'
}

function qualityStatusLabel(ticket: TicketStatus) {
  switch (ticket.quality_status) {
    case 'passed': return t('admin.accounts.openai.codexTicketQualityPassed')
    case 'quarantined':
    case 'suspect': return t('admin.accounts.openai.codexTicketQualityFailed')
    case 'running': return t('admin.accounts.openai.codexTicketQualityChecking')
    case 'inconclusive': return t('admin.accounts.openai.codexTicketQualityIncomplete')
    case 'stale': return t('admin.accounts.openai.codexTicketQualityStale')
    case 'skipped': return t('admin.accounts.openai.codexTicketQualitySkipped')
    default: {
      const minutes = qualityQueueMinutes(ticket.quality_next_check_at)
      return minutes === undefined
        ? t('admin.accounts.openai.codexTicketQualityQueued')
        : t('admin.accounts.openai.codexTicketQualityQueuedIn', { minutes })
    }
  }
}

// Minutes until the next automatic check may start; undefined when it is due now.
function qualityQueueMinutes(value?: string) {
  if (!validExpiry(value)) return undefined
  const remaining = Date.parse(value!) - Date.now()
  return remaining > 0 ? Math.max(1, Math.ceil(remaining / 60000)) : undefined
}

function qualityStatusClass(status: NonNullable<TicketStatus['quality_status']>) {
  if (status === 'passed') return 'text-emerald-600 dark:text-emerald-400'
  if (status === 'quarantined' || status === 'suspect') return 'font-medium text-rose-600 dark:text-rose-400'
  if (status === 'running') return 'text-cyan-600 dark:text-cyan-400'
  if (status === 'inconclusive') return 'text-amber-600 dark:text-amber-400'
  return 'text-gray-500 dark:text-gray-400'
}

function qualityReasonText(reason?: string) {
  const key = (reason ?? '').trim()
  if (!key) return undefined
  const path = `admin.accounts.openai.codexTicketQualityReasons.${key}`
  const label = t(path)
  // vue-i18n returns the path itself when the key is missing; show the raw reason then.
  return label === path ? key : label
}

function formatRemaining(seconds: number) {
  const total = Number.isFinite(seconds) ? Math.max(0, Math.floor(seconds)) : 0
  if (total >= 86400) return formatCountdown(total)
  const hours = Math.floor(total / 3600)
  if (hours) return `${hours}h${String(Math.floor(total % 3600 / 60)).padStart(2, '0')}m${String(total % 60).padStart(2, '0')}s`
  return `${Math.floor(total / 60)}m${String(total % 60).padStart(2, '0')}s`
}

function formatCountdown(seconds: number) {
  const total = Math.max(0, Math.floor(seconds))
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor(total % 3600 / 60)
  return `${String(hours).padStart(2, '0')}:${String(minutes).padStart(2, '0')}:${String(total % 60).padStart(2, '0')}`
}
</script>
