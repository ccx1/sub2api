<template>
  <label class="block min-w-0">
    <span class="input-label">{{ t('proxyGroups.optional') }}</span>
    <Select v-model="model" :options="options" :disabled="disabled" />
  </label>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ProxyGroup } from '@/types'
import Select from '@/components/common/Select.vue'

const props = defineProps<{ groups: ProxyGroup[]; disabled?: boolean }>()
const model = defineModel<number | null>({ required: true })
const { t } = useI18n()
const options = computed(() => {
  const result: Array<{ value: number | null; label: string }> = [{ value: null, label: t('proxyGroups.ungrouped') }]
  if (model.value && !props.groups.some(group => group.id === model.value)) {
    result.push({ value: model.value, label: t('proxyGroups.unavailable', { id: model.value }) })
  }
  result.push(...props.groups.map(group => ({ value: group.id, label: group.name })))
  return result
})
</script>
