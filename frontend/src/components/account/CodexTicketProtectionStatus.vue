<template>
  <dl class="grid min-w-0 gap-x-4 gap-y-2 text-xs sm:grid-cols-2 lg:grid-cols-3" data-testid="ticket-protection-status">
    <div v-for="row in rows" :key="row.key" class="min-w-0 break-all">
      <dt class="text-gray-500 dark:text-gray-400">{{ t(`${prefix}.${row.key}`) }}</dt>
      <dd class="mt-0.5 font-mono text-gray-800 dark:text-gray-200">{{ row.value }}</dd>
    </div>
  </dl>
  <p v-if="status.retry_at" class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ t(`${prefix}.retryHint`) }}</p>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CodexTicketRuntimeStatus } from '@/api/admin/codexTicketDiagnostics'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{ status: CodexTicketRuntimeStatus }>()
const { t } = useI18n()
const prefix = 'admin.accounts.codexTicketRuntime'
function label(value: string | undefined): string {
  if (!value) return '—'
  const key = `${prefix}.states.${value}`
  const translated = t(key)
  return translated === key ? value : translated
}
const rows = computed(() => {
  const status = props.status
  const result = [
    { key: 'state', value: label(status.state) }, { key: 'reason', value: label(status.reason) },
    { key: 'attempts', value: `${status.attempts_used} / ${status.max_attempts}` },
    { key: 'round', value: `${status.round} / ${status.max_rounds}` },
    { key: 'model', value: status.model || status.last_model || '—' },
    { key: 'proxy', value: status.proxy_id ?? '—' },
    { key: 'halfOpen', value: t(`${prefix}.${status.half_open ? 'yes' : 'no'}`) },
    { key: 'retryAt', value: status.retry_at ? formatDateTime(status.retry_at) : '—' },
    { key: 'cooldownUntil', value: status.cooldown_until ? formatDateTime(status.cooldown_until) : '—' },
    { key: 'generation', value: status.generation }
  ]
  for (const key of ['rule_matched', 'harvest_half_open', 'harvest_accepted'] as const) {
    if (status[key] !== undefined) result.push({ key, value: t(`${prefix}.${status[key] ? 'yes' : 'no'}`) })
  }
  if (status.silence_until) result.push({ key: 'silence_until', value: formatDateTime(status.silence_until) })
  if (status.policy_version) result.push({ key: 'policy_version', value: status.policy_version })
  return result
})
</script>
