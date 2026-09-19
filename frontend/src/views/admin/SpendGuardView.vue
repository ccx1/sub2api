<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('admin.spendGuard.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.spendGuard.description') }}</p>
        </div>
        <div class="flex flex-wrap gap-2">
          <button type="button" class="btn btn-secondary inline-flex items-center gap-2" :disabled="loading" @click="loadAll">
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
            {{ t('common.refresh') }}
          </button>
          <button type="button" class="btn btn-secondary inline-flex items-center gap-2" @click="showSettings = true">
            <Icon name="cog" size="sm" />
            {{ t('admin.spendGuard.settings') }}
          </button>
        </div>
      </div>

      <div v-if="loading" class="flex justify-center py-16">
        <div class="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-600"></div>
      </div>
      <div v-else-if="items.length === 0" class="card border-dashed px-6 py-12 text-center">
        <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.spendGuard.noData') }}</p>
      </div>
      <div v-else class="table-container overflow-x-auto">
        <table class="min-w-full text-sm">
          <thead>
            <tr class="border-b border-gray-100 text-left dark:border-dark-700">
              <th class="px-4 py-3">{{ t('admin.spendGuard.key') }}</th>
              <th class="px-4 py-3 text-right">{{ t('admin.spendGuard.requests') }}</th>
              <th class="px-4 py-3 text-right">{{ t('admin.spendGuard.tokensPerMin') }}</th>
              <th class="px-4 py-3 text-right">{{ t('admin.spendGuard.errRate') }}</th>
              <th class="px-4 py-3 text-center">{{ t('admin.spendGuard.status') }}</th>
              <th class="px-4 py-3 text-right">{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in items" :key="item.api_key_id" class="border-b border-gray-100 dark:border-dark-700">
              <td class="px-4 py-3 text-gray-900 dark:text-white">
                <span class="font-medium">{{ item.name || `#${item.api_key_id}` }}</span>
                <span class="ml-2 text-xs text-gray-400">#{{ item.api_key_id }}</span>
              </td>
              <td class="px-4 py-3 text-right tabular-nums">{{ item.requests }}</td>
              <td class="px-4 py-3 text-right tabular-nums">{{ compactNumber(item.tokens_per_min) }}</td>
              <td class="px-4 py-3 text-right tabular-nums">{{ percent(item.err_rate) }}</td>
              <td class="px-4 py-3 text-center">
                <span class="status-pill" :class="item.frozen ? 'status-pill-error' : 'status-pill-success'">
                  {{ item.frozen ? t('admin.spendGuard.frozen') : statusLabel(item.status) }}
                </span>
              </td>
              <td class="px-4 py-3 text-right">
                <button v-if="item.frozen" type="button" class="btn btn-sm btn-primary" @click="unfreeze(item)">
                  {{ t('admin.spendGuard.unfreeze') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="events.length" class="card">
        <div class="border-b border-gray-100 px-4 py-3 text-sm font-semibold dark:border-dark-700">{{ t('admin.spendGuard.events') }}</div>
        <div class="divide-y divide-gray-100 dark:divide-dark-700">
          <div v-for="(event, index) in events" :key="`${event.api_key_id}-${event.at}-${index}`" class="flex flex-wrap justify-between gap-2 px-4 py-3 text-sm">
            <span class="text-gray-700 dark:text-gray-300">
              <strong>{{ eventAction(event.action) }}</strong>
              {{ event.name || `#${event.api_key_id}` }}
            </span>
            <span class="text-xs text-gray-500">{{ eventReason(event.reason) }} · {{ formatDateTime(event.at) }}</span>
          </div>
        </div>
      </div>
    </div>

    <BaseDialog :show="showSettings" :title="t('admin.spendGuard.settings')" @close="showSettings = false">
      <div class="space-y-4">
        <div class="flex items-center gap-3">
          <Toggle v-model="form.enabled" />
          <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.spendGuard.enableAuto') }}</span>
        </div>
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <label class="space-y-1">
            <span class="input-label">{{ t('admin.spendGuard.windowMinutes') }}</span>
            <input v-model.number="form.window_minutes" class="input" type="number" min="1" />
          </label>
          <label class="space-y-1">
            <span class="input-label">{{ t('admin.spendGuard.tokensPerMinute') }}</span>
            <input v-model.number="form.tokens_per_minute" class="input" type="number" min="0" step="any" />
          </label>
          <label class="space-y-1">
            <span class="input-label">{{ t('admin.spendGuard.minRequests') }}</span>
            <input v-model.number="form.min_requests" class="input" type="number" min="1" />
          </label>
          <label class="space-y-1">
            <span class="input-label">{{ t('admin.spendGuard.maxErrorRate') }}</span>
            <input v-model.number="form.max_error_rate" class="input" type="number" min="0" max="1" step="0.05" />
          </label>
          <label class="space-y-1">
            <span class="input-label">{{ t('admin.spendGuard.intervalSeconds') }}</span>
            <input v-model.number="form.interval_seconds" class="input" type="number" min="10" step="10" />
          </label>
        </div>
        <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.spendGuard.settingsHint') }}</p>
      </div>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="showSettings = false">{{ t('common.cancel') }}</button>
          <button type="button" class="btn btn-primary" :disabled="saving" @click="saveSettings">
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import type { SpendGuardEvent, SpendGuardOffender, SpendGuardSettings } from '@/types'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'

const { t } = useI18n()
const appStore = useAppStore()
const items = ref<SpendGuardOffender[]>([])
const events = ref<SpendGuardEvent[]>([])
const loading = ref(false)
const saving = ref(false)
const showSettings = ref(false)
const form = reactive<SpendGuardSettings>({
  enabled: false,
  window_minutes: 5,
  tokens_per_minute: 2_000_000,
  min_requests: 5,
  max_error_rate: 0.8,
  interval_seconds: 60
})

const compactNumber = (value: number) => {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`
  return Math.round(value).toString()
}
const percent = (value: number) => `${(value * 100).toFixed(1)}%`
const statusLabel = (status: string) => {
  const key = `admin.spendGuard.status${status === 'active' ? 'Active' : status === 'disabled' ? 'Disabled' : status === 'quota_exhausted' ? 'QuotaExhausted' : status === 'expired' ? 'Expired' : ''}`
  return key.endsWith('status') ? status : t(key)
}
const eventAction = (action: string) => action === 'frozen' ? t('admin.spendGuard.frozen') : t('admin.spendGuard.unfreezeSuccess')
const eventReason = (reason: string) => reason === 'token velocity'
  ? t('admin.spendGuard.reasonTokenRate')
  : reason === 'error rate'
    ? t('admin.spendGuard.reasonErrorRate')
    : t('admin.spendGuard.reasonManual')

async function loadAll() {
  loading.value = true
  try {
    const [offenders, settings, eventResponse] = await Promise.all([
      adminAPI.spendGuard.offenders(),
      adminAPI.spendGuard.getSettings(),
      adminAPI.spendGuard.events()
    ])
    items.value = offenders.items || []
    events.value = eventResponse.items || []
    Object.assign(form, settings)
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.spendGuard.loadFailed')))
  } finally {
    loading.value = false
  }
}

async function saveSettings() {
  saving.value = true
  try {
    Object.assign(form, await adminAPI.spendGuard.updateSettings({ ...form }))
    showSettings.value = false
    appStore.showSuccess(t('admin.spendGuard.saveSuccess'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.spendGuard.saveFailed')))
  } finally {
    saving.value = false
  }
}

async function unfreeze(item: SpendGuardOffender) {
  try {
    await adminAPI.spendGuard.unfreeze(item.api_key_id)
    appStore.showSuccess(t('admin.spendGuard.unfreezeSuccess'))
    await loadAll()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.spendGuard.unfreezeFailed')))
  }
}

onMounted(loadAll)
</script>
