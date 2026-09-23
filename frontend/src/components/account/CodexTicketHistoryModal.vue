<template>
  <BaseDialog :show="show" :title="t('admin.accounts.codexTicketHistory.title')" width="extra-wide" :close-on-escape="!selectedAttempt && !previewOpen" @close="closeHistory">
    <div class="min-w-0 space-y-4">
      <p class="break-all text-sm font-medium text-gray-900 dark:text-gray-100">{{ account?.name }}</p>
      <CodexTicketRuntimePanel v-if="show && account" :account-id="account.id" />
      <button v-if="account" type="button" class="btn btn-secondary" data-testid="open-ticket-preview" @click="openPreview">{{ t('admin.accounts.codexTicketPreview.title') }}</button>
      <CodexTicketHistoryFilters :key="`${account?.id}-${show}`" :options="filterOptions" @apply="applyFilters" />
      <div v-if="loading" class="flex justify-center py-12"><LoadingSpinner /></div>
      <div v-else-if="error" role="alert" class="space-y-3 rounded-lg border border-red-200 p-4 dark:border-red-500/30">
        <p class="text-sm text-red-600 dark:text-red-400">{{ t('admin.accounts.codexTicketHistory.loadFailed') }}</p>
        <button type="button" class="btn btn-secondary" @click="loadHistory()">{{ t('admin.accounts.codexTicketHistory.retry') }}</button>
      </div>
      <template v-else-if="history">
        <dl class="grid grid-cols-3 gap-2 sm:gap-4">
          <div v-for="metric in metrics" :key="metric.key" class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
            <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t(`admin.accounts.codexTicketHistory.${metric.key === 'failed' ? 'unsuccessful' : metric.key}`) }}</dt>
            <dd class="mt-1 break-all text-xl font-semibold tabular-nums" :class="metric.color" :data-testid="`ticket-history-${metric.key}`">
              {{ history.summary[metric.key].toLocaleString(locale) }}
            </dd>
          </div>
        </dl>
        <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">
          <span class="mb-1 block">{{ t('admin.accounts.codexTicketHistory.summaryCompatibility') }}</span>
          {{ t('admin.accounts.codexTicketHistory.retainedHint', { limit: history.retained_limit || 100 }) }}
          <span class="mt-1 block">{{ t('admin.accounts.codexTicketHistory.exchangeRetainedHint', { limit: history.exchange_retained_limit || 10 }) }}</span>
          <span v-if="history.summary.last_attempt_at" class="mt-1 block">
            {{ t('admin.accounts.codexTicketHistory.lastAttempt') }}: {{ formatDateTime(history.summary.last_attempt_at) }}
          </span>
        </p>
        <div v-if="history.summary.outcome_counts" class="space-y-1 text-xs">
          <p v-if="history.summary.classification_started_at" class="text-gray-500">{{ t('admin.accounts.codexTicketHistory.classificationSince', { time: formatDateTime(history.summary.classification_started_at) }) }}</p>
          <div class="flex flex-wrap gap-2"><span v-for="(count, outcome) in history.summary.outcome_counts" :key="outcome" class="rounded border border-gray-200 px-2 py-1 dark:border-dark-600">{{ outcomeLabel(outcome) }}: {{ count }}</span></div>
        </div>
        <div class="flex flex-wrap items-center gap-3">
          <button type="button" class="btn btn-secondary" data-testid="retry-tickets" :disabled="loading || retrying || !account" @click="retryEligibleTickets()">
            {{ retrying ? t('admin.accounts.codexTicketHistory.retrying') : t('admin.accounts.codexTicketHistory.retryEligible') }}
          </button>
          <span v-if="retryMessage" class="text-sm text-gray-600 dark:text-gray-300" role="status">{{ retryMessage }}</span>
        </div>
        <p v-if="history.items.length === 0" class="py-10 text-center text-sm text-gray-500 dark:text-gray-400">
          {{ t(`admin.accounts.codexTicketHistory.${Object.keys(filters).length ? 'filteredEmpty' : 'empty'}`) }}
        </p>
        <div v-else class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-600" data-testid="ticket-history-table">
          <table class="w-full min-w-[1120px] table-fixed text-left text-sm">
            <caption class="sr-only">{{ t('admin.accounts.codexTicketHistory.title') }}</caption>
            <thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-800 dark:text-gray-400">
              <tr>
                <th v-for="column in columns" :key="column.key" scope="col" class="px-3 py-3 font-medium" :style="{ width: column.width }">
                  {{ t(`admin.accounts.codexTicketHistory.${column.key}`) }}
                </th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="attempt in history.items" :key="attempt.id" :data-testid="`ticket-history-row-${attempt.id}`" class="align-top text-gray-700 dark:text-gray-300">
                <td class="px-3 py-3">
                  <time :datetime="attempt.started_at" class="block">{{ formatDateTime(attempt.started_at) }}</time>
                  <span class="mt-1 block text-xs text-gray-500">{{ attemptDuration(attempt) }}</span>
                </td>
                <td class="break-all px-3 py-3 text-xs leading-5">
                  <dl class="space-y-2">
                    <div><dt class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.codexTicketHistory.requestedModel') }}</dt><dd class="font-mono">{{ attempt.model || '—' }}</dd></div>
                    <div v-for="stage in modelStages" :key="stage.key">
                      <dt class="text-gray-500 dark:text-gray-400">{{ t(`admin.accounts.codexTicketHistory.${stage.label}`) }}</dt>
                      <dd class="font-mono" :data-testid="`${stage.key}-models`">{{ reportedModels(attempt[stage.key], attempt[stage.status]) }}</dd>
                      <span v-if="attempt[stage.key]?.models_truncated" class="text-amber-600 dark:text-amber-400">{{ t('admin.accounts.codexTicketHistory.modelsTruncated') }}</span>
                    </div>
                  </dl>
                </td>
                <td class="px-3 py-3">
                  <span class="inline-flex rounded px-2 py-0.5 text-xs font-medium" :class="codexTicketOutcomeTone(attempt.outcome, attempt.success)">
                    {{ attempt.outcome ? outcomeLabel(attempt.outcome) : t(`admin.accounts.codexTicketHistory.${attempt.success ? 'success' : 'failed'}`) }}
                  </span>
                </td>
                <td class="break-words px-3 py-3 text-xs leading-5" data-testid="attempt-details">
                  <CodexTicketLifecycle :attempt="attempt" />
                  <p v-if="!attempt.success" class="mb-1 font-medium">{{ failureReason(attempt.reason) }}</p>
                  <details v-if="attempt.protection" class="mb-2"><summary class="cursor-pointer">{{ t('admin.accounts.codexTicketRuntime.snapshot') }}</summary><CodexTicketProtectionStatus :status="attempt.protection" /></details>
                  <dl class="space-y-1">
                    <div v-for="stage in observationStages" :key="stage.length" class="flex flex-wrap gap-x-1">
                      <dt>{{ t(`admin.accounts.codexTicketHistory.${stage.label}`) }}:</dt>
                      <dd :data-testid="stage.length">
                        {{ ticketLength(attempt[stage.length]) }}
                        <span v-if="hasHttpStatus(attempt[stage.status])" class="text-gray-500 dark:text-gray-400">· HTTP {{ attempt[stage.status] }}</span>
                      </dd>
                    </div>
                    <div class="flex flex-wrap gap-x-1">
                      <dt>{{ t('admin.accounts.codexTicketHistory.attemptRule') }}:</dt>
                      <dd data-testid="attempt-rule">{{ attemptRule(attempt) }}</dd>
                    </div>
                    <div v-if="attempt.business_verification_rounds && attempt.business_verification_rounds > 1" class="flex flex-wrap gap-x-1" data-testid="business-verification-progress">
                      <dt>{{ t('admin.accounts.codexTicketHistory.businessVerification') }}:</dt>
                      <dd class="font-mono tabular-nums">{{ attempt.business_verification_passed || 0 }}/{{ attempt.business_verification_rounds }}</dd>
                    </div>
                    <div v-if="attempt.business_verification_models?.length" class="space-y-0.5" data-testid="business-verification-models">
                      <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.codexTicketHistory.businessVerificationModels') }}:</dt>
                      <dd class="break-all font-mono">{{ attempt.business_verification_models.join(', ') }}</dd>
                    </div>
                  </dl>
                  <p v-if="attempt.length_mode === 'strict' && attempt.rejected_lengths?.length" class="mt-1 text-gray-500 dark:text-gray-400" data-testid="attempt-rejected-lengths">
                    {{ t('admin.accounts.codexTicketHistory.attemptRejectedLengths', { lengths: attempt.rejected_lengths.join(', ') }) }}
                  </p>
                  <button type="button" class="btn btn-secondary mt-2 px-2 py-1 text-xs" aria-haspopup="dialog" data-testid="view-exchange" @click="openExchange(attempt.id, $event)">
                    {{ t('admin.accounts.codexTicketHistory.viewExchange') }}
                  </button>
                </td>
                <td v-for="kind in proxyKinds" :key="kind" class="break-all px-3 py-3 text-xs leading-5">
                  <span v-if="attempt[kind]?.name" class="mb-1 block font-medium">{{ attempt[kind]?.name }}</span>
                  <span>{{ proxyAddress(attempt[kind]) }}</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <Pagination v-if="history.total > 0" :total="history.total" :page="page" :page-size="pageSize" :show-page-size-selector="false" @update:page="loadHistory" />
      </template>
    </div>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="loading || !account" @click="loadHistory()">{{ t('common.refresh') }}</button>
        <button type="button" class="btn btn-primary" @click="closeHistory">{{ t('common.close') }}</button>
      </div>
    </template>
  </BaseDialog>
  <CodexTicketExchangeDialog v-if="show && selectedAttempt" :key="selectedAttempt.id" ref="exchangeDialog" :attempt="selectedAttempt" :opener="exchangeOpener" @close="closeExchange" />
  <CodexTicketRequestPreview v-if="show && previewOpen && account" :account-id="account.id" :initial-model="history?.filter_options?.models[0] || history?.items[0]?.model" :opener="previewOpener" @close="previewOpen = false" />
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { CodexTicketExchange, CodexTicketHistory, CodexTicketHistoryAttempt, CodexTicketHistoryProxy, CodexTicketHistoryFilters as TicketFilters, CodexTicketHistoryFilterOptions } from '@/api/admin/accounts'
import type { Account } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Pagination from '@/components/common/Pagination.vue'
import CodexTicketExchangeDialog from './CodexTicketExchangeDialog.vue'
import CodexTicketHistoryFilters from './CodexTicketHistoryFilters.vue'
import CodexTicketLifecycle from './CodexTicketLifecycle.vue'
import CodexTicketRuntimePanel from './CodexTicketRuntimePanel.vue'
import CodexTicketProtectionStatus from './CodexTicketProtectionStatus.vue'
import CodexTicketRequestPreview from './CodexTicketRequestPreview.vue'
import { codexTicketOutcomeTone } from '@/utils/codexTicketOutcomes'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{ show: boolean; account: Pick<Account, 'id' | 'name'> | null }>()
const emit = defineEmits<{ close: [] }>()
const { t, te, locale } = useI18n()
const history = ref<CodexTicketHistory | null>(null)
const filters = ref<TicketFilters>({})
const filterOptions = ref<CodexTicketHistoryFilterOptions>({ models: [], reasons: [] })
const loading = ref(false)
const error = ref(false)
const retrying = ref(false)
const retryMessage = ref('')
const page = ref(1)
const pageSize = 20
const previewOpen = ref(false)
const previewOpener = shallowRef<HTMLElement | null>(null)
const selectedAttemptId = ref<string | null>(null)
const selectedAttempt = computed(() => history.value?.items.find(attempt => attempt.id === selectedAttemptId.value))
const exchangeOpener = shallowRef<HTMLElement | null>(null)
const exchangeDialog = ref<InstanceType<typeof CodexTicketExchangeDialog> | null>(null)
let requestSequence = 0
let retrySequence = 0
const metrics = [
  { key: 'total', color: 'text-gray-900 dark:text-gray-100' },
  { key: 'success', color: 'text-emerald-600 dark:text-emerald-400' },
  { key: 'failed', color: 'text-red-600 dark:text-red-400' }
] as const
const columns = [
  { key: 'startedAt', width: '16%' }, { key: 'model', width: '13%' }, { key: 'result', width: '8%' },
  { key: 'details', width: '31%' }, { key: 'harvestProxy', width: '16%' }, { key: 'businessProxy', width: '16%' }
] as const
const proxyKinds = ['harvest_proxy', 'business_proxy'] as const
const observationStages = [
  { label: 'harvestLength', length: 'harvest_ticket_length', status: 'harvest_http_status' },
  { label: 'businessLength', length: 'business_ticket_length', status: 'business_http_status' }
] as const
const modelStages = [
  { key: 'harvest_exchange', label: 'harvestModel', status: 'harvest_http_status' },
  { key: 'business_exchange', label: 'businessModel', status: 'business_http_status' }
] as const

