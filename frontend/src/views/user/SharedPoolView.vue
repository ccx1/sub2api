<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="flex flex-wrap gap-2" role="tablist" :aria-label="t('sharedPool.title')">
          <button v-for="item in tabs" :key="item" role="tab" :aria-selected="tab === item" :class="tab === item ? 'btn btn-primary' : 'btn btn-secondary'" @click="tab = item">{{ t(`sharedPool.${item}`) }}</button>
        </div>
        <button class="btn btn-secondary" :disabled="loading" @click="load">{{ t('common.refresh') }}</button>
      </div>
      <p v-if="error" role="alert" class="text-sm text-red-500">{{ error }}</p>
      <div v-if="loading" class="py-12 text-center text-gray-500">{{ t('common.loading') }}</div>
      <template v-else>
        <SharedPoolCatalog v-if="tab === 'pools'" :overview="overview" :stale="overviewStale" @contribute="openCreate" />
        <template v-else-if="tab === 'myAccounts'">
          <SharedRevenueSplit v-if="config" :config="config" :use-random-proxy="true" compare-proxy-modes />
          <SharedSettlementNotice v-if="config" :config="config" />
          <div v-if="summary" class="grid grid-cols-2 gap-3 lg:grid-cols-4">
            <div v-for="metric in summaryMetrics" :key="metric.key" class="card min-w-0 p-4"><p class="text-xs text-gray-500 dark:text-dark-400">{{ t(`sharedPool.${metric.key}`) }}</p><p class="mt-2 break-all text-xl font-semibold">{{ money(metric.value) }}</p></div>
          </div>
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div class="flex flex-wrap gap-2">
              <button class="btn btn-primary" :disabled="!config?.platforms.length" @click="openCreate">{{ t('sharedPool.create') }}</button>
              <button class="btn btn-secondary" :disabled="!config?.platforms.length" @click="openImport()">{{ t('sharedPool.importAccounts') }}</button>
            </div>
            <button class="btn btn-secondary" :disabled="transferring || !summary || summary.available <= 0" @click="confirmTransfer = true">{{ t('sharedPool.transfer') }}</button>
          </div>
          <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('sharedPool.pendingHint') }}</p>
          <p v-if="config && !config.platforms.length" class="text-sm text-amber-600">{{ t('sharedPool.noPlatforms') }}</p>
          <div v-if="!accounts.length" class="py-12 text-center"><h2 class="font-semibold">{{ t('sharedPool.emptyAccounts') }}</h2><p class="mt-2 text-sm text-gray-500">{{ t('sharedPool.emptyAccountsHint') }}</p></div>
          <div v-else class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            <SharedAccountCard v-for="account in accounts" :key="account.id" :account="account" :busy="busyIds.has(account.id)" @enable="enable(account, $event)" @authorize="authorizingDispatch = account" @protection="protection(account, $event)" @codex-ticket="codexTicket(account, $event)" @edit="openEdit(account)" @test="testing = account" @remove="removing = account" @usage-updated="refreshAfterUsage" />
          </div>
          <Pagination v-if="total > 12" :page="page" :page-size="12" :total="total" :show-page-size-selector="false" @update:page="changePage" />
        </template>
        <SharedEarningsTable v-else />
      </template>
    </div>
    <SharedAccountDialog v-if="dialogOpen && config" :show="dialogOpen" :account="editing" :config="config" @close="dialogOpen = false" @saved="saved" @import="openImport" />
    <SharedAccountImportDialog v-if="importOpen && config" :show="importOpen" :config="config" :initial-defaults="importDefaults" @close="importOpen = false" @imported="load" />
    <SharedAccountTestDialog v-if="testing" :account="testing" @close="testing = null" @completed="changePage(page)" />
    <SharedDispatchConsentDialog v-if="authorizingDispatch && config" :account="authorizingDispatch" :config="config" @close="authorizingDispatch = null" @confirm="authorizeDispatch" />
    <ConfirmDialog :show="!!removing" :title="t('sharedPool.remove')" :message="t('sharedPool.removeConfirm', { name: removing?.name })" danger @cancel="removing = null" @confirm="remove" />
    <ConfirmDialog :show="!!disablingProtection" :title="t('sharedPool.disableProtection')" :message="t('sharedPool.disableProtectionHint')" danger @cancel="disablingProtection = null" @confirm="confirmDisableProtection" />
    <ConfirmDialog :show="confirmTransfer" :title="t('sharedPool.transferConfirm')" :message="`${money(summary?.available || 0)} · ${t('sharedPool.transferHint')}`" @cancel="confirmTransfer = false" @confirm="transfer" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import SharedPoolCatalog from '@/components/sharedPool/SharedPoolCatalog.vue'
