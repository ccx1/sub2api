<template>
  <section class="space-y-4">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div class="min-w-0">
        <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('sharedPool.resourceOverview') }}</h2>
        <p class="mt-1 max-w-3xl text-sm leading-relaxed text-gray-500 dark:text-dark-400">{{ t('sharedPool.overviewHint') }}</p>
      </div>
      <button type="button" class="btn btn-primary shrink-0" data-test="contribute" @click="emit('contribute')">{{ t('sharedPool.contribute') }}</button>
    </div>
    <p v-if="stale && overview" role="status" class="rounded-lg border border-amber-200 bg-amber-50/60 p-3 text-sm text-amber-700 dark:border-amber-900/50 dark:bg-amber-950/20 dark:text-amber-400">{{ t('sharedPool.overviewStale') }}</p>
    <template v-if="overview">
      <div v-if="overview.tiers.length" class="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        <article v-for="tier in overview.tiers" :key="`${tier.platform}-${tier.tier}`" class="card min-w-0 p-4" data-test="tier-card">
          <div class="flex flex-wrap items-start justify-between gap-2">
            <div class="flex min-w-0 items-center gap-2">
              <PlatformIcon :platform="tier.platform" size="md" />
              <div class="min-w-0"><h3 class="break-words text-sm font-semibold text-gray-900 dark:text-white">{{ tierLabel(tier) }}</h3><p class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">{{ sharedPlatformNames[tier.platform] }}</p></div>
            </div>
            <span class="inline-flex items-center gap-1.5 rounded-full px-2 py-1 text-xs" :class="tierState(tier) === 'available' ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-400' : 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-dark-400'" data-test="tier-status"><span class="h-1.5 w-1.5 rounded-full bg-current"></span>{{ t(`sharedPool.${statusKeys[tierState(tier)]}`) }}</span>
          </div>
          <dl class="mt-4 grid grid-cols-2 gap-3 border-t border-gray-200 pt-3 dark:border-dark-700">
            <div class="min-w-0">
              <dt class="flex flex-wrap items-center gap-1 text-xs text-gray-500 dark:text-dark-400"><span>{{ t('sharedPool.participatingTotal') }}</span><span tabindex="0" :title="t('sharedPool.overviewParticipatingHint')" :aria-label="t('sharedPool.overviewParticipatingHint')" class="inline-flex rounded text-gray-400 focus-visible:ring-2 focus-visible:ring-cyan-500"><Icon name="infoCircle" size="xs" /></span></dt>
              <dd class="mt-1 break-all text-xl font-semibold tabular-nums" data-test="participating-accounts">{{ stale ? '—' : count(tier.participating_accounts) }} / {{ count(tier.total_accounts) }}</dd>
            </div>
            <div class="min-w-0"><dt class="flex flex-wrap items-center gap-1 text-xs text-gray-500 dark:text-dark-400">{{ t('sharedPool.participatingConcurrency') }}<span tabindex="0" :title="t('sharedPool.overviewParticipatingConcurrencyHint')" :aria-label="t('sharedPool.overviewParticipatingConcurrencyHint')" class="inline-flex rounded text-gray-400 focus-visible:ring-2 focus-visible:ring-cyan-500"><Icon name="infoCircle" size="xs" /></span></dt><dd class="mt-1 break-all text-xl font-semibold tabular-nums" data-test="concurrency-usage">{{ concurrency(tier) }}</dd></div>
          </dl>
        </article>
      </div>
      <div v-else class="py-10 text-center"><Icon name="users" size="xl" class="mx-auto mb-3 text-gray-400" /><h3 class="font-medium">{{ t('sharedPool.emptyOverview') }}</h3><p class="mt-2 text-sm text-gray-500 dark:text-dark-400">{{ t('sharedPool.emptyOverviewHint') }}</p></div>
      <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('sharedPool.overviewUpdatedAt') }} {{ overview.updated_at ? formatDateTime(overview.updated_at) : '—' }} · {{ t('sharedPool.overviewRefreshHint') }}</p>
    </template>
    <div v-else role="status" class="card p-8 text-center text-sm text-gray-500 dark:text-dark-400">{{ t('sharedPool.overviewUnavailableData') }}</div>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { formatDateTime } from '@/utils/format'
import type { SharedPoolCapacity, SharedPoolOverview, SharedPoolTier } from '@/api/sharedPool'
import { sharedPlatformNames, subscriptionTierOptions } from './settlementPolicy'

const props = defineProps<{ overview: SharedPoolOverview | null; stale?: boolean }>()
const emit = defineEmits<{ contribute: [] }>()
const { t } = useI18n()
const statusKeys = { available: 'overviewAvailable', unavailable: 'overviewUnavailable', unknown: 'overviewStatusUnknown' }
const count = (value: unknown): number | string => typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 ? value : '—'
function concurrency(value: SharedPoolCapacity) {
  if (props.stale) return '— / —'
  return `${count(value.current_concurrency)} / ${value.participating_concurrency_unlimited ? t('sharedPool.concurrencyUnlimited') : count(value.participating_concurrency)}`
}
function tierState(tier: SharedPoolTier): keyof typeof statusKeys {
  if (props.stale || count(tier.schedulable_accounts) === '—') return 'unknown'
  return tier.available && tier.schedulable_accounts > 0 ? 'available' : 'unavailable'
}
function tierLabel(tier: SharedPoolTier) {
  if (tier.tier === 'api_key') return 'API Key'
  return subscriptionTierOptions[tier.platform]?.find(option => option.value === tier.tier)?.label || t('sharedPool.unknownTier')
}
</script>