function reportedModels(exchange: CodexTicketExchange | null | undefined, status: number | null | undefined): string {
  if (exchange?.reported_models?.length) return exchange.reported_models.join(', ')
  if (!exchange) return t('admin.accounts.codexTicketHistory.modelNotRecorded')
  if (exchange.response || status) return t('admin.accounts.codexTicketHistory.modelNotReported')
  return t(`admin.accounts.codexTicketHistory.${exchange.request ? 'responseNotReceived' : 'modelNotRecorded'}`)
}

function openExchange(attemptId: string, event: MouseEvent): void {
  exchangeOpener.value = event.currentTarget as HTMLElement
  selectedAttemptId.value = attemptId
}

function openPreview(event: MouseEvent): void {
  previewOpener.value = event.currentTarget as HTMLElement
  previewOpen.value = true
}

function outcomeLabel(outcome: string): string {
  const key = `admin.accounts.codexTicketOutcome.${outcome}`
  return te(key) ? t(key) : outcome
}

function closeHistory(): void {
  previewOpen.value = false
  closeExchange()
  emit('close')
}

function closeExchange(): void {
  exchangeDialog.value?.deactivate()
  selectedAttemptId.value = null
}

function ticketLength(length: number | null | undefined): string {
  if (length == null || !Number.isSafeInteger(length) || length < 0) return t('admin.accounts.codexTicketHistory.lengthNotObserved')
  return length === 0 ? t('admin.accounts.codexTicketHistory.ticketMissing') : String(length)
}

