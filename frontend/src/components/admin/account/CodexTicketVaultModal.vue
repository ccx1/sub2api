<template>
  <BaseDialog :show="show" :title="t(`${prefix}.title`)" width="full" @close="emit('close')">
    <div class="min-w-0 space-y-4" data-testid="codex-ticket-vault">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <p class="break-all text-sm font-medium text-gray-900 dark:text-gray-100">{{ account?.name }}</p>
        <button type="button" class="btn btn-secondary" :disabled="loading || !account" data-testid="vault-refresh" @click="load">{{ t(`${prefix}.refresh`) }}</button>
      </div>
      <p class="rounded bg-gray-50 p-2 text-xs text-gray-600 dark:bg-dark-700 dark:text-gray-300">{{ t(`${prefix}.safeHint`) }}</p>
      <p v-if="loading" role="status" class="text-sm text-gray-500">{{ t(`${prefix}.loading`) }}</p>
      <p v-if="error" role="alert" class="break-all text-sm text-red-600 dark:text-red-400" data-testid="vault-error">{{ error }}</p>
      <p v-if="warning" role="alert" class="rounded bg-amber-50 p-2 text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-300" data-testid="vault-warning">{{ warning }}</p>

      <template v-if="vault">
        <section class="space-y-2 rounded-lg border border-gray-200 p-3 dark:border-dark-600" data-testid="vault-overview">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ t(`${prefix}.overview`) }}</h3>
          <dl class="grid gap-x-4 gap-y-1 break-all text-xs text-gray-700 dark:text-gray-300 sm:grid-cols-2 lg:grid-cols-3">
            <div v-for="row in overviewRows" :key="row.key"><dt class="inline text-gray-500 dark:text-gray-400">{{ t(`${prefix}.fields.${row.key}`) }}: </dt><dd class="inline">{{ row.value }}</dd></div>
          </dl>
        </section>

        <p v-if="!models.length" class="text-sm text-gray-500" data-testid="vault-empty">{{ t(`${prefix}.empty`) }}</p>
        <section v-for="model in models" :key="model.model" class="min-w-0 space-y-2 rounded-lg border border-gray-200 p-3 dark:border-dark-600" data-testid="vault-model">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <h3 class="break-all font-mono text-sm font-semibold text-gray-900 dark:text-gray-100">
              {{ model.model }}
              <span v-if="!model.configured" class="ml-2 rounded bg-gray-100 px-1.5 py-0.5 font-sans text-xs font-normal text-gray-600 dark:bg-dark-700 dark:text-gray-300">{{ t(`${prefix}.notConfigured`) }}</span>
            </h3>
            <div class="flex flex-wrap items-center gap-2 text-xs text-gray-600 dark:text-gray-300">
              <span data-testid="vault-model-summary">{{ t(`${prefix}.summary.total`) }} {{ model.total }} / {{ model.capacity ?? vault.pool_capacity }} · {{ t(`${prefix}.summary.available`) }} {{ model.available }} · {{ t(`${prefix}.summary.maturing`) }} {{ model.maturing }}</span>
              <template v-if="confirmModel === model.model">
                <button type="button" class="btn btn-danger" :disabled="!!busy" data-testid="vault-revoke-all-confirm" @click="revoke(model.model)">{{ t(`${prefix}.${busy ? 'revoking' : 'confirm'}`) }}</button>
                <button type="button" class="btn btn-secondary" :disabled="!!busy" data-testid="vault-revoke-all-cancel" @click="confirmModel = ''">{{ t(`${prefix}.cancel`) }}</button>
              </template>
              <button v-else-if="activeSlots(model).length" type="button" class="btn btn-secondary" :disabled="!!busy" data-testid="vault-revoke-all" @click="confirmModel = model.model">{{ t(`${prefix}.revokeAll`) }}</button>
            </div>
          </div>
          <p v-if="confirmModel === model.model" class="text-xs text-red-600 dark:text-red-400">{{ t(`${prefix}.revokeAllConfirm`) }}</p>
          <p v-if="!(model.slots ?? []).length" class="text-xs text-gray-500">{{ t(`${prefix}.noSlots`) }}</p>
          <div v-else class="overflow-x-auto">
            <table class="min-w-full text-left text-xs">
              <thead class="text-gray-500 dark:text-gray-400">
                <tr><th v-for="column in columns" :key="column" class="whitespace-nowrap px-2 py-1 font-medium">{{ t(`${prefix}.columns.${column}`) }}</th></tr>
              </thead>
              <tbody class="divide-y divide-gray-100 text-gray-700 dark:divide-dark-700 dark:text-gray-300">
                <tr v-for="slot in model.slots ?? []" :key="slot.fingerprint" data-testid="vault-slot">
                  <td class="whitespace-nowrap px-2 py-1 font-mono">
                    {{ slot.label }}
                    <span v-if="slot.business_selected" class="ml-1 rounded bg-emerald-50 px-1.5 py-0.5 font-sans text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300">{{ t(`${prefix}.businessSelected`) }}</span>
                  </td>
                  <td class="whitespace-nowrap px-2 py-1" :class="statusClass(slot.status)" data-testid="vault-slot-status">
                    {{ statusText(slot.status) }}
                    <span class="text-gray-500 dark:text-gray-400"> · {{ t(`${prefix}.${slot.verified ? 'verified' : slot.verification_skipped ? 'verificationSkipped' : 'unverified'}`) }}</span>
                  </td>
                  <td class="whitespace-nowrap px-2 py-1 font-mono">{{ slot.fingerprint }}</td>
                  <td class="px-2 py-1">
                    <div>{{ slot.credential_mode || '—' }}</div>
                    <div v-if="slot.length > 0" class="text-gray-500 dark:text-gray-400">{{ t(`${prefix}.credentialState`, { length: slot.length }) }}</div>
                    <div v-if="(slot.cookie_names ?? []).length" class="break-all text-gray-500 dark:text-gray-400">{{ t(`${prefix}.credentialCookies`, { names: (slot.cookie_names ?? []).join(', ') }) }}</div>
                  </td>
                  <td class="whitespace-nowrap px-2 py-1 font-mono">{{ nodeText(slot) }}</td>
                  <td class="whitespace-nowrap px-2 py-1">{{ t(`${prefix}.seconds`, { count: slot.age_seconds }) }}</td>
                  <td class="whitespace-nowrap px-2 py-1">{{ timeText(slot.mature_at) }}</td>
                  <td class="whitespace-nowrap px-2 py-1">
                    <div>{{ timeText(slot.expires_at) }}</div>
                    <div v-if="slot.expires_at" class="text-gray-500 dark:text-gray-400">{{ t(`${prefix}.remaining`, { count: slot.remaining_seconds }) }}</div>
                  </td>
                  <td class="whitespace-nowrap px-2 py-1">
                    <div>{{ timeText(slot.captured_at) }}</div>
                    <div v-if="slot.origin_captured_at" class="text-gray-500 dark:text-gray-400">{{ t(`${prefix}.origin`, { time: timeText(slot.origin_captured_at) }) }}</div>
                  </td>
                  <td class="px-2 py-1">{{ [slot.harvest_proxy_name, slot.harvest_country].filter(Boolean).join(' · ') || '—' }}</td>
                  <td class="px-2 py-1">
                    <template v-if="slot.invalidation">
                      <div>{{ reasonText(slot.invalidation.reason) }}</div>
                      <div class="text-gray-500 dark:text-gray-400">{{ sourceText(slot.invalidation.source) }} · {{ timeText(slot.invalidation.invalidated_at) }}</div>
                    </template>
                    <template v-else>—</template>
                  </td>
                  <td class="whitespace-nowrap px-2 py-1">
                    <button v-if="slot.status !== 'revoked'" type="button" class="btn btn-secondary" :disabled="!!busy" data-testid="vault-revoke" @click="revoke(model.model, slot.fingerprint)">
                      {{ t(`${prefix}.${busy === slot.fingerprint ? 'revoking' : 'revoke'}`) }}
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </template>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import {
  getCodexTicketVault,
  revokeCodexTicketVault,
  type CodexTicketVault,
  type CodexTicketVaultModel,
  type CodexTicketVaultSlot
} from '@/api/admin/codexTicketVault'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'

