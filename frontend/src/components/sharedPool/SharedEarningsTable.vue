<template>
  <div>
    <p v-if="error" role="alert" class="mb-3 text-sm text-red-500">{{ error }} <button class="underline" @click="load">{{ t('common.refresh') }}</button></p>
    <section v-if="admin" class="mb-5 space-y-2" data-test="user-earnings-summary">
      <div>
        <h3 class="font-semibold text-gray-900 dark:text-white">{{ t('sharedPool.userEarningsTitle') }}</h3>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('sharedPool.userEarningsHint') }}</p>
      </div>
      <div v-if="userTotalsLoading" class="py-5 text-center text-sm text-gray-500">{{ t('common.loading') }}</div>
      <p v-else-if="userTotalsError" role="alert" class="text-sm text-red-500">{{ userTotalsError }}</p>
      <div v-else-if="userTotals.length" class="table-container overflow-x-auto">
        <table class="w-full min-w-[900px] text-left text-sm">
          <thead>
            <tr class="border-b border-gray-200 dark:border-dark-700">
              <th class="p-3">{{ t('sharedPool.owner') }}</th>
              <th class="p-3">{{ t('sharedPool.accountsCount') }}</th>
              <th class="p-3">{{ t('sharedPool.accountTiers') }}</th>
              <th class="p-3">{{ t('sharedPool.entriesCount') }}</th>
              <th class="p-3">{{ t('sharedPool.total') }}</th>
              <th class="p-3">{{ t('sharedPool.available') }}</th>
              <th class="p-3">{{ t('sharedPool.pending') }}</th>
              <th class="p-3">{{ t('sharedPool.transferred') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in userTotals" :key="item.user_id" class="border-b border-gray-100 dark:border-dark-700">
              <td class="p-3"><p class="max-w-64 break-all font-medium text-gray-900 dark:text-white">{{ item.email || `#${item.user_id}` }}</p><p class="mt-1 text-xs text-gray-500 dark:text-gray-400">#{{ item.user_id }}</p></td>
              <td class="p-3">{{ item.account_count }}</td>
              <td class="p-3" data-test="account-tiers">
                <div v-if="item.account_tiers?.length" class="flex max-w-72 flex-wrap gap-1">
                  <span
                    v-for="tier in item.account_tiers"
                    :key="`${tier.tier}-${tier.count}`"
                    class="rounded bg-gray-100 px-2 py-0.5 text-xs text-gray-700 dark:bg-dark-700 dark:text-gray-300"
                  >{{ tier.tier || t('sharedPool.unknownTier') }} × {{ tier.count }}</span>
                </div>
                <span v-else class="text-gray-400">—</span>
              </td>
              <td class="p-3">{{ item.earnings_count }}</td>
              <td class="p-3 font-medium text-emerald-600 dark:text-emerald-400">{{ money(item.total_earned) }}</td>
              <td class="p-3">{{ money(item.available) }}</td>
              <td class="p-3">{{ money(item.pending) }}</td>
              <td class="p-3">{{ money(item.transferred) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-else class="py-3 text-sm text-gray-500">{{ t('sharedPool.noEarnings') }}</p>
    </section>
    <div v-if="loading" class="py-8 text-center text-sm text-gray-500">{{ t('common.loading') }}</div>
    <div v-else-if="!items.length" class="py-10 text-center text-sm text-gray-500">{{ t('sharedPool.noEarnings') }}</div>
    <div v-else class="table-container overflow-x-auto">
      <table class="w-full min-w-[860px] text-left text-sm">
        <thead>
          <tr class="border-b border-gray-200 dark:border-dark-700">
            <th class="p-3">{{ t('sharedPool.time') }}</th>
            <th class="p-3">{{ t('sharedPool.name') }}</th>
            <th v-if="admin" class="p-3">{{ t('sharedPool.groups') }}</th>
            <th class="p-3">{{ t('sharedPool.billed') }}</th>
            <th class="p-3">{{ t('sharedPool.settlementAmount') }}</th>
            <th class="p-3">{{ t('sharedPool.revenueSplit') }}</th>
            <th class="p-3">{{ t('sharedPool.ownerAmount') }}</th>
            <th class="p-3">{{ t('sharedPool.platformAmount') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in items" :key="item.id" class="border-b border-gray-100 dark:border-dark-700">
            <td class="whitespace-nowrap p-3">{{ formatDateTime(item.created_at) }}</td>
            <td class="p-3">{{ item.account_name || `#${item.account_id}` }}</td>
            <td v-if="admin" class="p-3">{{ item.group_name || `#${item.group_id}` }}</td>
            <td class="p-3">{{ money(item.billing_amount) }}</td>
            <td class="whitespace-nowrap p-3" data-test="settlement-amount"><p>{{ item.settlement_amount == null ? '—' : money(item.settlement_amount) }}</p><p v-if="item.base_amount != null && item.settlement_multiplier != null" class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('sharedPool.baseAmount') }} {{ money(item.base_amount) }} × {{ item.settlement_multiplier }}</p><p v-if="item.spread_amount != null" class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('sharedPool.spreadAmount') }} {{ money(item.spread_amount) }}</p></td>
            <td class="whitespace-nowrap p-3 text-xs">
              <p>{{ t('sharedPool.platformShare') }} {{ item.platform_rate_bps / 100 }}%</p>
              <p class="mt-1 text-gray-500 dark:text-dark-400">{{ t('sharedPool.proxyShare') }} {{ item.proxy_rate_bps / 100 }}%</p>
            </td>
            <td class="p-3 text-emerald-600 dark:text-emerald-400">{{ money(item.owner_amount) }}</td>
            <td class="p-3">{{ money(item.platform_amount) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <Pagination v-if="total > 20" :page="page" :page-size="20" :total="total" :show-page-size-selector="false" @update:page="changePage" />
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Pagination from '@/components/common/Pagination.vue'
import { formatDateTime } from '@/utils/format'
import { adminSharedPoolAPI, sharedPoolAPI, type SharedEarning, type SharedUserEarnings } from '@/api/sharedPool'
const props = defineProps<{ admin?: boolean }>()
const { t } = useI18n()
const items = ref<SharedEarning[]>([])
const page = ref(1)
const total = ref(0)
const error = ref('')
const loading = ref(false)
const userTotals = ref<SharedUserEarnings[]>([])
const userTotalsLoading = ref(false)
const userTotalsError = ref('')
const money = (amount: number) => `$${Number(amount || 0).toFixed(6)}`
async function load() {
  loading.value = true; error.value = ''
  userTotalsError.value = ''
  if (props.admin) {
    userTotalsLoading.value = true
    try { userTotals.value = await adminSharedPoolAPI.userEarnings() }
    catch (e: unknown) { userTotalsError.value = (e as Error).message || t('sharedPool.loadFailed') }
    finally { userTotalsLoading.value = false }
  }
  try {
    const result = await (props.admin ? adminSharedPoolAPI : sharedPoolAPI).earnings(page.value)
    items.value = result.items || []; total.value = result.total
  } catch (e: unknown) { error.value = (e as Error).message || t('sharedPool.loadFailed') }
  finally { loading.value = false }
}
function changePage(value: number) { page.value = value; void load() }
onMounted(load)
</script>
