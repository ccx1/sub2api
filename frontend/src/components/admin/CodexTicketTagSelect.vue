<template>
  <div data-testid="tag-select" class="min-w-0 space-y-2" role="group" :aria-label="label">
    <label :for="selectId" class="input-label">{{ label }}</label>
    <div v-if="selectedValues.length" class="flex min-w-0 flex-wrap gap-2">
      <span
        v-for="value in selectedValues"
        :key="value"
        data-testid="selected-tag"
        class="inline-flex max-w-full items-center gap-1 rounded-md border border-primary-200 bg-primary-50 px-2 py-1 text-sm text-primary-800 dark:border-primary-800 dark:bg-primary-900/20 dark:text-primary-200"
      >
        <span class="min-w-0 break-all">{{ optionLabel(value) }}</span>
        <button
          type="button"
          :data-testid="`tag-remove-${value}`"
          :aria-label="`${t('common.remove')} ${optionLabel(value)}`"
          :disabled="disabled"
          class="shrink-0 rounded p-0.5 hover:bg-primary-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:cursor-not-allowed disabled:opacity-50 dark:hover:bg-primary-800"
          @click="removeValue(value)"
        >
          <Icon name="x" size="sm" />
        </button>
      </span>
    </div>
    <!-- 禁用时重建，关闭已传送到 body 的菜单，避免绕过外层 fieldset。 -->
    <Select
      :key="selectDisabled ? 'disabled' : 'enabled'"
      :id="selectId"
      :model-value="null"
      :options="availableOptions"
      :placeholder="placeholder"
      :aria-label="label"
      :disabled="selectDisabled"
      :searchable="true"
      :creatable="false"
      @update:model-value="addValue"
    />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'

type TagOption = {
  value: string
  label: string
  disabled?: boolean
}

const props = withDefaults(defineProps<{
  modelValue: string[]
  options: TagOption[]
  label: string
  placeholder: string
  disabled?: boolean
  max?: number
}>(), { disabled: false, max: 32 })

const emit = defineEmits<{ 'update:modelValue': [value: string[]] }>()
const { t } = useI18n()
const selectId = `codex-ticket-tags-${Math.random().toString(36).slice(2, 10)}`
const selectedValues = computed(() => [...new Set(props.modelValue)])
const optionMap = computed(() => new Map(props.options.map(option => [option.value, option])))
const availableOptions = computed(() => [...optionMap.value.values()]
  .filter(option => !selectedValues.value.includes(option.value)))
const selectDisabled = computed(() => props.disabled || selectedValues.value.length >= props.max)
const optionLabel = (value: string) => optionMap.value.get(value)?.label ?? value

function addValue(value: string | number | boolean | null) {
  if (selectDisabled.value || typeof value !== 'string' || selectedValues.value.includes(value)) return
  const option = optionMap.value.get(value)
  if (!option || option.disabled) return
  emit('update:modelValue', [...selectedValues.value, value])
}

function removeValue(value: string) {
  if (props.disabled || !selectedValues.value.includes(value)) return
  emit('update:modelValue', selectedValues.value.filter(item => item !== value))
}
</script>
