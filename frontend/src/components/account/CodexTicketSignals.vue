<template>
  <div class="min-w-0 space-y-1 break-all text-xs" data-testid="upstream-signals">
    <dl><div v-for="row in signalRows" :key="row.key"><dt class="inline text-gray-500 dark:text-gray-400">{{ t(`${prefix}.${row.key}`) }}: </dt><dd class="inline font-mono">{{ row.value }}</dd></div></dl>
    <dl v-for="(quota, index) in signals.quota?.slice(0, 8)" :key="`${quota.id}-${index}`" class="border-t border-gray-200 pt-1 dark:border-dark-600">
      <dt class="font-medium">{{ quota.id }}</dt>
      <dd v-for="row in quotaRows(quota)" :key="row.key">{{ t(`${prefix}.${row.key}`) }}: <span class="font-mono">{{ row.value }}</span></dd>
    </dl>
    <p class="text-gray-500 dark:text-gray-400">{{ t(`${prefix}.signalsHint`) }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CodexTicketSignals } from '@/api/admin/codexTicketDiagnostics'

const props = defineProps<{ signals: CodexTicketSignals }>()
const { t } = useI18n()
const prefix = 'admin.accounts.codexTicketDiagnostics'
const display = (value: unknown): string => value === undefined || value === null || value === '' ? t(`${prefix}.notRecorded`) : typeof value === 'boolean' ? t(`${prefix}.${value ? 'yes' : 'no'}`) : String(value)
const rows = (value: object, keys: string[]) => keys.map(key => ({ key, value: display((value as Record<string, unknown>)[key]) }))
const signalRows = computed(() => rows(props.signals, ['safety_buffering_enabled', 'faster_model', 'active_limit', 'plan_type']))
const quotaRows = (quota: NonNullable<CodexTicketSignals['quota']>[number]) => rows(quota, ['used_percent', 'reset_at', 'reset_after_seconds', 'window_minutes', 'limit_reached'])
</script>
