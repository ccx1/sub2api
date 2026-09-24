<template>
  <div class="flex min-w-0 flex-wrap items-center gap-2">
    <button type="button" class="btn btn-secondary" data-test="auto-transfer-settings" aria-haspopup="dialog" :aria-expanded="dialogOpen" @click="openDialog">
      <Icon name="cog" size="sm" class="mr-2" />{{ t('sharedPool.autoTransferTitle') }}
    </button>
    <span v-if="saved" data-test="saved-status" role="status" class="text-xs" :class="saved.enabled ? 'text-emerald-600 dark:text-emerald-400' : 'text-gray-500 dark:text-dark-400'">{{ t(saved.enabled ? 'sharedPool.autoTransferEnabled' : 'sharedPool.autoTransferDisabled') }}</span>
    <span v-else role="status" class="text-xs text-gray-500 dark:text-dark-400">{{ t(loading ? 'common.loading' : 'sharedPool.autoTransferStatusUnavailable') }}</span>
    <BaseDialog :show="dialogOpen" :title="t('sharedPool.autoTransferTitle')" width="normal" :close-on-escape="!saving" :show-close-button="!saving" @close="closeDialog">
      <p class="text-sm leading-relaxed text-gray-500 dark:text-dark-400">{{ t('sharedPool.autoTransferHint') }}</p>
      <p v-if="loading" role="status" class="mt-4 text-sm text-gray-500">{{ t('common.loading') }}</p>
      <div v-else-if="loadError" class="mt-4 space-y-3">
        <p role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t('sharedPool.autoTransferLoadFailed') }}</p>
        <button type="button" class="btn btn-secondary btn-sm" @click="load">{{ t('sharedPool.autoTransferRetry') }}</button>
      </div>
      <form v-else-if="saved" id="shared-auto-transfer-form" class="mt-5 space-y-4" :aria-busy="saving" novalidate @submit.prevent="save">
        <div class="flex min-h-10 items-center gap-3">
          <Toggle id="shared-auto-transfer-enabled" v-model="enabled" :disabled="saving" :aria-label="t('sharedPool.autoTransferToggle')" aria-describedby="shared-auto-transfer-status" class="disabled:cursor-not-allowed disabled:opacity-50" />
          <label for="shared-auto-transfer-enabled" class="text-sm">{{ t('sharedPool.autoTransferToggle') }}</label>
        </div>
        <div>
          <label for="shared-auto-transfer-threshold" class="input-label">{{ t('sharedPool.autoTransferThreshold') }}</label>
          <input id="shared-auto-transfer-threshold" v-model.trim="threshold" type="text" inputmode="decimal" required class="input w-full" :disabled="saving" :aria-invalid="!!thresholdError" :aria-describedby="thresholdError ? 'shared-auto-transfer-threshold-error' : undefined" />
          <p v-if="thresholdError" id="shared-auto-transfer-threshold-error" role="alert" class="mt-2 text-xs text-red-600 dark:text-red-400">{{ thresholdError }}</p>
        </div>
        <div>
          <label for="shared-auto-transfer-time" class="input-label">{{ t('sharedPool.autoTransferTime') }}</label>
          <input id="shared-auto-transfer-time" v-model="dailyTime" type="time" step="60" required class="input w-full min-w-0" :disabled="saving" :aria-invalid="!!timeError" :aria-describedby="timeError ? 'shared-auto-transfer-time-error shared-auto-transfer-timezone' : 'shared-auto-transfer-timezone'" />
          <p v-if="timeError" id="shared-auto-transfer-time-error" role="alert" class="mt-2 text-xs text-red-600 dark:text-red-400">{{ timeError }}</p>
          <p id="shared-auto-transfer-timezone" class="mt-2 break-words text-xs text-gray-500 dark:text-dark-400">{{ t('sharedPool.autoTransferTimezone', { timezone: saved.timezone }) }}</p>
        </div>
        <p v-if="saveError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t('sharedPool.autoTransferSaveFailed') }}</p>
        <p id="shared-auto-transfer-status" role="status" class="text-xs text-gray-500 dark:text-dark-400">{{ t(dirty ? 'sharedPool.autoTransferUnsaved' : 'sharedPool.autoTransferSaveHint') }}</p>
      </form>
      <template #footer>
        <button type="button" class="btn btn-secondary" :disabled="saving" @click="closeDialog">{{ t('common.cancel') }}</button>
        <button v-if="saved && !loading && !loadError" type="submit" form="shared-auto-transfer-form" class="btn btn-primary" :disabled="saving || !dirty || !!thresholdError || !!timeError">{{ t(saving ? 'common.saving' : 'common.save') }}</button>
      </template>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import { sharedPoolAPI, type SharedAutoTransferSettings } from '@/api/sharedPool'

const { t } = useI18n()
const dialogOpen = ref(false)
const saved = ref<SharedAutoTransferSettings | null>(null)
const enabled = ref(false)
const threshold = ref('1')
const dailyTime = ref('00:00')
const loading = ref(false)
const saving = ref(false)
const loadError = ref(false)
const saveError = ref(false)
const formatThreshold = (value: number) => value.toFixed(8).replace(/\.?0+$/, '')
const dirty = computed(() => !!saved.value && (enabled.value !== saved.value.enabled || threshold.value !== formatThreshold(saved.value.threshold) || dailyTime.value !== saved.value.daily_time))
const thresholdError = computed(() => {
  const match = /^(\d+)(?:\.(\d{1,8}))?$/.exec(threshold.value)
  if (!match) return t('sharedPool.autoTransferInvalidThreshold')
  const integer = Number(match[1])
  // 最大整数附近的浮点转换会丢失小数，先检查原始小数位。
  const exceedsMaximum = integer > 1_000_000_000 || (integer === 1_000_000_000 && /[1-9]/.test(match[2] || ''))
  return exceedsMaximum || Number(threshold.value) < 0.00000001 ? t('sharedPool.autoTransferInvalidThreshold') : ''
})
const timeError = computed(() => /^([01]\d|2[0-3]):[0-5]\d$/.test(dailyTime.value) ? '' : t('sharedPool.autoTransferInvalidTime'))

function apply(settings: SharedAutoTransferSettings) {
  saved.value = settings
  enabled.value = settings.enabled
  threshold.value = formatThreshold(settings.threshold)
  dailyTime.value = settings.daily_time
}
function openDialog() {
  if (saving.value) return
  if (saved.value) apply(saved.value)
  saveError.value = false
  dialogOpen.value = true
}
function closeDialog() {
  if (saving.value) return
  dialogOpen.value = false
}
async function load() {
  if (loading.value) return
  loading.value = true
  loadError.value = false
  try { apply(await sharedPoolAPI.autoTransferSettings()) }
  catch { loadError.value = true }
  finally { loading.value = false }
}
async function save() {
  if (saving.value || !saved.value || !dirty.value || thresholdError.value || timeError.value) return
  saving.value = true
  saveError.value = false
  try {
    apply(await sharedPoolAPI.saveAutoTransferSettings({ enabled: enabled.value, threshold: Number(threshold.value), daily_time: dailyTime.value }))
    dialogOpen.value = false
  } catch { saveError.value = true }
  finally { saving.value = false }
}
onMounted(() => { void load() })
</script>
