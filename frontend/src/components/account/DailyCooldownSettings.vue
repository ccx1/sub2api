<template>
  <fieldset :disabled="disabled" class="min-w-0 space-y-3" data-testid="daily-cooldown-settings">
    <div class="flex items-start justify-between gap-4">
      <div class="min-w-0">
        <p class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.accounts.dailyCooldown.title') }}</p>
        <p class="input-hint mt-1">{{ t('admin.accounts.dailyCooldown.hint') }}</p>
      </div>
      <Toggle :model-value="modelValue.enabled" :disabled="disabled" :aria-label="t('admin.accounts.dailyCooldown.title')" class="mt-0.5" data-testid="daily-cooldown-enabled" @update:model-value="update({ enabled: $event })" />
    </div>
    <template v-if="modelValue.enabled">
      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <DailyCooldownTimeSelect :model-value="modelValue.start" :label="t('admin.accounts.dailyCooldown.start')" :disabled="disabled" data-testid="daily-cooldown-start" @update:model-value="update({ start: $event })" />
        <DailyCooldownTimeSelect :model-value="modelValue.end" :label="t('admin.accounts.dailyCooldown.end')" :disabled="disabled" data-testid="daily-cooldown-end" @update:model-value="update({ end: $event })" />
      </div>
      <label class="block">
        <span class="input-label">{{ t('admin.accounts.dailyCooldown.timezone') }}</span>
        <input :value="modelValue.timezone" type="text" required class="input" placeholder="Asia/Shanghai" data-testid="daily-cooldown-timezone" @input="update({ timezone: ($event.target as HTMLInputElement).value })" />
      </label>
      <p class="input-hint">{{ t('admin.accounts.dailyCooldown.timezoneHint') }}</p>
      <p v-if="modelValue.start > modelValue.end" class="input-hint">{{ t('admin.accounts.dailyCooldown.overnightHint') }}</p>
      <p v-if="error" role="alert" class="text-xs text-red-600 dark:text-red-400">{{ t(error) }}</p>
    </template>
  </fieldset>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import DailyCooldownTimeSelect from './DailyCooldownTimeSelect.vue'
import { dailyCooldownValidationError, type DailyCooldown } from '@/utils/dailyCooldown'

const props = defineProps<{ modelValue: DailyCooldown; disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: DailyCooldown] }>()
const { t } = useI18n()
const error = computed(() => dailyCooldownValidationError(props.modelValue))
function update(patch: Partial<DailyCooldown>) {
  if (!props.disabled) emit('update:modelValue', { ...props.modelValue, ...patch })
}
</script>
