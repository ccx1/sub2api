<template>
  <div v-if="loading" role="status" class="card flex items-center justify-center gap-3 py-16 text-sm text-gray-500 dark:text-gray-400">
    <Icon name="refresh" class="animate-spin" />{{ t('common.loading') }}
  </div>
  <div v-else-if="!accounts.length" class="card flex flex-col items-center px-6 py-14 text-center">
    <Icon name="users" size="xl" class="mb-3 text-gray-400 dark:text-gray-500" />
    <h3 class="font-medium text-gray-900 dark:text-white">{{ t(searching ? 'sharedPool.noMatchingAccounts' : 'sharedPool.emptyAccounts') }}</h3>
    <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">{{ t(searching ? 'sharedPool.searchAgainHint' : 'sharedPool.adminEmptyAccountsHint') }}</p>
  </div>
  <div v-else class="table-container max-w-full overflow-x-auto">
    <table class="w-full min-w-[1260px] text-left text-sm">
      <thead class="bg-gray-50/70 text-xs text-gray-500 dark:bg-dark-800/60 dark:text-gray-400">
        <tr class="border-b border-gray-200 dark:border-dark-700">
          <th class="px-4 py-3 font-medium">{{ t('sharedPool.name') }}</th>
          <th class="px-4 py-3 font-medium">{{ t('sharedPool.subscriptionTier') }}</th>
          <th class="px-4 py-3 font-medium">{{ t('sharedPool.owner') }}</th>
          <th class="px-4 py-3 font-medium">{{ t('sharedPool.groups') }}</th>
          <th class="px-4 py-3 font-medium">{{ t('common.status') }}</th>
          <th class="px-4 py-3 font-medium">{{ t('sharedPool.revenueSplit') }}</th>
          <th class="px-4 py-3 text-right font-medium">{{ t('sharedPool.ownerAmount') }}</th>
          <th class="shared-account-actions px-4 py-3 text-right font-medium sm:sticky sm:right-0">{{ t('common.actions') }}</th>
        </tr>
      </thead>
      <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
        <tr v-for="account in accounts" :key="account.id" class="align-top transition-colors hover:bg-gray-50/50 dark:hover:bg-dark-700/20">
          <td class="px-4 py-4">
            <div class="flex items-start gap-2.5"><span class="rounded-lg border border-gray-200 p-2 text-gray-700 dark:border-dark-600 dark:text-gray-200"><PlatformIcon :platform="account.platform" size="md" /></span><div class="min-w-0"><p class="max-w-52 break-words font-medium text-gray-900 dark:text-white">{{ account.name }}</p><p class="mt-1 text-xs text-gray-500 dark:text-gray-400">#{{ account.id }} · {{ account.platform }} · {{ account.type.toUpperCase() }}</p></div></div>
            <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ t('sharedPool.concurrency') }} {{ account.concurrency }} · {{ t(account.proxy_mode === 'random' ? 'sharedPool.randomProxy' : 'sharedPool.customProxy') }}</p>
            <p v-if="account.priority !== undefined" class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.priority') }} {{ account.priority }}</p>
          </td>
          <td class="px-4 py-4" data-test="admin-account-tier"><p class="whitespace-nowrap font-medium text-gray-900 dark:text-white">{{ account.type === 'oauth' ? tierLabel(account) : '—' }}</p><p v-if="account.type === 'oauth'" class="mt-2 whitespace-nowrap text-xs text-gray-500 dark:text-gray-400">{{ t(account.subscription_tier_override ? 'sharedPool.subscriptionTierManual' : 'sharedPool.subscriptionTierAutomatic') }}</p></td>
          <td class="px-4 py-4"><p class="max-w-48 break-all text-gray-800 dark:text-gray-200">{{ account.owner_email || '—' }}</p><p class="mt-1 text-xs text-gray-500 dark:text-gray-400">#{{ account.owner_user_id }}</p></td>
          <td class="px-4 py-4"><div class="flex max-w-44 flex-wrap gap-1.5"><span v-for="group in visibleGroups(account)" :key="group.id" class="rounded border border-cyan-200/70 bg-cyan-50/60 px-2 py-1 text-xs text-cyan-700 dark:border-cyan-800/50 dark:bg-cyan-950/30 dark:text-cyan-300">{{ group.name }}</span><span v-if="hiddenGroupCount(account)" data-test="admin-account-groups-more" class="cursor-help rounded border border-cyan-200/70 bg-cyan-50/60 px-2 py-1 text-xs text-cyan-700 dark:border-cyan-800/50 dark:bg-cyan-950/30 dark:text-cyan-300" :title="allGroupNames(account)" :aria-label="allGroupNames(account)">+{{ hiddenGroupCount(account) }}</span><span v-if="!account.groups?.length" class="text-xs text-amber-600 dark:text-amber-400">{{ t('sharedPool.noGroup') }}</span></div></td>
          <td class="px-4 py-4">
            <span class="inline-flex items-center gap-1.5 whitespace-nowrap rounded-full px-2 py-1 text-xs" :class="statusClass(account)"><span class="h-1.5 w-1.5 rounded-full bg-current"></span>{{ statusLabel(account) }}</span>
            <p class="mt-2 flex items-center gap-1 text-xs text-gray-500 dark:text-gray-400"><Icon name="shield" size="xs" />{{ t('sharedPool.protection') }} · {{ t(account.protection_enabled ? 'sharedPool.enabled' : 'sharedPool.disabled') }}</p>
            <p v-if="account.error_message" class="mt-2 max-w-44 break-words text-xs text-red-600 dark:text-red-400">{{ account.error_message }}</p>
          </td>
          <td class="px-4 py-4 text-xs">
            <template v-if="Number.isFinite(account.platform_rate_bps) && Number.isFinite(account.proxy_rate_bps)"><p class="whitespace-nowrap text-gray-800 dark:text-gray-200">{{ t('sharedPool.platformShare') }} <span class="font-medium tabular-nums">{{ account.platform_rate_bps / 100 }}%</span></p><p class="mt-2 whitespace-nowrap text-gray-500 dark:text-gray-400">{{ t('sharedPool.proxyShare') }} <span class="tabular-nums">{{ account.proxy_mode === 'random' ? account.proxy_rate_bps / 100 : 0 }}%</span></p></template>
            <span v-else class="text-gray-500 dark:text-gray-400">{{ t('sharedPool.rateUnavailable') }}</span>
            <p class="mt-2 text-gray-500 dark:text-gray-400" data-test="admin-account-multiplier">{{ account.dispatch_consent ? `${t('sharedPool.settlementMultiplier')} ${account.settlement_multiplier ?? '—'}x` : t('sharedPool.legacySettlement') }}</p>
          </td>
          <td class="whitespace-nowrap px-4 py-4 text-right text-xs tabular-nums"><p class="text-gray-800 dark:text-gray-200">{{ t('sharedPool.total') }} <span class="font-semibold">{{ money(account.total_earnings) }}</span></p><p class="mt-2 text-emerald-600 dark:text-emerald-400">{{ t('sharedPool.today') }} {{ money(account.today_earnings) }}</p><p class="mt-2 text-gray-500 dark:text-gray-400" :title="t('sharedPool.estimateHint')">{{ t('sharedPool.estimate') }} {{ account.estimated_earnings == null ? '—' : money(account.estimated_earnings) }}</p></td>
          <td class="shared-account-actions px-4 py-4 text-right sm:sticky sm:right-0"><button type="button" class="btn btn-secondary btn-sm whitespace-nowrap" @click="emit('allocate', account)"><Icon name="link" size="sm" class="mr-1.5" />{{ t('sharedPool.allocation') }}</button><p class="mt-3 whitespace-nowrap text-xs text-gray-500 dark:text-gray-400" :title="t('sharedPool.lastUsed')">{{ account.last_used_at ? formatDateTime(account.last_used_at) : t('sharedPool.neverUsed') }}</p></td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import type { SharedAccount } from '@/api/sharedPool'
