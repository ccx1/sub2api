<template>
  <label class="block min-w-0">
    <span class="input-label">{{ t('proxyGroups.optional') }}</span>
    <select v-model="model" class="input" :disabled="disabled">
      <option :value="null">{{ t('proxyGroups.ungrouped') }}</option>
      <option v-if="model && !groups.some(group => group.id === model)" :value="model">
        {{ t('proxyGroups.unavailable', { id: model }) }}
      </option>
      <option v-for="group in groups" :key="group.id" :value="group.id">{{ group.name }}</option>
    </select>
  </label>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { ProxyGroup } from '@/types'

defineProps<{ groups: ProxyGroup[]; disabled?: boolean }>()
const model = defineModel<number | null>({ required: true })
const { t } = useI18n()
</script>
