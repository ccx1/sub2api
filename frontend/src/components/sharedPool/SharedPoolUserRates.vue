<template>
  <div class="min-w-0 space-y-5">
    <form class="card overflow-hidden" @submit.prevent="save">
      <div class="border-b border-gray-200 px-5 py-4 dark:border-dark-700 sm:px-6"><h2 class="flex items-center gap-2 font-semibold text-gray-900 dark:text-white"><Icon name="userCircle" size="sm" class="text-cyan-600 dark:text-cyan-400" />{{ t('sharedPool.userRateEditor') }}</h2><p class="mt-1.5 text-sm text-gray-500 dark:text-gray-400">{{ t('sharedPool.userRateHint') }}</p></div>
      <div class="grid gap-4 p-5 sm:grid-cols-2 sm:p-6 xl:grid-cols-5 xl:items-end">
        <div><label for="rate-user-id" class="input-label">{{ t('sharedPool.userId') }}</label><input id="rate-user-id" ref="userInput" v-model.number="userID" type="number" min="1" step="1" required class="input w-full" :placeholder="t('sharedPool.userIdPlaceholder')" /></div>
        <div><label for="user-platform-rate" class="input-label">{{ t('sharedPool.platformShare') }} (%)</label><input id="user-platform-rate" v-model="platformRate" type="number" min="0" max="100" step="0.01" class="input w-full" :placeholder="t('sharedPool.inherit')" /></div>
        <div><label for="user-proxy-rate" class="input-label">{{ t('sharedPool.proxyShare') }} (%)</label><input id="user-proxy-rate" v-model="proxyRate" type="number" min="0" max="100" step="0.01" class="input w-full" :placeholder="t('sharedPool.inherit')" /></div>
        <div><label for="user-settlement-multiplier" class="input-label">{{ t('sharedPool.settlementMultiplier') }}</label><input id="user-settlement-multiplier" v-model="settlementMultiplier" type="number" min="0" max="100" step="any" class="input w-full" :placeholder="t('sharedPool.inherit')" /></div>
        <div class="flex gap-2 sm:justify-end"><button v-if="userID" type="button" class="btn btn-secondary" :disabled="saving" @click="reset">{{ t('common.cancel') }}</button><button class="btn btn-primary flex-1 sm:flex-none" :disabled="saving"><Icon :name="saving ? 'refresh' : 'check'" size="sm" class="mr-1.5" :class="{ 'animate-spin': saving }" />{{ saving ? t('common.saving') : t('common.save') }}</button></div>
      </div>
    </form>
    <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
    <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('sharedPool.userRateList') }}</h2>
    <div v-if="loading" role="status" class="card flex items-center justify-center gap-2 py-12 text-sm text-gray-500 dark:text-gray-400"><Icon name="refresh" class="animate-spin" />{{ t('common.loading') }}</div>
    <div v-else-if="!rates.length" class="card flex flex-col items-center px-5 py-12 text-center"><Icon name="users" size="xl" class="mb-3 text-gray-400" /><p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('sharedPool.noUserRates') }}</p><p class="mt-2 text-sm text-gray-500 dark:text-gray-400">{{ t('sharedPool.noUserRatesHint') }}</p></div>
    <div v-else class="table-container max-w-full overflow-x-auto">
      <table class="w-full min-w-[620px] text-left text-sm">
        <thead class="bg-gray-50/70 text-xs text-gray-500 dark:bg-dark-800/60 dark:text-gray-400"><tr><th class="px-5 py-3 font-medium">{{ t('sharedPool.owner') }}</th><th class="px-5 py-3 font-medium">{{ t('sharedPool.platformShare') }}</th><th class="px-5 py-3 font-medium">{{ t('sharedPool.proxyShare') }}</th><th class="px-5 py-3 font-medium">{{ t('sharedPool.settlementMultiplier') }}</th><th class="px-5 py-3 text-right font-medium">{{ t('common.actions') }}</th></tr></thead>
        <tbody>
          <tr v-for="rate in rates" :key="rate.user_id" class="border-t border-gray-100 hover:bg-gray-50/50 dark:border-dark-700 dark:hover:bg-dark-700/20">
            <td class="px-5 py-4"><p class="max-w-64 break-all font-medium text-gray-900 dark:text-white">{{ rate.email || `#${rate.user_id}` }}</p><p class="mt-1 text-xs text-gray-500 dark:text-gray-400">#{{ rate.user_id }}</p></td>
            <td class="px-5 py-4 tabular-nums">{{ formatRate(rate.platform_rate_bps) }}</td>
            <td class="px-5 py-4 tabular-nums">{{ formatRate(rate.proxy_rate_bps) }}</td>
            <td class="px-5 py-4 tabular-nums" data-test="user-multiplier">{{ rate.settlement_multiplier == null ? t('sharedPool.inherit') : `${rate.settlement_multiplier}x` }}</td>
            <td class="px-5 py-4 text-right"><button type="button" class="btn btn-secondary btn-sm" :disabled="saving" @click="edit(rate)"><Icon name="edit" size="sm" class="mr-1.5" />{{ t('common.edit') }}</button></td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminSharedPoolAPI, type SharedUserRate } from '@/api/sharedPool'
import Icon from '@/components/icons/Icon.vue'
import { validSettlementMultiplier } from './settlementPolicy'
const emit = defineEmits<{ saved: [] }>()
const { t } = useI18n()
const app = useAppStore()
const rates = ref<SharedUserRate[]>([])
const userID = ref<number>()
const userInput = ref<HTMLInputElement>()
const platformRate = ref('')
const proxyRate = ref('')
const settlementMultiplier = ref('')
const error = ref('')
const saving = ref(false)
const loading = ref(false)
const formatRate = (rate: number | null) => rate == null ? t('sharedPool.inherit') : `${rate / 100}%`
function edit(rate: SharedUserRate) {
  userID.value = rate.user_id
  platformRate.value = rate.platform_rate_bps == null ? '' : String(rate.platform_rate_bps / 100)
  proxyRate.value = rate.proxy_rate_bps == null ? '' : String(rate.proxy_rate_bps / 100)
  settlementMultiplier.value = rate.settlement_multiplier == null ? '' : String(rate.settlement_multiplier)
  userInput.value?.focus()
}
function reset() { userID.value = undefined; platformRate.value = ''; proxyRate.value = ''; settlementMultiplier.value = ''; error.value = '' }
async function load() {
  loading.value = true
  try { rates.value = await adminSharedPoolAPI.userRates() }
  catch (e: unknown) { error.value = (e as Error).message || t('sharedPool.loadFailed') }
  finally { loading.value = false }
}
async function save() {
  if (saving.value || !userID.value) return
  saving.value = true; error.value = ''
  try {
    const toBPS = (value: string) => value === '' ? null : Math.round(Number(value) * 100)
    const multiplier = settlementMultiplier.value === '' ? null : Number(settlementMultiplier.value)
    if (multiplier !== null && !validSettlementMultiplier(multiplier)) throw new Error(t('sharedPool.invalidSettlementMultiplier'))
    await adminSharedPoolAPI.saveUserRate(userID.value, { platform_rate_bps: toBPS(platformRate.value), proxy_rate_bps: toBPS(proxyRate.value), settlement_multiplier: multiplier })
    emit('saved')
    app.showSuccess(t('sharedPool.saved')); await load()
  } catch (e: unknown) { error.value = (e as Error).message || t('sharedPool.actionFailed') }
  finally { saving.value = false }
}
onMounted(load)
</script>