function hasHttpStatus(status: number | null | undefined): boolean {
  return status != null && Number.isInteger(status) && status >= 100 && status <= 599
}

function attemptRule(attempt: CodexTicketHistoryAttempt): string {
  if (attempt.length_mode === 'auto') return t('admin.accounts.codexTicketHistory.autoRule')
  if (attempt.length_mode !== 'strict') return t('admin.accounts.codexTicketHistory.ruleNotRecorded')
  const length = attempt.target_length
  return length != null && Number.isSafeInteger(length) && length > 0
    ? t('admin.accounts.codexTicketHistory.strictRule', { length })
    : t('admin.accounts.codexTicketHistory.strictRuleWithoutLength')
}

function proxyAddress(proxy: CodexTicketHistoryProxy | null | undefined): string {
  if (!proxy) return t('admin.accounts.codexTicketHistory.notSelected')
  return proxy.address === 'direct' ? t('admin.accounts.codexTicketHistory.direct') : proxy.address
}

function failureReason(reason: string): string {
  const key = `admin.accounts.codexTicketHistory.reasons.${reason}`
  return reason && te(key) ? t(key) : reason || '—'
}

function attemptDuration(attempt: CodexTicketHistoryAttempt): string {
  const ms = Date.parse(attempt.finished_at) - Date.parse(attempt.started_at)
  return Number.isFinite(ms) && ms >= 0 ? t('admin.accounts.codexTicketHistory.duration', { ms }) : '—'
}