// 票库只展示后端返回的元数据；接口不返回 STATE、Cookie 值或 token，无需二次验证。
const props = defineProps<{ show: boolean; account: { id: number; name: string } | null }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const appStore = useAppStore()
const prefix = 'admin.accounts.codexTicketVault'
const lifecycle = 'admin.accounts.codexTicketHistory.lifecycle'
const columns = ['slot', 'status', 'fingerprint', 'credential', 'node', 'age', 'matureAt', 'expiresAt', 'capturedAt', 'harvest', 'invalidation', 'actions']
const statuses = ['available', 'maturing', 'revoked', 'consumed', 'binding', 'credential', 'cookie_missing', 'expired', 'unverified', 'unavailable']
const lifecycleReasons = ['response_model_mismatch', 'response_ticket_rejected', 'cookie_changed', 'ttl_expired', 'admin_revoked']
const lifecycleSources = ['http', 'websocket', 'websocket_handshake', 'websocket_prewarm', 'ticket_vault']

const loading = ref(false)
const error = ref('')
const warning = ref('')
const vault = ref<CodexTicketVault | null>(null)
// busy 为正在作废的指纹，或 `all:模型`。
const busy = ref('')
const confirmModel = ref('')
let sequence = 0

const models = computed(() => vault.value?.models ?? [])
const overviewRows = computed(() => {
  const v = vault.value
  if (!v) return []
  const onOff = (value: boolean) => t(`${prefix}.values.${value ? 'on' : 'off'}`)
  const seconds = (count: number) => t(`${prefix}.seconds`, { count })
  const rows = [
    { key: 'ticketEnabled', value: onOff(v.ticket_enabled) },
    { key: 'harvestEnabled', value: onOff(v.harvest_enabled) },
    { key: 'configEnabled', value: onOff(v.config_enabled) },
    { key: 'proxy', value: t(`${prefix}.values.${v.proxy_available ? 'available' : 'unavailable'}`) },
    { key: 'credentialMode', value: v.credential_mode || '—' },
    { key: 'poolCapacity', value: String(v.pool_capacity) },
    { key: 'ttl', value: seconds(v.ttl_seconds) },
    { key: 'cookieTtl', value: seconds(v.cookie_ttl_seconds) },
    { key: 'usageMode', value: v.policy.usage_mode === 'aged' ? t(`${prefix}.usageModes.aged`) : t(`${prefix}.usageModes.immediate`) }
  ]
  if (v.account_pool_capacity) rows.push({ key: 'accountPoolCapacity', value: String(v.account_pool_capacity) })
  if (v.policy.usage_mode === 'aged') rows.push({ key: 'minAge', value: seconds(v.policy.min_ticket_age_seconds) })
  if (v.policy.usage_mode === 'aged' && v.policy.historical_ticket_validity_seconds) {
    rows.push({ key: 'historicalValidity', value: seconds(v.policy.historical_ticket_validity_seconds) })
  }
  rows.push(
    { key: 'consumeAfterUse', value: onOff(v.policy.consume_after_use) },
    { key: 'failClosed', value: onOff(v.policy.fail_closed) },
    { key: 'serverTime', value: timeText(v.server_time) }
  )
  return rows
})

