<template>
  <section class="card overflow-hidden">
    <div class="border-b border-gray-200 px-5 py-4 dark:border-dark-700 sm:px-6">
      <h2 class="flex items-center gap-2 font-semibold text-gray-900 dark:text-white">
        <Icon name="chart" size="sm" class="text-cyan-600 dark:text-cyan-400" />
        {{ t('sharedPool.subscriptionRates') }}
      </h2>
      <p class="mt-1.5 text-sm leading-relaxed text-gray-500 dark:text-gray-400">
        {{ t('sharedPool.subscriptionRatesHint') }}
      </p>
    </div>
    <div class="space-y-5 p-5 sm:p-6">
      <div v-for="platform in platforms" :key="platform" class="space-y-3" :data-rate-platform="platform">
        <div class="flex items-center gap-2 text-sm font-medium text-gray-900 dark:text-gray-100">
          <PlatformIcon :platform="platform" />
          <span>{{ platformNames[platform] }}</span>
          <span class="text-xs font-normal text-gray-500 dark:text-gray-400">{{ t('sharedPool.subscriptionRatesOptional') }}</span>
        </div>
        <div v-if="!tierOptions[platform].length" class="rounded-lg bg-gray-50 px-3 py-2 text-sm text-gray-500 dark:bg-dark-800/60 dark:text-gray-400">
          {{ t('sharedPool.subscriptionRatesNoTiers') }}
        </div>
        <div v-else class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          <label v-for="tier in tierOptions[platform]" :key="tier.value" class="block" :data-rate-tier="tier.value">
            <span class="input-label">{{ tier.label }}</span>
            <div class="relative">
              <input
                :id="`subscription-rate-${platform}-${tier.value}`"
                v-model.number="values[platform][tier.value]"
                type="number"
                min="0"
                max="100"
                step="any"
                :disabled="disabled"
                class="input w-full pr-10"
                :aria-label="`${platformNames[platform]} ${tier.label} ${t('sharedPool.subscriptionMultiplier')}`"
              />
              <span class="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-sm text-gray-400">×</span>
            </div>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('sharedPool.subscriptionRatesFallback') }}</p>
          </label>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import type { SharedPlatform, SharedSettings } from '@/api/sharedPool'
import { sharedPlatforms as platforms, sharedPlatformNames as platformNames, subscriptionTierOptions as tierOptions, validSettlementMultiplier } from './settlementPolicy'

const props = defineProps<{ rates?: SharedSettings['subscription_settlement_multipliers']; disabled?: boolean }>()
const { t } = useI18n()
type RateValue = number | ''
const values = reactive<Record<SharedPlatform, Record<string, RateValue>>>({ openai: {}, anthropic: {}, gemini: {}, antigravity: {} })

function reset(value: SharedSettings['subscription_settlement_multipliers']) {
  for (const platform of platforms) {
    const next: Record<string, RateValue> = {}
    for (const tier of tierOptions[platform]) {
      const rate = value?.[platform]?.[tier.value]
      next[tier.value] = typeof rate === 'number' ? rate : ''
    }
    values[platform] = next
  }
}
watch(() => props.rates, reset, { immediate: true, deep: true })

function serialize(): NonNullable<SharedSettings['subscription_settlement_multipliers']> {
  const result: NonNullable<SharedSettings['subscription_settlement_multipliers']> = {}
  for (const platform of platforms) {
    const rates: Record<string, number> = {}
    for (const tier of tierOptions[platform]) {
      const value = values[platform][tier.value]
      if (value === '') continue
      if (!validSettlementMultiplier(value)) throw new Error(t('sharedPool.invalidSettlementMultiplier'))
      rates[tier.value] = value
    }
    if (Object.keys(rates).length) result[platform] = rates
  }
  return result
}
defineExpose({ serialize })
</script>