async function loadHistory(requestedPage = page.value): Promise<void> {
  if (!props.show || !props.account) return
  const sequence = ++requestSequence
  closeExchange()
  page.value = requestedPage
  loading.value = true
  error.value = false
  try {
    const result = await adminAPI.accounts.getCodexTicketHistory(props.account.id, { page: requestedPage, page_size: pageSize, ...filters.value })
    if (sequence !== requestSequence) return
    history.value = result
    filterOptions.value = result.filter_options || { models: [], reasons: [] }
    page.value = result.page
  } catch {
    if (sequence !== requestSequence) return
    history.value = null
    error.value = true
  } finally {
    if (sequence === requestSequence) loading.value = false
  }
}

function applyFilters(value: TicketFilters): void {
  filters.value = value
  void loadHistory(1)
}

async function retryEligibleTickets(): Promise<void> {
  if (!props.show || !props.account || retrying.value) return
  const sequence = ++retrySequence
  const accountId = props.account.id
  retrying.value = true
  retryMessage.value = ''
  try {
    const result = await adminAPI.accounts.retryCodexTicket(accountId)
    if (sequence !== retrySequence) return
    retryMessage.value = result.scheduled > 0
      ? t('admin.accounts.codexTicketHistory.retryScheduled', { count: result.scheduled, models: result.models.join(', ') })
      : t('admin.accounts.codexTicketHistory.retrySkipped')
    await loadHistory(page.value)
  } catch {
    if (sequence !== retrySequence) return
    retryMessage.value = t('admin.accounts.codexTicketHistory.retryFailed')
  } finally {
    if (sequence === retrySequence) retrying.value = false
  }
}

watch([() => props.show, () => props.account?.id], () => {
  previewOpen.value = false
  requestSequence++
  retrySequence++
  page.value = 1
  history.value = null
  filters.value = {}
  filterOptions.value = { models: [], reasons: [] }
  closeExchange()
  error.value = false
  retryMessage.value = ''
  retrying.value = false
  loading.value = false
  void loadHistory()
}, { immediate: true })

onBeforeUnmount(() => { requestSequence++; retrySequence++ })
</script>