function timeText(value?: string): string {
  return value ? formatDateTime(value) : '—'
}
function statusText(status: string): string {
  return statuses.includes(status) ? t(`${prefix}.statuses.${status}`) : status
}
function statusClass(status: string): string {
  if (status === 'available') return 'text-emerald-600 dark:text-emerald-400'
  if (status === 'maturing') return 'text-amber-600 dark:text-amber-400'
  return 'text-red-600 dark:text-red-400'
}
function reasonText(reason: string): string {
  return lifecycleReasons.includes(reason) ? t(`${lifecycle}.reasons.${reason}`) : reason || '—'
}
function sourceText(source: string): string {
  return lifecycleSources.includes(source) ? t(`${lifecycle}.sources.${source}`) : source || '—'
}
function nodeText(slot: CodexTicketVaultSlot): string {
  const node = slot.route_node
  if (!node) return '—'
  return [node.name || node.host, node.country, node.region].filter(Boolean).join(' · ') || '—'
}
function activeSlots(model: CodexTicketVaultModel): CodexTicketVaultSlot[] {
  return (model.slots ?? []).filter(slot => slot.status !== 'revoked')
}

async function load() {
  const account = props.account
  if (!account) return
  const current = ++sequence
  loading.value = true
  error.value = ''
  try {
    const data = await getCodexTicketVault(account.id)
    if (current === sequence) vault.value = data
  } catch (err) {
    if (current === sequence) error.value = extractApiErrorMessage(err, t(`${prefix}.failed`))
  } finally {
    if (current === sequence) loading.value = false
  }
}

async function revoke(model: string, fingerprint?: string) {
  const account = props.account
  if (!account || busy.value) return
  busy.value = fingerprint ?? `all:${model}`
  warning.value = ''
  try {
    const result = await revokeCodexTicketVault(account.id, fingerprint ? { model, fingerprint } : { model, all: true })
    if (props.account?.id !== account.id) return
    if (result.remaining > 0) warning.value = t(`${prefix}.revokeRemaining`, { count: result.remaining })
    appStore.showSuccess(result.revoked > 0 ? t(`${prefix}.revoked`, { count: result.revoked }) : t(`${prefix}.revokeNoop`))
  } catch (err) {
    if (props.account?.id === account.id) appStore.showError(extractApiErrorMessage(err, t(`${prefix}.revokeFailed`)))
  } finally {
    busy.value = ''
    confirmModel.value = ''
  }
  if (props.show && props.account?.id === account.id) await load()
}

watch(() => [props.show, props.account?.id], () => {
  sequence++
  vault.value = null
  error.value = ''
  warning.value = ''
  busy.value = ''
  confirmModel.value = ''
  loading.value = false
  if (props.show && props.account) void load()
}, { immediate: true })
</script>
