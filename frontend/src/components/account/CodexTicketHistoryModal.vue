<template>
  <BaseDialog :show="show" :title="t('admin.accounts.codexTicketHistory.title')" width="extra-wide" @close="emit('close')">
    <div class="min-w-0 space-y-4">
      <p class="break-all text-sm font-medium text-gray-900 dark:text-gray-100">{{ account?.name }}</p>
      <div v-if="loading" class="flex justify-center py-12"><LoadingSpinner /></div>
      <div v-else-if="error" role="alert" class="space-y-3 rounded-lg border border-red-200 p-4 dark:border-red-500/30">
        <p class="text-sm text-red-600 dark:text-red-400">{{ t('admin.accounts.codexTicketHistory.loadFailed') }}</p>
        <button type="button" class="btn btn-secondary" @click="loadHistory()">{{ t('admin.accounts.codexTicketHistory.retry') }}</button>
      </div>
      <template v-else-if="history">
        <dl class="grid grid-cols-3 gap-2 sm:gap-4">
          <div v-for="metric in metrics" :key="metric.key" class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
            <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t(`admin.accounts.codexTicketHistory.${metric.key}`) }}</dt>
            <dd class="mt-1 break-all text-xl font-semibold tabular-nums" :class="metric.color" :data-testid="`ticket-history-${metric.key}`">
              {{ history.summary[metric.key].toLocaleString(locale) }}
            </dd>
          </div>
        </dl>
        <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.codexTicketHistory.retainedHint', { limit: history.retained_limit || 100 }) }}
          <span class="mt-1 block">{{ t('admin.accounts.codexTicketHistory.exchangeRetainedHint', { limit: history.exchange_retained_limit || 10 }) }}</span>
          <span v-if="history.summary.last_attempt_at" class="mt-1 block">
            {{ t('admin.accounts.codexTicketHistory.lastAttempt') }}: {{ formatDateTime(history.summary.last_attempt_at) }}
          </span>
        </p>
        <div class="flex flex-wrap items-center gap-3">
          <button type="button" class="btn btn-secondary" :disabled="loading || retrying || !account" @click="retryEligibleTickets()">
            {{ retrying ? t('admin.accounts.codexTicketHistory.retrying') : t('admin.accounts.codexTicketHistory.retryEligible') }}
          </button>
          <span v-if="retryMessage" class="text-sm text-gray-600 dark:text-gray-300" role="status">{{ retryMessage }}</span>
        </div>
        <p v-if="history.items.length === 0" class="py-10 text-center text-sm text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.codexTicketHistory.empty') }}
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
                  <span class="inline-flex rounded px-2 py-0.5 text-xs font-medium" :class="attempt.success ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-400' : 'bg-red-50 text-red-700 dark:bg-red-500/10 dark:text-red-400'">
                    {{ t(`admin.accounts.codexTicketHistory.${attempt.success ? 'success' : 'failed'}`) }}
                  </span>
                </td>
                <td class="break-words px-3 py-3 text-xs leading-5" data-testid="attempt-details">
                  <p v-if="!attempt.success" class="mb-1 font-medium text-red-600 dark:text-red-400">{{ failureReason(attempt.reason) }}</p>
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
                  </dl>
                  <p v-if="attempt.length_mode === 'strict' && attempt.rejected_lengths?.length" class="mt-1 text-gray-500 dark:text-gray-400" data-testid="attempt-rejected-lengths">
                    {{ t('admin.accounts.codexTicketHistory.attemptRejectedLengths', { lengths: attempt.rejected_lengths.join(', ') }) }}
                  </p>
                  <button type="button" class="btn btn-secondary mt-2 px-2 py-1 text-xs" :aria-expanded="selectedAttemptId === attempt.id" aria-controls="ticket-history-exchange" data-testid="view-exchange" @click="toggleExchange(attempt.id)">
                    {{ t(`admin.accounts.codexTicketHistory.${selectedAttemptId === attempt.id ? 'hideExchange' : 'viewExchange'}`) }}
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
        <CodexTicketExchangeDetails v-if="selectedAttempt" id="ticket-history-exchange" :key="selectedAttempt.id" ref="exchangeDetails" :attempt="selectedAttempt" />
        <Pagination v-if="history.total > 0" :total="history.total" :page="page" :page-size="pageSize" :show-page-size-selector="false" @update:page="loadHistory" />
      </template>
    </div>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="loading || !account" @click="loadHistory()">{{ t('common.refresh') }}</button>
        <button type="button" class="btn btn-primary" @click="emit('close')">{{ t('common.close') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { CodexTicketExchange, CodexTicketHistory, CodexTicketHistoryAttempt, CodexTicketHistoryProxy } from '@/api/admin/accounts'
import type { Account } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Pagination from '@/components/common/Pagination.vue'
import CodexTicketExchangeDetails from './CodexTicketExchangeDetails.vue'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{ show: boolean; account: Pick<Account, 'id' | 'name'> | null }>()
const emit = defineEmits<{ close: [] }>()
const { t, te, locale } = useI18n()
const history = ref<CodexTicketHistory | null>(null)
const loading = ref(false)
const error = ref(false)
const retrying = ref(false)
const retryMessage = ref('')
const page = ref(1)
const pageSize = 20
const selectedAttemptId = ref<string | null>(null)
const selectedAttempt = computed(() => history.value?.items.find(attempt => attempt.id === selectedAttemptId.value))
const exchangeDetails = ref<InstanceType<typeof CodexTicketExchangeDetails> | null>(null)
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

async function toggleExchange(attemptId: string): Promise<void> {
  selectedAttemptId.value = selectedAttemptId.value === attemptId ? null : attemptId
  await nextTick()
  exchangeDetails.value?.$el.scrollIntoView?.({ block: 'nearest', behavior: 'smooth' })
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
  selectedAttemptId.value = null
  page.value = requestedPage
  loading.value = true
  error.value = false
  try {
    const result = await adminAPI.accounts.getCodexTicketHistory(props.account.id, { page: requestedPage, page_size: pageSize })
    if (sequence !== requestSequence) return
    history.value = result
    page.value = result.page
  } catch {
    if (sequence !== requestSequence) return
    history.value = null
    error.value = true
  } finally {
    if (sequence === requestSequence) loading.value = false
  }
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
  requestSequence++
  retrySequence++
  page.value = 1
  history.value = null
  selectedAttemptId.value = null
  error.value = false
  retryMessage.value = ''
  retrying.value = false
  loading.value = false
  void loadHistory()
}, { immediate: true })

onBeforeUnmount(() => { requestSequence++; retrySequence++ })
</script>
