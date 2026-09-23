<template>
  <div class="mb-2 space-y-1 border-b border-gray-200 pb-2 dark:border-dark-600" data-testid="ticket-lifecycle">
    <p class="font-medium" :class="status === 'invalidated' ? 'text-amber-700 dark:text-amber-400' : 'text-gray-700 dark:text-gray-300'">
      {{ t(`${prefix}.status`) }}: {{ t(`${prefix}.statuses.${status}`) }}
    </p>
    <template v-if="attempt.invalidation">
      <p data-testid="ticket-invalidation-reason">{{ t(`${prefix}.reason`) }}: {{ label('reasons', attempt.invalidation.reason) }}</p>
      <p>{{ t(`${prefix}.invalidatedAt`) }}: {{ formatDateTime(attempt.invalidation.invalidated_at) }}</p>
      <p>{{ t(`${prefix}.source`) }}: {{ label('sources', attempt.invalidation.source) }}</p>
      <p v-if="attempt.invalidation.reported_models?.length" class="break-all">{{ t(`${prefix}.reportedModels`) }}: {{ attempt.invalidation.reported_models.join(', ') }}</p>
      <p v-if="attempt.invalidation.returned_ticket_length != null">{{ t(`${prefix}.returnedLength`) }}: {{ attempt.invalidation.returned_ticket_length }}</p>
      <details v-if="attempt.invalidation.signals" class="space-y-2"><summary class="cursor-pointer">{{ t('admin.accounts.codexTicketDiagnostics.invalidationSignals') }}</summary><CodexTicketSignals :signals="attempt.invalidation.signals" /></details>
    </template>
    <p v-else-if="status === 'ttl_elapsed'" data-testid="ticket-invalidation-reason">{{ t(`${prefix}.reason`) }}: {{ t(`${prefix}.reasons.ttl_expired`) }}</p>
    <p v-else-if="status === 'unknown'" class="text-gray-500 dark:text-gray-400">{{ t(`${prefix}.unknownHint`) }}</p>
    <p v-if="attempt.ticket_expires_at">{{ t(`${prefix}.expiresAt`) }}: {{ formatDateTime(attempt.ticket_expires_at) }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CodexTicketHistoryAttempt } from '@/api/admin/accounts'
import { formatDateTime } from '@/utils/format'
import CodexTicketSignals from './CodexTicketSignals.vue'

const props = defineProps<{ attempt: CodexTicketHistoryAttempt }>()
const { t, te } = useI18n()
const prefix = 'admin.accounts.codexTicketHistory.lifecycle'
const status = computed(() => props.attempt.ticket_status || (props.attempt.success ? 'unknown' : 'not_issued'))
function label(kind: string, value: string): string {
  const key = `${prefix}.${kind}.${value}`
  return te(key) ? t(key) : t(`${prefix}.${kind}.unknown`)
}
</script>
