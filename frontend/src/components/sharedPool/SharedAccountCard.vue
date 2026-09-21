<template>
  <article class="card min-w-0 p-4">
    <div class="flex items-start justify-between gap-3">
      <div class="min-w-0"><h3 class="break-words text-sm font-semibold">{{ account.name }}</h3><p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ account.platform }} · {{ account.type.toUpperCase() }}<template v-if="account.subscription_tier"> · {{ t('sharedPool.subscriptionTier') }} {{ subscriptionTierLabel }}</template> · {{ t('sharedPool.concurrency') }} {{ account.concurrency }}</p></div>
      <span class="shrink-0 rounded px-1.5 py-0.5 text-xs" :class="account.admin_disabled || account.status !== 'active' ? 'bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-400' : account.enabled ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-400' : 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-dark-400'">{{ status }}</span>
    </div>
    <p v-if="admin" class="mt-2 break-all text-xs text-gray-500">{{ t('sharedPool.owner') }}: {{ account.owner_email || `#${account.owner_user_id}` }}</p>
    <p v-if="account.error_message" class="mt-2 break-words text-xs text-red-600 dark:text-red-400">{{ account.error_message }}</p>
    <p v-if="dailyCooldown.enabled" class="mt-2 break-words text-xs text-gray-500 dark:text-dark-400" data-test="daily-cooldown-summary" :title="t('sharedPool.dailyCooldownHint')">{{ t('sharedPool.dailyCooldownSummary', { start: dailyCooldown.start, end: dailyCooldown.end, timezone: dailyCooldown.timezone }) }}</p>
    <div v-if="admin" class="mt-3 flex flex-wrap gap-1.5" data-test="account-groups">
      <span v-for="group in account.groups" :key="group.id" class="max-w-full break-words rounded border border-gray-200 px-1.5 py-0.5 text-xs dark:border-dark-600">{{ group.name }}</span>
      <span v-if="!account.groups?.length" class="text-xs text-amber-600">{{ t('sharedPool.noGroup') }}</span>
    </div>
    <SharedAccountUsage v-if="!admin && account.type === 'oauth'" :account="account" :busy="busy" @busy-change="usageBusy = $event" @usage-updated="emit('usageUpdated')" />
    <dl class="mt-3 grid grid-cols-3 gap-2 text-sm">
      <div class="min-w-0"><dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('sharedPool.today') }}</dt><dd class="mt-1 break-all font-semibold tabular-nums">{{ money(account.today_earnings) }}</dd></div>
      <div class="min-w-0"><dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('sharedPool.total') }}</dt><dd class="mt-1 break-all font-semibold tabular-nums">{{ money(account.total_earnings) }}</dd></div>
      <div class="min-w-0" :title="t('sharedPool.estimateHint')"><dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('sharedPool.estimate') }}</dt><dd class="mt-1 break-all tabular-nums">{{ account.estimated_earnings == null ? t('sharedPool.unavailable') : money(account.estimated_earnings) }}</dd></div>
    </dl>
    <SharedRevenueSplit class="mt-3 leading-relaxed" :config="account" :use-random-proxy="account.proxy_mode === 'random'" inline />
    <p class="mt-2 text-xs text-gray-500 dark:text-dark-400" data-test="account-settlement">{{ account.dispatch_consent ? t('sharedPool.settlementMultiplier') : t('sharedPool.legacySettlement') }}<template v-if="account.dispatch_consent && account.subscription_tier"> · {{ subscriptionTierLabel }}</template><span v-if="account.dispatch_consent"> {{ validSettlementMultiplier(account.settlement_multiplier) ? `${account.settlement_multiplier}x` : '—' }}</span></p>
    <div class="mt-2 flex flex-wrap justify-between gap-x-3 gap-y-1 text-xs text-gray-500 dark:text-dark-400">
      <span>{{ t(account.proxy_mode === 'random' ? 'sharedPool.randomProxy' : 'sharedPool.customProxy') }}</span>
      <span :title="t('sharedPool.lastUsed')">{{ account.last_used_at ? formatDateTime(account.last_used_at) : t('sharedPool.neverUsed') }}</span>
    </div>
    <div class="mt-3 border-t border-gray-200 pt-3 dark:border-dark-700">
      <template v-if="!admin">
        <div v-if="!account.dispatch_consent" class="mb-3 space-y-2 text-xs text-amber-700 dark:text-amber-400">
          <p>{{ t('sharedPool.legacyConsentHint') }}</p>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="actionBusy" data-test="authorize-dispatch" @click="emit('authorize')">{{ t('sharedPool.authorizeDispatch') }}</button>
        </div>
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="flex items-center gap-2" :title="t('sharedPool.sharingHint')">
            <span class="text-xs text-gray-600 dark:text-dark-300">{{ t(account.dispatch_consent ? 'sharedPool.sharing' : 'sharedPool.legacySharing') }}</span>
            <Toggle :model-value="account.enabled" :aria-label="t(account.dispatch_consent ? 'sharedPool.sharing' : 'sharedPool.legacySharing')" :disabled="actionBusy" class="disabled:cursor-not-allowed disabled:opacity-50" @update:model-value="!actionBusy && emit('enable', $event)" />
          </div>
          <div class="flex items-center gap-2" :title="t('sharedPool.protectionHint')">
            <span class="text-xs text-gray-600 dark:text-dark-300">{{ t('sharedPool.protection') }}</span>
            <Toggle :model-value="account.protection_enabled" :aria-label="t('sharedPool.protection')" :disabled="actionBusy" class="disabled:cursor-not-allowed disabled:opacity-50" @update:model-value="!actionBusy && emit('protection', $event)" />
          </div>
          <div v-if="account.platform === 'openai' && account.type === 'oauth' && (account.codex_ticket_required || typeof account.codex_ticket_enabled === 'boolean')" class="flex items-center gap-2" :title="t(account.codex_ticket_required ? 'sharedPool.codexTicketRequiredHint' : 'sharedPool.codexTicketHint')">
            <span class="text-xs text-gray-600 dark:text-dark-300">{{ t('sharedPool.codexTicket') }}</span>
            <span v-if="account.codex_ticket_required" class="text-xs text-cyan-600 dark:text-cyan-400" data-test="ticket-required">{{ t('sharedPool.codexTicketRequired') }}</span>
            <Toggle :model-value="Boolean(account.codex_ticket_required || account.codex_ticket_enabled)" :aria-label="t('sharedPool.codexTicket')" :disabled="actionBusy || account.codex_ticket_required" class="disabled:cursor-not-allowed disabled:opacity-50" @update:model-value="!actionBusy && !account.codex_ticket_required && emit('codexTicket', $event)" />
          </div>
        </div>
        <div class="mt-3 flex flex-wrap items-center gap-2">
          <button type="button" class="btn btn-secondary btn-sm" :disabled="actionBusy" @click="emit('edit')">{{ t('common.edit') }}</button>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="actionBusy" @click="emit('test')">{{ t('sharedPool.test') }}</button>
          <button type="button" class="btn btn-ghost btn-sm ml-auto text-red-600 dark:text-red-400" :disabled="actionBusy" @click="emit('remove')">{{ t('sharedPool.remove') }}</button>
        </div>
      </template>
      <button v-else type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="emit('allocate')">{{ t('sharedPool.allocation') }}</button>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatDateTime } from '@/utils/format'
