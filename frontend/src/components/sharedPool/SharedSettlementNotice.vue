<template>
  <div class="space-y-1 text-xs leading-relaxed text-gray-500 dark:text-dark-400" data-test="settlement-notice">
    <p>{{ t('sharedPool.settlementPolicyHint') }}</p>
    <p>{{ t('sharedPool.defaultSettlementMultiplier') }} {{ validSettlementMultiplier(config.settlement_multiplier) ? `${config.settlement_multiplier}x` : t('sharedPool.settlementUnconfigured') }}</p>
    <p>{{ t('sharedPool.dispatchBillingHint') }}</p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { SharedConfig, SharedPlatform } from '@/api/sharedPool'
import { validSettlementMultiplier } from './settlementPolicy'
defineProps<{ config: SharedConfig; platform?: SharedPlatform }>()
const { t } = useI18n()
</script>
