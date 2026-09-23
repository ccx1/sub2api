<template>
  <form class="space-y-3" data-testid="ticket-history-filters" @submit.prevent="apply">
    <div class="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
      <label class="min-w-0 space-y-1 text-xs text-gray-600 dark:text-gray-300">
        <span>{{ t(`${prefix}.result`) }}</span>
        <select v-model="draft.result" class="input w-full" data-testid="filter-result">
          <option value="">{{ t(`${prefix}.filters.all`) }}</option>
          <option value="success">{{ t(`${prefix}.success`) }}</option>
          <option value="failed">{{ t(`${prefix}.failed`) }}</option>
        </select>
      </label>
      <label class="min-w-0 space-y-1 text-xs text-gray-600 dark:text-gray-300">
        <span>{{ t(`${prefix}.outcome`) }}</span>
        <select v-model="draft.outcome" class="input w-full" data-testid="filter-outcome">
          <option value="">{{ t(`${prefix}.filters.all`) }}</option>
          <option v-for="outcome in codexTicketOutcomes" :key="outcome" :value="outcome">{{ t(`admin.accounts.codexTicketOutcome.${outcome}`) }}</option>
        </select>
      </label>
      <label class="min-w-0 space-y-1 text-xs text-gray-600 dark:text-gray-300">
        <span>{{ t(`${prefix}.lifecycle.status`) }}</span>
        <select v-model="draft.ticket_status" class="input w-full" data-testid="filter-ticket-status">
          <option value="">{{ t(`${prefix}.filters.all`) }}</option>
          <option v-for="status in statuses" :key="status" :value="status">{{ t(`${prefix}.lifecycle.statuses.${status}`) }}</option>
        </select>
      </label>
      <label class="min-w-0 space-y-1 text-xs text-gray-600 dark:text-gray-300">
        <span>{{ t(`${prefix}.requestedModel`) }}</span>
        <select v-model="draft.model" class="input w-full" data-testid="filter-model">
          <option value="">{{ t(`${prefix}.filters.all`) }}</option>
          <option v-for="model in options.models" :key="model" :value="model">{{ model }}</option>
        </select>
      </label>
      <label class="min-w-0 space-y-1 text-xs text-gray-600 dark:text-gray-300">
        <span>{{ t(`${prefix}.filters.reason`) }}</span>
        <select v-model="draft.reason" class="input w-full" data-testid="filter-reason">
          <option value="">{{ t(`${prefix}.filters.all`) }}</option>
          <option v-for="reason in options.reasons" :key="reason" :value="reason">{{ reasonLabel(reason) }}</option>
        </select>
      </label>
      <label class="min-w-0 space-y-1 text-xs text-gray-600 dark:text-gray-300">
        <span>{{ t(`${prefix}.filters.startedFrom`) }}</span>
        <input v-model="draft.started_from" type="datetime-local" step="1" class="input w-full min-w-0" data-testid="filter-started-from">
      </label>
      <label class="min-w-0 space-y-1 text-xs text-gray-600 dark:text-gray-300">
        <span>{{ t(`${prefix}.filters.startedTo`) }}</span>
        <input v-model="draft.started_to" type="datetime-local" step="1" class="input w-full min-w-0" data-testid="filter-started-to">
      </label>
      <div class="flex flex-wrap items-end gap-2 sm:col-span-2">
        <button type="submit" class="btn btn-primary" data-testid="filter-apply">{{ t(`${prefix}.filters.apply`) }}</button>
        <button type="button" class="btn btn-secondary" data-testid="filter-reset" @click="reset">{{ t(`${prefix}.filters.reset`) }}</button>
      </div>
    </div>
    <p v-if="invalidRange" role="alert" class="text-xs text-red-600 dark:text-red-400">{{ t(`${prefix}.filters.invalidRange`) }}</p>
    <p class="text-xs text-gray-500 dark:text-gray-400">{{ t(`${prefix}.filters.hint`) }}</p>
  </form>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CodexTicketHistoryFilterOptions, CodexTicketHistoryFilters, CodexTicketStatus } from '@/api/admin/accounts'
import { codexTicketOutcomes } from '@/utils/codexTicketOutcomes'

defineProps<{ options: CodexTicketHistoryFilterOptions }>()
const emit = defineEmits<{ apply: [filters: CodexTicketHistoryFilters] }>()
const { t, te } = useI18n()
const prefix = 'admin.accounts.codexTicketHistory'
const statuses: CodexTicketStatus[] = ['issued', 'invalidated', 'ttl_elapsed', 'not_issued', 'unknown']
const emptyDraft = () => ({ result: '', outcome: '', ticket_status: '', model: '', reason: '', started_from: '', started_to: '' })
const draft = reactive(emptyDraft())
const invalidRange = ref(false)

function reasonLabel(reason: string): string {
  const [kind, code] = reason.split(':')
  const key = `${prefix}.${kind === 'invalidation' ? 'lifecycle.reasons' : 'reasons'}.${code}`
  const label = te(key) ? t(key) : code || reason
  return t(`${prefix}.filters.${kind === 'invalidation' ? 'invalidationReason' : 'attemptReason'}`, { reason: label })
}

function apply(): void {
  const from = draft.started_from ? new Date(draft.started_from) : null
  const to = draft.started_to ? new Date(draft.started_to) : null
  invalidRange.value = Boolean((from && !Number.isFinite(from.getTime())) || (to && !Number.isFinite(to.getTime())) || (from && to && from > to))
  if (invalidRange.value) return
  const filters: CodexTicketHistoryFilters = {}
  if (draft.result) filters.result = draft.result as CodexTicketHistoryFilters['result']
  if (draft.outcome) filters.outcome = draft.outcome
  if (draft.ticket_status) filters.ticket_status = draft.ticket_status as CodexTicketStatus
  if (draft.model) filters.model = draft.model
  if (draft.reason) filters.reason = draft.reason
  if (from) filters.started_from = from.toISOString()
  if (to) filters.started_to = to.toISOString()
  emit('apply', filters)
}

function reset(): void {
  Object.assign(draft, emptyDraft())
  invalidRange.value = false
  emit('apply', {})
}
</script>
