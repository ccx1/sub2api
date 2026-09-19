<template>
  <fieldset :disabled="disabled" class="min-w-0 space-y-3" data-testid="daily-cooldown-settings">
    <label class="flex cursor-pointer items-center gap-2 text-sm font-medium text-gray-700 dark:text-gray-300">
      <input :checked="modelValue.enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" data-testid="daily-cooldown-enabled" @change="update({ enabled: ($event.target as HTMLInputElement).checked })" />
      <span>{{ t('admin.accounts.dailyCooldown.title') }}</span>
    </label>
    <p class="input-hint">{{ t('admin.accounts.dailyCooldown.hint') }}</p>
    <template v-if="modelValue.enabled">
      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <label class="block min-w-0">
          <span class="input-label">{{ t('admin.accounts.dailyCooldown.start') }}</span>
          <input :value="modelValue.start" type="time" step="60" required class="input min-w-0" data-testid="daily-cooldown-start" @input="update({ start: ($event.target as HTMLInputElement).value })" />
        </label>
        <label class="block min-w-0">
          <span class="input-label">{{ t('admin.accounts.dailyCooldown.end') }}</span>
          <input :value="modelValue.end" type="time" step="60" required class="input min-w-0" data-testid="daily-cooldown-end" @input="update({ end: ($event.target as HTMLInputElement).value })" />
        </label>
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
import { dailyCooldownValidationError, type DailyCooldown } from '@/utils/dailyCooldown'

const props = defineProps<{ modelValue: DailyCooldown; disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: DailyCooldown] }>()
const { t } = useI18n()
const error = computed(() => dailyCooldownValidationError(props.modelValue))
const update = (patch: Partial<DailyCooldown>) => emit('update:modelValue', { ...props.modelValue, ...patch })
</script>