import SharedAccountCard from '@/components/sharedPool/SharedAccountCard.vue'
import SharedAccountDialog from '@/components/sharedPool/SharedAccountDialog.vue'
import SharedAccountImportDialog from '@/components/sharedPool/SharedAccountImportDialog.vue'
import SharedRevenueSplit from '@/components/sharedPool/SharedRevenueSplit.vue'
import SharedSettlementNotice from '@/components/sharedPool/SharedSettlementNotice.vue'
import SharedAccountTestDialog from '@/components/sharedPool/SharedAccountTestDialog.vue'
import SharedDispatchConsentDialog from '@/components/sharedPool/SharedDispatchConsentDialog.vue'
import SharedEarningsTable from '@/components/sharedPool/SharedEarningsTable.vue'
import { sharedPoolAPI, type SharedAccount, type SharedConfig, type SharedPoolOverview, type SharedSummary, type SharedImportDefaults } from '@/api/sharedPool'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const app = useAppStore()
const auth = useAuthStore()
const tabs = ['pools', 'myAccounts', 'earnings'] as const
const tab = ref<typeof tabs[number]>('pools')
const overview = ref<SharedPoolOverview | null>(null)
const overviewStale = ref(false)
const accounts = ref<SharedAccount[]>([])
const config = ref<SharedConfig>()
const summary = ref<SharedSummary>()
const loading = ref(false)
const error = ref('')
const page = ref(1)
const total = ref(0)
const dialogOpen = ref(false)
const importOpen = ref(false)
const importDefaults = ref<SharedImportDefaults>()
const editing = ref<SharedAccount | null>(null)
const testing = ref<SharedAccount | null>(null)
const authorizingDispatch = ref<SharedAccount | null>(null)
const removing = ref<SharedAccount | null>(null)
const disablingProtection = ref<SharedAccount | null>(null)
const busyIds = ref(new Set<number>())
const confirmTransfer = ref(false)
const transferring = ref(false)
let disposed = false
let poolRequestId = 0
let pendingPoolRequests = 0
let poolRefreshTimer: ReturnType<typeof setInterval> | undefined
const money = (value: number) => `$${Number(value || 0).toFixed(4)}`
const summaryMetrics = computed(() => [
  { key: 'total', value: summary.value?.total_earned || 0 }, { key: 'available', value: summary.value?.available || 0 },
  { key: 'pending', value: summary.value?.pending || 0 }, { key: 'transferred', value: summary.value?.transferred || 0 }
])
async function loadAccounts() {
  const result = await sharedPoolAPI.accounts(page.value)
  accounts.value = result.items || []; total.value = result.total
}
async function refreshPools(automatic = false) {
  if (disposed || (automatic && (pendingPoolRequests > 0 || loading.value || tab.value !== 'pools' || document.visibilityState === 'hidden'))) return
  const requestId = ++poolRequestId
  pendingPoolRequests++
  try {
    const result = await sharedPoolAPI.overview()
    if (!disposed && requestId === poolRequestId) { overview.value = result; overviewStale.value = false }
  } catch (e: unknown) {
    if (!disposed && requestId === poolRequestId) {
      overviewStale.value = true
    }
    if (!automatic) throw e
  } finally { pendingPoolRequests-- }
}
function stopPoolRefresh() {
  if (poolRefreshTimer !== undefined) clearInterval(poolRefreshTimer)
  poolRefreshTimer = undefined
}
function syncPoolRefresh() {
  stopPoolRefresh()
  if (!disposed && tab.value === 'pools' && document.visibilityState !== 'hidden') {
    poolRefreshTimer = setInterval(() => { void refreshPools(true) }, 10_000)
  }
}
async function load() {
  loading.value = true; error.value = ''
  const results = await Promise.allSettled([refreshPools(), sharedPoolAPI.config(), sharedPoolAPI.summary(), loadAccounts()])
  const [, configResult, summaryResult] = results
  if (configResult.status === 'fulfilled') config.value = configResult.value
  if (summaryResult.status === 'fulfilled') summary.value = summaryResult.value
  // 概览统计失败由自身状态提示，不能阻止用户管理或贡献账号。
  const failure = results.slice(1).find(result => result.status === 'rejected')
  if (failure?.status === 'rejected') error.value = (failure.reason as Error)?.message || t('sharedPool.loadFailed')
  loading.value = false
}
function openCreate() { tab.value = 'myAccounts'; editing.value = null; dialogOpen.value = !!config.value?.platforms.length }
function openEdit(account: SharedAccount) { editing.value = account; dialogOpen.value = true }
function openImport(defaults?: SharedImportDefaults) { tab.value = 'myAccounts'; dialogOpen.value = false; importDefaults.value = defaults; importOpen.value = !!config.value?.platforms.length }
function saved() { dialogOpen.value = false; app.showSuccess(t('sharedPool.saved')); void load() }
async function action(account: SharedAccount, operation: () => Promise<unknown>) {
  if (busyIds.value.has(account.id)) return
  busyIds.value.add(account.id)
  try { await operation(); await loadAccounts(); await refreshPools() }
  catch (e: unknown) { app.showError((e as Error).message || t('sharedPool.actionFailed')) }
  finally { busyIds.value.delete(account.id) }
}
function enable(account: SharedAccount, enabled: boolean) { void action(account, () => sharedPoolAPI.enable(account.id, enabled)) }
function authorizeDispatch() {
  const account = authorizingDispatch.value
  authorizingDispatch.value = null
  if (account) void action(account, () => sharedPoolAPI.enable(account.id, true, true))
}
function codexTicket(account: SharedAccount, enabled: boolean) { void action(account, () => sharedPoolAPI.codexTicket(account.id, enabled)) }
function protection(account: SharedAccount, enabled: boolean) {
  if (!enabled) disablingProtection.value = account
  else void action(account, () => sharedPoolAPI.protection(account.id, true))
}
function confirmDisableProtection() {
  const account = disablingProtection.value
  disablingProtection.value = null
  if (account) void action(account, () => sharedPoolAPI.protection(account.id, false))
}
function remove() {
  const account = removing.value
  removing.value = null
  if (account) void action(account, () => sharedPoolAPI.remove(account.id))
}
async function transfer() {
  confirmTransfer.value = false
  if (transferring.value || !summary.value || summary.value.available <= 0) return
  transferring.value = true
  try {
    const result = await sharedPoolAPI.transfer()
    summary.value.available = Math.max(0, summary.value.available - result.amount)
    summary.value.transferred += result.amount
    app.showSuccess(t('sharedPool.transferredSuccess', { amount: result.amount.toFixed(4) }))
    const refreshes = await Promise.allSettled([
      sharedPoolAPI.summary().then(value => { summary.value = value }), auth.refreshUser()
    ])
    if (refreshes.some(result => result.status === 'rejected')) app.showWarning(t('sharedPool.refreshFailed'))
  } catch (e: unknown) { app.showError((e as Error).message || t('sharedPool.actionFailed')) }
  finally { transferring.value = false }
}
async function changePage(value: number) {
  page.value = value
  try { await loadAccounts() } catch (e: unknown) { app.showError((e as Error).message || t('sharedPool.loadFailed')) }
}
async function refreshAfterUsage() {
  const results = await Promise.allSettled([loadAccounts(), refreshPools()])
  if (results.some(result => result.status === 'rejected')) app.showWarning(t('sharedPool.refreshFailed'))
}
watch(tab, syncPoolRefresh)
onMounted(() => {
  void load()
  syncPoolRefresh()
  document.addEventListener('visibilitychange', syncPoolRefresh)
})
onBeforeUnmount(() => {
  disposed = true
  stopPoolRefresh()
  document.removeEventListener('visibilitychange', syncPoolRefresh)
})
</script>
