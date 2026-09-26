<template>
  <fieldset :disabled="disabled" class="min-w-0 space-y-3" data-testid="excel-bps-options">
    <label class="flex items-center gap-2 text-sm">
      <input :checked="allModels" type="checkbox" :data-testid="`${testIdPrefix}-all-models`" @change="setAllModels(($event.target as HTMLInputElement).checked)" />
      <span>{{ t('admin.accounts.openai.excelBPSAllModels') }}</span>
    </label>
    <div v-if="!allModels" :data-testid="`${testIdPrefix}-model-selection`">
      <label class="input-label">{{ t('admin.accounts.openai.excelBPSModels') }}</label>
      <ModelWhitelistSelector :model-value="options.models ?? []" platform="openai" @update:model-value="update({ models: $event })" />
      <button type="button" class="btn btn-secondary" :data-testid="`${testIdPrefix}-astra-only`"
        @click="update({ models: [...EXCEL_BPS_DEFAULT_MODELS] })">{{ t('admin.accounts.openai.excelBPSAstraOnly') }}</button>
      <p class="input-hint">{{ t('admin.accounts.openai.excelBPSModelsHint') }}</p>
    </div>
    <p class="text-xs text-amber-600 dark:text-amber-400">{{ t('admin.accounts.openai.excelBPSNotice') }}</p>
    <div>
      <label class="flex items-center gap-2">
        <input :checked="options.auto_disable_on_403" type="checkbox" :data-testid="`${testIdPrefix}-auto-disable-on-403`"
          class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500"
          @change="update({ auto_disable_on_403: ($event.target as HTMLInputElement).checked })" />
        <span class="text-sm">{{ t('admin.accounts.openai.excelBPSAutoDisableOn403') }}</span>
      </label>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.excelBPSAutoDisableOn403Desc') }}</p>
    </div>
    <div>
      <label class="flex items-center gap-2">
        <input :checked="options.cache_creation_as_input" type="checkbox" :data-testid="`${testIdPrefix}-cache-creation-as-input`"
          class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500"
          @change="update({ cache_creation_as_input: ($event.target as HTMLInputElement).checked })" />
        <span class="text-sm">{{ t('admin.accounts.openai.excelBPSCacheCreationAsInput') }}</span>
      </label>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.excelBPSCacheCreationAsInputDesc') }}</p>
    </div>
  </fieldset>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import ModelWhitelistSelector from '@/components/account/ModelWhitelistSelector.vue'
import { EXCEL_BPS_DEFAULT_MODELS, type ExcelBPSOptions } from '@/utils/excelBPSOptions'

withDefaults(defineProps<{ disabled?: boolean; testIdPrefix?: string }>(), { testIdPrefix: 'excel-bps' })
const options = defineModel<ExcelBPSOptions>({ required: true })
const { t } = useI18n()
const allModels = computed(() => options.value.models === null)
function update(patch: Partial<ExcelBPSOptions>) { options.value = { ...options.value, ...patch } }
// 取消“全部模型”时与账号编辑一致，默认选中 Astra。
function setAllModels(checked: boolean) { update({ models: checked ? null : [...EXCEL_BPS_DEFAULT_MODELS] }) }
</script>
