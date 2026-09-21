<template>
  <p v-if="inline" class="input-hint" data-test="revenue-split-inline">{{ valid ? t('sharedPool.revenueSplitInline', { platform: percent(config.platform_rate_bps), proxy: percent(proxyRate), owner: percent(10000 - platformTotal) }) : t('sharedPool.rateUnavailable') }}</p>
  <div v-else class="space-y-3" :class="compact ? 'border-t border-gray-200 pt-4 dark:border-dark-700' : 'rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-800'" data-test="revenue-split">
    <h4 class="text-sm font-medium text-gray-900 dark:text-white">{{ t(compareProxyModes ? 'sharedPool.splitRateLabel' : 'sharedPool.revenueSplit') }}</h4>
    <template v-if="valid">
      <template v-if="compareProxyModes">
        <p class="text-2xl font-semibold text-gray-900 dark:text-white" data-test="conditional-platform-share">{{ percent(config.platform_rate_bps) }} <span class="text-base text-gray-500 dark:text-dark-400">+ ({{ percent(config.proxy_rate_bps) }})</span></p>
        <p class="text-sm text-gray-600 dark:text-dark-300">{{ t('sharedPool.feeConditionalHint', { rate: Number((config.proxy_rate_bps / 100).toFixed(2)) }) }}</p>
      </template>
      <dl class="grid gap-3" :class="compareProxyModes ? 'grid-cols-2' : compact ? 'grid-cols-3' : 'grid-cols-1 sm:grid-cols-3'">
        <div v-if="!compareProxyModes"><dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('sharedPool.platformShare') }}</dt><dd class="mt-1 font-semibold" data-test="platform-share">{{ percent(config.platform_rate_bps) }}</dd></div>
        <div v-if="!compareProxyModes"><dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('sharedPool.proxyShare') }}</dt><dd class="mt-1 font-semibold" data-test="proxy-share">{{ percent(proxyRate) }}</dd></div>
        <div><dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('sharedPool.yourShare') }}<span v-if="compareProxyModes"> · {{ t('sharedPool.randomProxy') }}</span></dt><dd class="mt-1 font-semibold text-emerald-600 dark:text-emerald-400" data-test="owner-share">{{ percent(10000 - platformTotal) }}</dd></div>
        <div v-if="compareProxyModes"><dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('sharedPool.yourShare') }} · {{ t('sharedPool.customProxy') }}</dt><dd class="mt-1 font-semibold text-emerald-600 dark:text-emerald-400" data-test="custom-owner-share">{{ percent(10000 - config.platform_rate_bps) }}</dd></div>
      </dl>
      <p v-if="!compact" class="text-xs text-gray-600 dark:text-dark-300"><span v-if="!compareProxyModes">{{ t('sharedPool.platformTotalShare') }} {{ percent(platformTotal) }} · </span>{{ t('sharedPool.revenueSplitHint') }}</p>
      <p v-if="!useRandomProxy && !compact" class="text-xs text-gray-500 dark:text-dark-400">{{ t('sharedPool.proxyShareInactive') }}</p>
    </template>
    <p v-else role="alert" class="text-sm text-amber-600 dark:text-amber-400">{{ t('sharedPool.rateUnavailable') }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SharedConfig } from '@/api/sharedPool'

const props = defineProps<{ config: Pick<SharedConfig, 'platform_rate_bps' | 'proxy_rate_bps'>; useRandomProxy: boolean; compact?: boolean; compareProxyModes?: boolean; inline?: boolean }>()
const { t } = useI18n()
const proxyRate = computed(() => props.useRandomProxy ? props.config.proxy_rate_bps : 0)
const platformTotal = computed(() => props.config.platform_rate_bps + proxyRate.value)
const valid = computed(() => [props.config.platform_rate_bps, props.config.proxy_rate_bps].every(value => Number.isInteger(value) && value >= 0 && value <= 10000) && platformTotal.value <= 10000)
const percent = (value: number) => `${Number((value / 100).toFixed(2))}%`
</script>
