<template>
  <section class="card space-y-3 p-5 sm:p-6">
    <h2 class="font-semibold">{{ t('sharedPool.settlementPolicy') }}</h2>
    <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('sharedPool.settlementSettingsHint') }}</p>
    <label class="block"><span class="input-label">{{ t('sharedPool.defaultSettlementMultiplier') }}</span><input v-model.number="multiplier" required type="number" min="0" max="100" step="any" :disabled="disabled" class="input w-full sm:w-48" data-settlement-default /></label>
  </section>
</template>
<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SharedSettlementPolicy } from '@/api/sharedPool'
import { validSettlementMultiplier } from './settlementPolicy'
const props = defineProps<{ settings: SharedSettlementPolicy; disabled?: boolean }>()
const { t } = useI18n()
const multiplier = ref<number | ''>(props.settings.settlement_multiplier ?? 1)
function serialize(): Required<SharedSettlementPolicy> {
  if (!validSettlementMultiplier(multiplier.value)) throw new Error(t('sharedPool.invalidSettlementMultiplier'))
  return { settlement_multiplier: multiplier.value }
}
defineExpose({ serialize })
</script>
