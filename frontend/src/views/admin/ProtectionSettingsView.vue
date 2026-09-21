<template>
  <AppLayout>
    <div class="space-y-6">
      <header>
        <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('accountProtection.title') }}</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('accountProtection.description') }}</p>
      </header>
      <p v-if="loading" role="status" class="py-12 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('accountProtection.loading') }}</p>
      <div v-else-if="loadError" role="alert" class="card flex flex-wrap items-center justify-between gap-3 p-5">
        <p class="text-sm text-red-600 dark:text-red-400">{{ loadError }}</p>
        <button type="button" class="btn btn-secondary" data-testid="load-retry" @click="load">{{ t('accountProtection.retry') }}</button>
      </div>
      <template v-else>
        <section class="card space-y-4 p-5" aria-labelledby="protection-default-title">
          <div>
            <h2 id="protection-default-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('accountProtection.defaultTitle') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('accountProtection.defaultHint') }}</p>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('accountProtection.otherAccountsHint') }}</p>
          </div>
          <form class="flex flex-col gap-3 sm:flex-row sm:items-end" @submit.prevent="saveDefault">
            <label class="min-w-0 flex-1 space-y-1 sm:max-w-md">
              <span class="input-label">{{ t('accountProtection.defaultLabel') }}</span>
              <select v-model="selectedDefault" data-testid="default-mode" class="input" :disabled="saving || applyingAccount || !applicableStrategies.length">
                <option v-for="strategy in applicableStrategies" :key="strategy.id" :value="strategy.id">{{ strategy.name }}</option>
              </select>
            </label>
            <button type="submit" data-testid="save-default" class="btn btn-primary" :disabled="!canSave">
              {{ t(saving ? 'accountProtection.saving' : 'accountProtection.saveDefault') }}
            </button>
          </form>
          <p class="text-sm text-gray-600 dark:text-gray-300" data-testid="saved-default">{{ t('accountProtection.savedDefault', { name: savedStrategyName }) }}</p>
          <p v-if="saveError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ saveError }}</p>
          <p v-if="saveError && !syncResult" class="text-sm text-gray-500 dark:text-gray-400">{{ t('accountProtection.syncRetryHint') }}</p>
          <div v-if="syncResult" data-testid="sync-result" class="space-y-2 border-t border-gray-100 pt-4 text-sm dark:border-dark-700" aria-live="polite">
            <h3 class="font-semibold text-gray-900 dark:text-white">{{ t('accountProtection.syncTitle') }}</h3>
            <p class="text-gray-700 dark:text-gray-300">{{ t('accountProtection.syncSummary', syncResult) }}</p>
            <p v-if="syncResult.error" class="text-red-600 dark:text-red-400">{{ t('accountProtection.syncError', { error: syncResult.error }) }}</p>
            <template v-if="syncResult.failed.length">
              <p class="text-amber-700 dark:text-amber-300">{{ t('accountProtection.syncFailed', { count: syncResult.failed.length }) }}</p>
              <ul class="max-h-64 space-y-1 overflow-auto">
                <li v-for="failure in syncResult.failed" :key="failure.account_id" class="break-words text-red-600 dark:text-red-400">#{{ failure.account_id }}: {{ failure.error }}</li>
              </ul>
            </template>
            <p v-if="syncResult.error || syncResult.failed.length" class="text-gray-500 dark:text-gray-400">{{ t('accountProtection.syncRetryHint') }}</p>
          </div>
        </section>
        <ProtectionStrategyApply :strategies="strategies" :default-mode="savedDefault" :disabled="saving" :refresh-token="syncRevision" @busy="applyingAccount = $event" />
        <ProtectionStrategyCatalog :strategies="strategies" :default-mode="savedDefault" />
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import ProtectionStrategyApply from '@/components/account/ProtectionStrategyApply.vue'
import ProtectionStrategyCatalog from '@/components/account/ProtectionStrategyCatalog.vue'
import { getProtectionSettings, listProtectionStrategies, saveProtectionSettings, type ProtectionStrategy, type ProtectionSyncResult } from '@/api/admin/accountProtection'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(true)
const saving = ref(false)
const loadError = ref('')
const saveError = ref('')
const applyingAccount = ref(false)
const syncResult = ref<ProtectionSyncResult | null>(null)
const syncRevision = ref(0)
const strategies = ref<ProtectionStrategy[]>([])
const savedDefault = ref('mode2')
const selectedDefault = ref('mode2')
const applicableStrategies = computed(() => strategies.value.filter(strategy => strategy.apply_supported))
const savedStrategyName = computed(() => strategies.value.find(strategy => strategy.id === savedDefault.value)?.name || savedDefault.value)
const canSave = computed(() => !saving.value && !applyingAccount.value
  && applicableStrategies.value.some(strategy => strategy.id === selectedDefault.value))

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const [settings, catalog] = await Promise.all([getProtectionSettings(), listProtectionStrategies()])
    if (!catalog.some(strategy => strategy.apply_supported)) throw new Error(t('accountProtection.noStrategies'))
    strategies.value = catalog
    savedDefault.value = settings.default_mode
    selectedDefault.value = settings.default_mode
  } catch (error) {
    loadError.value = extractApiErrorMessage(error, t('accountProtection.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function saveDefault() {
  if (!canSave.value) return
  saving.value = true
  saveError.value = ''
  syncResult.value = null
  try {
    const settings = await saveProtectionSettings({ default_mode: selectedDefault.value })
    savedDefault.value = settings.default_mode
    selectedDefault.value = settings.default_mode
    syncResult.value = settings.sync
    syncRevision.value++
    if (settings.sync.error || settings.sync.failed.length) {
      saveError.value = t('accountProtection.syncIncomplete')
    } else {
      appStore.showSuccess(t('accountProtection.saveSuccess'))
    }
  } catch (error) {
    saveError.value = extractApiErrorMessage(error, t('accountProtection.saveFailed'))
    syncRevision.value++
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>
