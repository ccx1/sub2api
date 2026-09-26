<template>
  <BaseDialog :show="show" :title="t('admin.accountImportSettings.title')" width="wide" :z-index="60" @close="close">
    <div v-if="loading" role="status" class="py-8 text-center text-sm text-gray-500">{{ t('common.loading') }}</div>
    <div v-else-if="loadError" role="alert" class="space-y-3 py-4">
      <p class="text-sm text-red-600 dark:text-red-400">{{ t(loadError) }}</p>
      <button type="button" class="btn btn-secondary" data-testid="import-settings-retry" @click="load">{{ t('common.tryAgain') }}</button>
    </div>
    <form v-else-if="loaded" id="account-import-settings-form" class="space-y-5" @submit.prevent="save">
      <p class="text-sm text-gray-600 dark:text-dark-300">{{ t('admin.accountImportSettings.description') }}</p>
      <div class="flex items-center justify-between gap-4 rounded-lg border border-gray-200 p-3 dark:border-dark-700">
        <span id="account-import-settings-enabled" class="text-sm font-medium">{{ t('admin.accountImportSettings.enabled') }}</span>
        <Toggle v-model="form.enabled" :disabled="saving" aria-labelledby="account-import-settings-enabled" data-testid="import-settings-enabled" />
      </div>
      <p v-if="!form.enabled" class="input-hint">{{ t('admin.accountImportSettings.disabledHint') }}</p>
      <AccountImportSettingsForm v-model="form" v-model:validation-error="validationError" :proxies="proxies" :disabled="saving || !form.enabled" />
      <p v-if="validationError && form.enabled" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t(validationError) }}</p>
      <p v-if="saveError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ saveError }}</p>
    </form>
    <template #footer>
      <div class="flex flex-wrap justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="saving" @click="close">{{ t('common.cancel') }}</button>
        <button type="submit" form="account-import-settings-form" class="btn btn-primary" data-testid="import-settings-save" :disabled="!loaded || loading || saving || !!loadError || (form.enabled && !!validationError)">
          {{ saving ? t('common.saving') : t('common.save') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Toggle from '@/components/common/Toggle.vue'
import AccountImportSettingsForm from './AccountImportSettingsForm.vue'
import { adminAPI } from '@/api/admin'
import { defaultAccountImportSettings, getAccountImportSettings, saveAccountImportSettings } from '@/api/admin/accountImportSettings'
import { useAppStore } from '@/stores/app'
import { defaultExcelBPSOptions, normalizeExcelBPSOptions } from '@/utils/excelBPSOptions'
import { normalizeRandomProxyEmptyPoolPolicy, normalizeRandomProxyGroupId, normalizeRandomProxyPoolIds, normalizeRandomProxyPoolScope, normalizeRandomProxyRegionFallback, normalizeRandomProxyReuseMinutes, randomProxyExtra } from '@/utils/randomProxy'
import type { Proxy } from '@/types'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const appStore = useAppStore()
const form = ref(defaultAccountImportSettings())
const proxies = ref<Proxy[]>([])
const loading = ref(false)
const loaded = ref(false)
const saving = ref(false)
const loadError = ref('')
const saveError = ref('')
const validationError = ref<string | null>(null)
let loadVersion = 0

async function load() {
  if (saving.value) return
  const version = ++loadVersion
  loading.value = true
  loaded.value = false
  loadError.value = ''
  saveError.value = ''
  validationError.value = null
  const [settings, directory] = await Promise.allSettled([getAccountImportSettings(), adminAPI.proxies.getAll()])
  if (version !== loadVersion || !props.show) return
  loading.value = false
  if (settings.status === 'rejected' || directory.status === 'rejected') {
    loadError.value = settings.status === 'rejected' ? 'admin.accountImportSettings.loadFailed' : 'admin.accountImportSettings.proxiesLoadFailed'
    return
  }
  form.value = { ...settings.value, excel_bps_options: normalizeExcelBPSOptions(settings.value.excel_bps_options), extra: { ...settings.value.extra } }
  proxies.value = directory.value
  loaded.value = true
}

async function save() {
  if (!loaded.value || loading.value || saving.value || loadError.value || (form.value.enabled && validationError.value)) return
  saving.value = true
  saveError.value = ''
  try {
    // 协议关闭时子选项回到默认值，避免保存无效的旧模型范围。
    const payload = { ...form.value, excel_bps_options: form.value.excel_bps_enabled ? normalizeExcelBPSOptions(form.value.excel_bps_options) : defaultExcelBPSOptions(), extra: { ...form.value.extra } }
    if (payload.proxy_mode !== 'fixed') payload.proxy_id = null
    if (payload.extra.proxy_region_mode !== 'billing') delete payload.extra.proxy_region_fallback_country
    if (payload.proxy_mode !== 'random') {
      for (const key of Object.keys(payload.extra)) if (key.startsWith('random_proxy_')) delete payload.extra[key]
    } else {
      const random: Record<string, unknown> = randomProxyExtra(normalizeRandomProxyPoolScope(payload.extra.random_proxy_pool_scope), normalizeRandomProxyPoolIds(payload.extra.random_proxy_pool_ids), {
        policy: normalizeRandomProxyEmptyPoolPolicy(payload.extra.random_proxy_empty_pool_policy),
        groupId: normalizeRandomProxyGroupId(payload.extra.random_proxy_group_id),
        regionFallback: normalizeRandomProxyRegionFallback(payload.extra.random_proxy_region_fallback),
        maxReuseMinutes: normalizeRandomProxyReuseMinutes(payload.extra.random_proxy_max_reuse_minutes)
      })
      delete random.proxy_mode
      Object.assign(payload.extra, random)
    }
    await saveAccountImportSettings(payload)
    appStore.showSuccess(t('admin.accountImportSettings.saved'))
    emit('saved')
    emit('close')
  } catch (error: unknown) {
    const failure = error as { response?: { data?: { detail?: string; message?: string } }; message?: string }
    saveError.value = failure.response?.data?.message || failure.response?.data?.detail || failure.message || t('admin.accountImportSettings.saveFailed')
  } finally {
    saving.value = false
  }
}

function close() {
  if (!saving.value) emit('close')
}

watch(() => props.show, show => {
  if (show) void load()
  else { loadVersion++; loading.value = false }
}, { immediate: true })
</script>
