<template>
  <AppLayout>
    <div class="min-w-0 space-y-6">
      <header class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div class="tabs grid w-full grid-cols-2 gap-1 sm:inline-flex sm:w-auto" role="tablist" :aria-label="t('sharedPool.adminTitle')">
          <button v-for="item in tabs" :id="`shared-pool-tab-${item}`" :key="item" type="button" role="tab" class="tab flex items-center justify-center gap-2" :class="{ 'tab-active': tab === item }" :aria-selected="tab === item" :aria-controls="`shared-pool-panel-${item}`" @click="tab = item">
            <Icon :name="tabIcons[item]" size="sm" />{{ t(`sharedPool.${item}`) }}
          </button>
        </div>
        <button type="button" class="btn btn-secondary self-start" :disabled="loading || accountsLoading" @click="load">
          <Icon name="refresh" size="sm" class="mr-2" :class="{ 'animate-spin': loading }" />
          {{ t('common.refresh') }}
        </button>
      </header>
      <p v-if="error" role="alert" class="flex items-center gap-2 rounded-lg border border-red-200 bg-red-50/60 p-3 text-sm text-red-600 dark:border-red-900/50 dark:bg-red-950/20 dark:text-red-400"><Icon name="exclamationCircle" size="sm" />{{ error }}</p>
      <div v-if="loading" role="status" class="flex items-center justify-center gap-3 py-16 text-sm text-gray-500 dark:text-gray-400"><Icon name="refresh" class="animate-spin" />{{ t('common.loading') }}</div>
      <template v-else>
        <section :id="`shared-pool-panel-${tab}`" role="tabpanel" :aria-labelledby="`shared-pool-tab-${tab}`" class="min-w-0 space-y-4">
          <template v-if="tab === 'accounts'">
            <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
              <div><h2 class="font-semibold text-gray-900 dark:text-white">{{ t('sharedPool.accounts') }} <span class="ml-2 rounded-md bg-gray-100 px-2 py-0.5 text-xs font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300">{{ total }}</span></h2><p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('sharedPool.adminAccountsHint') }}</p></div>
              <form class="flex w-full gap-2 sm:max-w-sm" @submit.prevent="searchAccounts">
                <div class="relative min-w-0 flex-1"><Icon name="search" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" /><input v-model="search" class="input w-full pl-10" :placeholder="t('sharedPool.searchAccounts')" :aria-label="t('common.search')" /></div>
                <button class="btn btn-secondary shrink-0" :disabled="accountsLoading">{{ t('common.search') }}</button>
              </form>
            </div>
            <SharedPoolAdminAccountsTable :accounts="accounts" :loading="accountsLoading" :searching="!!appliedSearch" @allocate="allocating = $event" />
            <Pagination v-if="total > 20" :page="page" :page-size="20" :total="total" :show-page-size-selector="false" @update:page="changePage" />
          </template>
          <SharedPoolSettingsForm v-else-if="tab === 'settings' && settings" :settings="settings" :groups="groups" @saved="settingsSaved" />
          <SharedPoolUserRates v-else-if="tab === 'userRates'" @saved="refreshAccountRates" />
          <template v-else-if="tab === 'earnings'">
            <div><h2 class="font-semibold text-gray-900 dark:text-white">{{ t('sharedPool.earnings') }}</h2><p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('sharedPool.revenueSplitHint') }}</p></div>
            <SharedEarningsTable admin />
          </template>
        </section>
      </template>
    </div>
    <SharedPoolAllocationDialog v-if="allocating" :account="allocating" :groups="groups" @close="allocating = null" @saved="allocated" />
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
import SharedPoolAdminAccountsTable from '@/components/sharedPool/SharedPoolAdminAccountsTable.vue'
import SharedPoolSettingsForm from '@/components/sharedPool/SharedPoolSettingsForm.vue'
import SharedPoolUserRates from '@/components/sharedPool/SharedPoolUserRates.vue'
import SharedPoolAllocationDialog from '@/components/sharedPool/SharedPoolAllocationDialog.vue'
import SharedEarningsTable from '@/components/sharedPool/SharedEarningsTable.vue'
import { getAllIncludingInactive } from '@/api/admin/groups'
import { adminSharedPoolAPI, type SharedAccount, type SharedSettings } from '@/api/sharedPool'
import type { AdminGroup } from '@/types'
import { useAppStore } from '@/stores/app'
const { t } = useI18n()
const app = useAppStore()
const tabs = ['accounts', 'settings', 'userRates', 'earnings'] as const
const tabIcons = { accounts: 'users', settings: 'cog', userRates: 'userCircle', earnings: 'chart' } as const
const tab = ref<typeof tabs[number]>('accounts')
const loading = ref(false)
const accountsLoading = ref(false)
const error = ref('')
const accounts = ref<SharedAccount[]>([])
const groups = ref<AdminGroup[]>([])
const settings = ref<SharedSettings>()
const allocating = ref<SharedAccount | null>(null)
const search = ref('')
const appliedSearch = ref('')
const page = ref(1)
const total = ref(0)
async function loadAccounts() {
  const result = await adminSharedPoolAPI.accounts(page.value, appliedSearch.value)
  accounts.value = result.items || []; total.value = result.total
}
async function load() {
  loading.value = true; error.value = ''
  try {
    const [settingsData, groupData] = await Promise.all([adminSharedPoolAPI.settings(), getAllIncludingInactive(), loadAccounts()])
    settings.value = settingsData; groups.value = groupData
  } catch (e: unknown) { error.value = (e as Error).message || t('sharedPool.loadFailed') }
  finally { loading.value = false }
}
async function changePage(value: number) {
  if (accountsLoading.value) return
  page.value = value
  accountsLoading.value = true; error.value = ''
  try { await loadAccounts() } catch (e: unknown) { error.value = (e as Error).message || t('sharedPool.loadFailed') }
  finally { accountsLoading.value = false }
}
async function refreshAccountRates() {
  error.value = ''
  try { await loadAccounts() } catch (e: unknown) { error.value = (e as Error).message || t('sharedPool.loadFailed') }
}
function settingsSaved(value: SharedSettings) { settings.value = value; void refreshAccountRates() }
function searchAccounts() {
  if (accountsLoading.value) return
  appliedSearch.value = search.value.trim()
  void changePage(1)
}
function allocated() { allocating.value = null; app.showSuccess(t('sharedPool.saved')); void changePage(page.value) }
onMounted(load)
</script>
