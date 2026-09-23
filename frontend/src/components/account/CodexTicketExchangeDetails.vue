<template>
  <section class="min-w-0 max-w-full space-y-4 rounded-lg border border-gray-200 p-3 dark:border-dark-600 sm:p-4" data-testid="ticket-exchange-details">
    <h3 class="break-all text-sm font-semibold text-gray-900 dark:text-gray-100">
      {{ t(`${prefix}.exchangeTitle`) }} · {{ attempt.model }} · {{ formatDateTime(attempt.started_at) }}
    </h3>
    <section v-for="stage in stages" :key="stage.key" class="min-w-0 space-y-2" :data-testid="`${stage.key}-exchange`">
      <h4 class="text-sm font-medium text-gray-900 dark:text-gray-100">{{ t(`${prefix}.${stage.label}`) }}</h4>
      <p v-if="stage.exchange" class="text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t(`${prefix}.${stage.exchange.capture_mode === 'raw' ? 'exchangeRawHint' : 'exchangeRedactedHint'}`) }}</p>
      <dl class="space-y-1 break-all text-xs text-gray-700 dark:text-gray-300">
        <div><dt class="inline">{{ t(`${prefix}.requestedModel`) }}: </dt><dd class="inline font-mono">{{ stage.exchange?.requested_model || attempt.model || '—' }}</dd></div>
        <div>
          <dt class="inline">{{ t(`${prefix}.reportedModels`) }}: </dt>
          <dd class="inline font-mono">{{ reportedModels(stage) }}</dd>
          <span v-if="stage.exchange?.models_truncated" class="ml-1 text-amber-600 dark:text-amber-400">({{ t(`${prefix}.modelsTruncated`) }})</span>
        </div>
      </dl>
      <CodexTicketDiagnostics :exchange="stage.exchange" />
      <p v-if="!stage.exchange?.request && !stage.exchange?.response" class="text-xs leading-5 text-gray-500 dark:text-gray-400" data-testid="exchange-unavailable">
        {{ t(`${prefix}.exchangeUnavailable`) }}
      </p>
      <div v-else class="grid min-w-0 gap-3 lg:grid-cols-2">
        <div v-for="kind in messageKinds" :key="kind" class="min-w-0 space-y-2 rounded border border-gray-200 p-3 dark:border-dark-600" :data-testid="`${stage.key}-${kind}`">
          <div class="flex flex-wrap items-center justify-between gap-2 text-xs">
            <h5 class="font-medium text-gray-900 dark:text-gray-100">{{ t(`${prefix}.${stage.exchange?.capture_mode === 'raw' ? 'raw' : 'redacted'}${kind === 'request' ? 'Request' : 'Response'}`) }}</h5>
            <button v-if="stage.exchange?.[kind]" type="button" class="btn btn-secondary px-2 py-1 text-xs" :disabled="copying" @click="copyMessage(stage, kind)">
              {{ t(`${prefix}.${stage.exchange.capture_mode === 'raw' ? 'copyRawExchange' : 'copyExchange'}`) }}
            </button>
          </div>
          <template v-if="stage.exchange?.[kind]">
            <p v-if="stage.exchange[kind]?.method || stage.exchange[kind]?.url" class="break-all font-mono text-xs text-gray-700 dark:text-gray-300">{{ stage.exchange[kind]?.method }} {{ stage.exchange[kind]?.url }}</p>
            <p v-if="stage.exchange[kind]?.status_code" class="text-xs font-medium text-gray-700 dark:text-gray-300">HTTP {{ stage.exchange[kind]?.status_code }}</p>
            <div class="flex flex-wrap gap-x-2 text-xs text-gray-500 dark:text-gray-400">
              <span>{{ t(`${prefix}.bodyBytes`, { bytes: stage.exchange[kind]?.body_bytes ?? 0 }) }}</span>
              <span v-if="stage.exchange[kind]?.body_truncated" class="text-amber-600 dark:text-amber-400">{{ t(`${prefix}.bodyOmitted`) }}</span>
              <span v-if="stage.exchange[kind]?.headers_truncated" class="text-amber-600 dark:text-amber-400">{{ t(`${prefix}.headersTruncated`) }}</span>
            </div>
            <h6 class="text-xs font-medium text-gray-600 dark:text-gray-400">{{ t(`${prefix}.headers`) }}</h6>
            <pre class="max-h-48 max-w-full overflow-y-auto whitespace-pre-wrap break-all rounded bg-gray-50 p-2 font-mono text-xs text-gray-700 dark:bg-dark-800 dark:text-gray-300">{{ formatHeaders(stage.exchange[kind]!) || t(`${prefix}.emptyHeaders`) }}</pre>
            <h6 class="text-xs font-medium text-gray-600 dark:text-gray-400">{{ t(`${prefix}.body`) }}</h6>
            <pre class="max-h-80 max-w-full overflow-y-auto whitespace-pre-wrap break-all rounded bg-gray-50 p-2 font-mono text-xs text-gray-700 dark:bg-dark-800 dark:text-gray-300">{{ stage.exchange[kind]?.body || t(`${prefix}.${stage.exchange[kind]?.body_truncated ? 'bodyOmitted' : 'emptyBody'}`) }}</pre>
            <p v-if="copyFeedback?.key === `${stage.key}-${kind}`" :role="copyFeedback.success ? 'status' : 'alert'" class="text-xs" :class="copyFeedback.success ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-600 dark:text-red-400'">
              {{ t(`${prefix}.${copyFeedback.success ? (stage.exchange.capture_mode === 'raw' ? 'rawExchangeCopied' : 'exchangeCopied') : 'exchangeCopyFailed'}`) }}
            </p>
          </template>
          <p v-else class="text-xs text-gray-500 dark:text-gray-400">{{ t(`${prefix}.${kind}NotRecorded`) }}</p>
        </div>
      </div>
    </section>
  </section>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CodexTicketExchange, CodexTicketHistoryAttempt, CodexTicketHTTPMessage } from '@/api/admin/accounts'
