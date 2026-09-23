<template>
  <div class="grid min-w-0 gap-3 text-xs sm:grid-cols-2" data-testid="ticket-diagnostics">
    <dl class="min-w-0 space-y-1 break-all">
      <div v-for="row in networkRows" :key="row.key"><dt class="inline text-gray-500 dark:text-gray-400">{{ t(`${prefix}.${row.key}`) }}: </dt><dd class="inline font-mono">{{ row.value }}</dd></div>
      <div><dd class="text-gray-500 dark:text-gray-400">{{ t(`${prefix}.peerHint`) }}</dd></div>
    </dl>
    <dl class="min-w-0 space-y-1 break-all">
      <div v-for="row in declarationRows" :key="row.key"><dt class="inline text-gray-500 dark:text-gray-400">{{ t(`${prefix}.${row.key}`) }}: </dt><dd class="inline font-mono">{{ row.value }}</dd></div>
      <div v-if="exchange?.model_declaration?.truncated"><dd class="text-amber-700 dark:text-amber-300">{{ t(`${prefix}.truncated`) }}</dd></div>
    </dl>
    <dl v-if="exchange?.upstream_error" class="min-w-0 space-y-1 break-all" data-testid="upstream-error">
      <div v-for="row in errorRows" :key="row.key"><dt class="inline text-gray-500 dark:text-gray-400">{{ t(`${prefix}.${row.key}`) }}: </dt><dd class="inline font-mono">{{ row.value }}</dd></div>
    </dl>
    <CodexTicketSignals v-if="exchange?.signals" :signals="exchange.signals" />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CodexTicketExchange } from '@/api/admin/accounts'
import CodexTicketSignals from './CodexTicketSignals.vue'

const props = defineProps<{ exchange?: CodexTicketExchange | null }>()
const { t } = useI18n()
const prefix = 'admin.accounts.codexTicketDiagnostics'
const display = (value: unknown): string => value === undefined || value === null || value === '' ? t(`${prefix}.notRecorded`) : typeof value === 'boolean' ? t(`${prefix}.${value ? 'yes' : 'no'}`) : String(value)
const rows = (value: object | undefined, keys: string[]) => keys.map(key => ({ key, value: display((value as Record<string, unknown> | undefined)?.[key]) }))
const networkRows = computed(() => rows(props.exchange?.network, ['response_header_ms', 'peer_addr', 'http_version', 'final_origin']))
const declarationRows = computed(() => rows(props.exchange?.model_declaration, ['first_model', 'first_event', 'terminal_model', 'terminal_event', 'conflict']))
const errorRows = computed(() => rows(props.exchange?.upstream_error, ['wire_http_status', 'effective_status', 'type', 'code', 'scope', 'retry_at', 'classification_source']))
</script>
