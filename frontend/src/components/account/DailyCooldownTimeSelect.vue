<template>
  <div class="min-w-0" role="group" :aria-label="label">
    <p class="input-label">{{ label }}</p>
    <div class="grid min-w-0 grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-2">
      <!-- 禁用时重建选择器，关闭 fieldset 外已打开的 Teleport 菜单。 -->
      <Select :key="`hour-${disabled}`" :model-value="hour" :options="hours" :disabled="disabled" :aria-label="t('admin.accounts.dailyCooldown.hourLabel', { label })" placeholder="--" class="min-w-0" @update:model-value="update('hour', $event)" />
      <span aria-hidden="true" class="text-sm text-gray-400">:</span>
      <Select :key="`minute-${disabled}`" :model-value="minute" :options="minutes" :disabled="disabled" :aria-label="t('admin.accounts.dailyCooldown.minuteLabel', { label })" placeholder="--" class="min-w-0" @update:model-value="update('minute', $event)" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'

const props = defineProps<{ modelValue: string; label: string; disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const { t } = useI18n()
const options = (count: number) => Array.from({ length: count }, (_, index) => {
  const value = String(index).padStart(2, '0')
  return { value, label: value }
})
const hours = options(24)
const minutes = options(60)
const hour = computed(() => hours.find(option => option.value === props.modelValue.split(':')[0])?.value)
const minute = computed(() => minutes.find(option => option.value === props.modelValue.split(':')[1])?.value)
function update(part: 'hour' | 'minute', value: string | number | boolean | null) {
  if (props.disabled || !(part === 'hour' ? hours : minutes).some(option => option.value === value)) return
  emit('update:modelValue', part === 'hour' ? `${value}:${minute.value ?? '00'}` : `${hour.value ?? '00'}:${value}`)
}
</script>