import { useClipboard } from '@/composables/useClipboard'
import { formatDateTime } from '@/utils/format'
import CodexTicketDiagnostics from './CodexTicketDiagnostics.vue'

const props = defineProps<{ attempt: CodexTicketHistoryAttempt }>()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const prefix = 'admin.accounts.codexTicketHistory'
const messageKinds = ['request', 'response'] as const
const copying = ref(false)
const copyFeedback = ref<{ key: string; success: boolean } | null>(null)
const stages = computed(() => [
  { key: 'harvest', label: 'harvestStage', exchange: props.attempt.harvest_exchange, status: props.attempt.harvest_http_status },
  { key: 'business', label: 'businessStage', exchange: props.attempt.business_exchange, status: props.attempt.business_http_status }
])

function reportedModels(stage: { exchange?: CodexTicketExchange | null; status?: number | null }): string {
  if (stage.exchange?.reported_models?.length) return stage.exchange.reported_models.join(', ')
  if (!stage.exchange) return t(`${prefix}.modelNotRecorded`)
  if (stage.exchange.response || stage.status) return t(`${prefix}.modelNotReported`)
  return t(`${prefix}.${stage.exchange.request ? 'responseNotReceived' : 'modelNotRecorded'}`)
}

function formatHeaders(message: CodexTicketHTTPMessage): string {
  return Object.entries(message.headers ?? {}).flatMap(([name, values]) => values.map(value => `${name}: ${value}`)).join('\n')
}

async function copyMessage(stage: { key: string; exchange?: CodexTicketExchange | null }, kind: 'request' | 'response'): Promise<void> {
  const message = stage.exchange?.[kind]
  if (!message) return
  copying.value = true
  const raw = stage.exchange?.capture_mode === 'raw'
  const lines = raw ? [] : [t(`${prefix}.exchangeRedactedHint`)]
  if (message.method || message.url) lines.push([message.method, message.url].filter(Boolean).join(' '))
  if (message.status_code) lines.push(`HTTP ${message.status_code}`)
  if (!raw && message.headers_truncated) lines.push(t(`${prefix}.headersTruncated`))
  if (!raw && message.body_truncated) lines.push(t(`${prefix}.bodyOmitted`))
  lines.push(formatHeaders(message), '')
  const body = raw ? message.body ?? '' : message.body || t(`${prefix}.${message.body_truncated ? 'bodyOmitted' : 'emptyBody'}`)
  try {
    const success = await copyToClipboard(lines.join('\n') + '\n' + body, t(`${prefix}.${raw ? 'rawExchangeCopied' : 'exchangeCopied'}`))
    copyFeedback.value = { key: `${stage.key}-${kind}`, success }
  } catch {
    copyFeedback.value = { key: `${stage.key}-${kind}`, success: false }
  } finally {
    copying.value = false
  }
}
</script>
