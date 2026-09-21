<template>
  <section v-if="account.type === 'oauth'" class="mt-3 min-w-0 space-y-1.5" :aria-label="t('admin.accounts.columns.usageWindows')" :aria-busy="loading || quotaBusy">
    <div class="flex items-center justify-between gap-2">
      <h4 class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.accounts.columns.usageWindows') }}</h4>
      <button v-if="!isOpenAI" type="button" class="rounded p-1 text-gray-400 hover:text-primary-500 disabled:cursor-not-allowed disabled:opacity-50" :disabled="loading || busy" :aria-label="t('sharedPool.refreshUsage')" :title="t('sharedPool.refreshUsage')" @click="queryUsage">
        <Icon name="refresh" size="xs" :class="{ 'animate-spin': loading }" />
      </button>
    </div>
    <div v-if="tickets.length" class="space-y-0.5">
      <div v-for="ticket in tickets" :key="ticket.model" class="flex flex-wrap items-center gap-1 text-[10px] leading-4" data-testid="codex-ticket-status">
        <span class="font-medium text-gray-500 dark:text-gray-400" :title="ticket.model">{{ shortTicketModel(ticket.model) }}</span>
        <span v-if="ticket.ready" class="text-emerald-600 dark:text-emerald-400">{{ ticketRemaining(ticket.remaining_seconds) }}</span>
        <span v-else-if="ticket.blocked" class="text-amber-600 dark:text-amber-400">{{ t('admin.accounts.openai.codexTurnTicketPaused') }}</span>
        <span v-else class="text-gray-500">{{ t('admin.accounts.openai.codexTurnTicketMissing') }}</span>
      </div>
    </div>
    <p v-if="loading && !usage" class="text-xs text-gray-400" role="status">{{ t('common.loading') }}</p>
    <p v-if="unavailable" class="text-xs text-amber-600 dark:text-amber-400" role="status">{{ t('sharedPool.usageUnavailable') }}</p>
    <div v-if="windows.length" class="space-y-1.5">
      <UsageProgressBar v-for="window in windows" :key="window.key" :label="window.labelKey ? t(window.labelKey) : window.label" :utilization="window.utilization" :resets-at="window.resetsAt" :window-stats="window.windowStats" :estimated-total-cost="window.estimatedTotalCost" :show-now-when-idle="window.showNowWhenIdle" :color="window.color" />
      <p v-if="account.platform === 'gemini'" class="text-[9px] text-gray-400">{{ t('admin.accounts.gemini.quotaPolicy.simulatedNote') }}</p>
    </div>
    <p v-else-if="!loading && !unavailable" class="text-xs text-gray-400">{{ t('sharedPool.usageEmpty') }}</p>
    <OpenAIQuotaResetCell v-if="isOpenAI" :account="quotaAccount" :query-quota="sharedPoolAPI.refreshQuota" :reset-quota="sharedPoolAPI.resetQuota" :disabled="busy || loading" @busy-change="quotaBusy = $event" @quota-reset="quotaReset">
      <template #pre-actions>
        <button type="button" class="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-[10px] font-medium text-blue-600 transition-colors hover:bg-blue-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-blue-400 dark:hover:bg-blue-900/30" :disabled="busy || loading || quotaBusy" :title="t('sharedPool.refreshUsage')" @click="queryUsage">
          <Icon name="refresh" size="xs" :class="{ 'animate-spin': loading }" />
          {{ t('admin.accounts.usageWindow.activeQuery') }}
        </button>
      </template>
    </OpenAIQuotaResetCell>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { sharedPoolAPI, type SharedAccount, type SharedAccountUsageInfo } from '@/api/sharedPool'
import { buildOAuthUsageWindows } from '@/utils/oauthUsageWindows'
import UsageProgressBar from '@/components/account/UsageProgressBar.vue'
import OpenAIQuotaResetCell from '@/components/account/OpenAIQuotaResetCell.vue'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{ account: SharedAccount; busy?: boolean }>()
const emit = defineEmits<{ 'busy-change': [busy: boolean]; 'usage-updated': [] }>()
const { t } = useI18n()
const usage = ref<SharedAccountUsageInfo | null>(null)
const loading = ref(false)
const quotaBusy = ref(false)
const failed = ref(false)
const isOpenAI = computed(() => props.account.platform === 'openai' && props.account.type === 'oauth')
const tickets = computed(() => isOpenAI.value && props.account.codex_ticket_enabled !== false ? usage.value?.codex_turn_tickets ?? [] : [])
const quotaAccount = computed(() => ({
  id: props.account.id, platform: props.account.platform, type: props.account.type,
  extra: { codex_reset_credit_snapshot: usage.value?.codex_reset_credit_snapshot }
}))
const unavailable = computed(() => failed.value || usage.value?.error || usage.value?.is_forbidden || usage.value?.needs_reauth || usage.value?.needs_verify || usage.value?.is_banned)
let request: AbortController | null = null
const windows = computed(() => usage.value ? buildOAuthUsageWindows(props.account.platform, usage.value) : [])

function shortTicketModel(model: string) {
  return ({ 'gpt-6-astra': 'astra', 'gpt-5.6-sol': 'sol' } as Record<string, string>)[model] || model
}
function ticketRemaining(seconds: number) {
  const total = Math.max(0, Math.floor(seconds || 0))
  return `${Math.floor(total / 60)}m${String(total % 60).padStart(2, '0')}s`
}
function queryUsage() {
  if (props.busy || loading.value || quotaBusy.value) return
  void loadUsage('active', true)
}
function quotaReset() {
  void loadUsage('active')
  emit('usage-updated')
}
async function loadUsage(source: 'passive' | 'active', force = false) {
  if (props.account.type !== 'oauth') return
  request?.abort()
  const controller = new AbortController()
  request = controller
  loading.value = true
  failed.value = false
  try {
    const result = await sharedPoolAPI.getUsage(props.account.id, source, controller.signal, force)
    if (!controller.signal.aborted) usage.value = result
  } catch {
    if (!controller.signal.aborted) failed.value = true
  } finally {
    if (!controller.signal.aborted) loading.value = false
  }
}

watch(() => [props.account.id, props.account.platform, props.account.type, props.account.last_used_at, props.account.codex_ticket_enabled], () => {
  request?.abort()
  usage.value = null
  failed.value = false
  loading.value = false
  void loadUsage(props.account.platform === 'anthropic' ? 'passive' : 'active')
}, { immediate: true })
watch(() => loading.value || quotaBusy.value, value => emit('busy-change', value), { immediate: true })
onBeforeUnmount(() => request?.abort())
</script>
