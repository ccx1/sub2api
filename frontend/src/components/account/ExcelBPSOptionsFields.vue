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
    <div v-for="option in contentOptions" :key="option.field">
      <label class="flex items-center gap-2">
        <input :checked="options[option.field]" type="checkbox" :data-testid="`${testIdPrefix}-${option.testId}`"
          class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500"
          @change="update({ [option.field]: ($event.target as HTMLInputElement).checked })" />
        <span class="text-sm">{{ t(`admin.accounts.openai.${option.label}`) }}</span>
      </label>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t(`admin.accounts.openai.${option.label}Desc`) }}</p>
    </div>
    <div v-if="allowGroupMove">
      <label class="flex items-center gap-2">
        <input :checked="options.auto_move_on_403" type="checkbox" :data-testid="`${testIdPrefix}-auto-move-on-403`"
          class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500"
          @change="update({ auto_move_on_403: ($event.target as HTMLInputElement).checked, target_group_id: null })" />
        <span class="text-sm">{{ t('admin.accounts.openai.excelBPSAutoMoveOn403') }}</span>
      </label>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.excelBPSAutoMoveOn403Desc') }}</p>
      <label v-if="options.auto_move_on_403" class="mt-2 block">
        <span class="input-label">{{ t('admin.accounts.openai.excelBPS403TargetGroup') }}</span>
        <select :value="options.target_group_id ?? ''" class="input w-full" :data-testid="`${testIdPrefix}-target-group`"
          @change="setTarget(($event.target as HTMLSelectElement).value)">
          <option value="">{{ t('admin.accounts.openai.excelBPS403SelectTarget') }}</option>
          <option :value="0">{{ t('admin.accounts.openai.excelBPS403LeaveAllGroups') }}</option>
          <option v-for="group in eligibleGroups" :key="group.id" :value="group.id">{{ group.name }}</option>
        </select>
      </label>
    </div>
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

const props = withDefaults(defineProps<{
  disabled?: boolean; testIdPrefix?: string; allowGroupMove?: boolean
  groups?: { id: number; name: string; platform: string; status?: string }[]
}>(), { testIdPrefix: 'excel-bps', allowGroupMove: false, groups: () => [] })
const options = defineModel<ExcelBPSOptions>({ required: true })
const { t } = useI18n()
const allModels = computed(() => options.value.models === null)
const eligibleGroups = computed(() => props.groups.filter(group => group.platform === 'openai'))
const contentOptions = [
  { field: 'ignore_images', testId: 'ignore-images', label: 'excelBPSIgnoreImages' },
  { field: 'ignore_encrypted_content', testId: 'ignore-encrypted-content', label: 'excelBPSIgnoreEncryptedContent' },
  { field: 'omit_unsupported_tools', testId: 'omit-unsupported-tools', label: 'excelBPSOmitUnsupportedTools' }
] as const
function update(patch: Partial<ExcelBPSOptions>) { options.value = { ...options.value, ...patch } }
function setTarget(value: string) { update({ target_group_id: value === '' ? null : Number(value) }) }
// 取消“全部模型”时与账号编辑一致，默认选中 Astra。
function setAllModels(checked: boolean) { update({ models: checked ? null : [...EXCEL_BPS_DEFAULT_MODELS] }) }
</script>