import { subscriptionTierOptions } from './settlementPolicy'
import { formatDateTime } from '@/utils/format'
defineProps<{ accounts: SharedAccount[]; loading?: boolean; searching?: boolean }>()
const emit = defineEmits<{ allocate: [account: SharedAccount] }>()
const { t, te } = useI18n()
const groupPreviewLimit = 3
const money = (amount: number) => `$${Number(amount || 0).toFixed(4)}`
function visibleGroups(account: SharedAccount) { return account.groups?.slice(0, groupPreviewLimit) || [] }
function hiddenGroupCount(account: SharedAccount) { return Math.max(0, (account.groups?.length || 0) - groupPreviewLimit) }
function allGroupNames(account: SharedAccount) { return (account.groups || []).map(group => group.name).join('\n') }
function tierLabel(account: SharedAccount) {
  return subscriptionTierOptions[account.platform]?.find(tier => tier.value === account.subscription_tier)?.label || t('sharedPool.unknownTier')
}
function statusLabel(account: SharedAccount) {
  if (account.admin_disabled) return t('sharedPool.adminDisabled')
  if (!account.enabled) return t('sharedPool.disabled')
  if (!account.group_ids?.length) return t('sharedPool.waiting')
  const key = `sharedPool.status.${account.status}`
  return te(key) ? t(key) : account.status
}
function statusClass(account: SharedAccount) {
  if (account.admin_disabled || !account.enabled) return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-400'
  if (account.status === 'active' && account.group_ids?.length) return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-400'
  return 'bg-amber-50 text-amber-700 dark:bg-amber-950/40 dark:text-amber-400'
}
</script>

<style scoped>
@media (min-width: 640px) {
  .shared-account-actions {
    background: var(--surface-elevated);
    box-shadow: -1px 0 0 rgba(var(--border-rgb), 0.6);
  }
}
</style>