import { normalizeDailyCooldown } from '@/utils/dailyCooldown'
import type { SharedAccount } from '@/api/sharedPool'
import Toggle from '@/components/common/Toggle.vue'
import SharedRevenueSplit from './SharedRevenueSplit.vue'
import SharedAccountUsage from './SharedAccountUsage.vue'
import { subscriptionTierOptions, validSettlementMultiplier } from './settlementPolicy'
const props = defineProps<{ account: SharedAccount; busy?: boolean; admin?: boolean }>()
const emit = defineEmits<{ enable: [enabled: boolean]; authorize: []; protection: [enabled: boolean]; codexTicket: [enabled: boolean]; edit: []; test: []; remove: []; allocate: []; usageUpdated: [] }>()
const usageBusy = ref(false)
const actionBusy = computed(() => props.busy || usageBusy.value)
const dailyCooldown = computed(() => normalizeDailyCooldown(props.account.daily_cooldown))
const { t, te } = useI18n()
const subscriptionTierLabel = computed(() => {
  const tier = props.account.subscription_tier
  if (!tier) return ''
  return subscriptionTierOptions[props.account.platform].find(option => option.value === tier)?.label || tier
})
const money = (amount: number) => `$${Number(amount || 0).toFixed(4)}`
const status = computed(() => {
  const account = props.account
  if (account.admin_disabled) return t('sharedPool.adminDisabled')
  if (!account.enabled) return t('sharedPool.disabled')
  if (props.admin && !account.group_ids?.length) return t('sharedPool.waiting')
  const key = `sharedPool.status.${account.status}`
  return te(key) ? t(key) : account.status
})
</script>
