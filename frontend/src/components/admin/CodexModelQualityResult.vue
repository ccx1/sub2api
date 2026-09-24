<template>
  <article class="min-w-0 space-y-2 border-t border-gray-200 py-4 dark:border-dark-700" data-testid="quality-result">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <h4 class="break-all font-mono text-sm font-medium text-gray-900 dark:text-white">{{ item.model }}</h4>
      <span class="rounded px-2 py-1 text-xs font-medium" :class="statusClass">{{ t(`codexModelQuality.status.${item.status}`) }}</span>
    </div>
    <p class="break-words text-sm text-gray-600 dark:text-gray-300" data-testid="quality-reason">{{ reasonText }}</p>
    <p v-if="['skipped', 'inconclusive'].includes(item.status)" class="text-xs text-gray-500 dark:text-gray-400">{{ t('codexModelQuality.incompleteHint') }}</p>
    <p v-if="item.baseline_reused" class="text-xs text-cyan-700 dark:text-cyan-300">{{ t('codexModelQuality.baselineReused') }}</p>
    <p v-if="qualityPaused" data-testid="quality-cooldown-notice" class="text-xs text-amber-700 dark:text-amber-300">{{ t('codexModelQuality.qualityPausedHint') }}</p>
    <dl class="grid gap-x-5 gap-y-2 text-xs text-gray-500 dark:text-gray-400 sm:grid-cols-2 lg:grid-cols-3">
      <div><dt>{{ t('codexModelQuality.capabilityScore') }}</dt><dd class="mt-1 text-sm text-gray-900 dark:text-gray-100">{{ score }}</dd></div>
      <div><dt>{{ t('codexModelQuality.modelIdentity') }}</dt><dd class="mt-1 text-gray-900 dark:text-gray-100">{{ t(`codexModelQuality.identity.${item.model_identity}`) }}</dd></div>
      <div><dt>{{ t('codexModelQuality.duration') }}</dt><dd class="mt-1">{{ item.duration_ms === undefined ? '—' : `${(item.duration_ms / 1000).toFixed(1)} s` }}</dd></div>
      <div><dt>{{ t('codexModelQuality.checkedAt') }}</dt><dd class="mt-1">{{ date(item.checked_at) }}</dd></div>
      <div><dt>{{ t('codexModelQuality.nextCheckAt') }}</dt><dd class="mt-1">{{ date(item.next_check_at) }}</dd></div>
      <div><dt>{{ t('codexModelQuality.sourceTitle') }}</dt><dd class="mt-1">{{ t(`codexModelQuality.source.${item.source}`) }}</dd></div>
      <div><dt>{{ t('codexModelQuality.consecutiveLowQuality') }}</dt><dd data-testid="quality-consecutive-count" class="mt-1">{{ item.consecutive_low_quality ?? 0 }}</dd></div>
      <div v-if="item.quality_paused_until"><dt>{{ t('codexModelQuality.qualityPausedUntil') }}</dt><dd data-testid="quality-paused-until" class="mt-1">{{ date(item.quality_paused_until) }}</dd></div>
    </dl>
    <details class="pt-1 text-xs text-gray-500 dark:text-gray-400">
      <summary class="cursor-pointer select-none">{{ t('codexModelQuality.evidence') }}</summary>
      <dl class="mt-3 grid gap-x-5 gap-y-2 sm:grid-cols-2">
        <div><dt>{{ t('codexModelQuality.fingerprintCandidate') }}</dt><dd class="mt-1 break-all">{{ item.fingerprint_candidate || '—' }}</dd></div>
        <div><dt>{{ t('codexModelQuality.fingerprintProbability') }}</dt><dd class="mt-1">{{ probability }}</dd></div>
        <div><dt>{{ t('codexModelQuality.fingerprintSimilarity') }}</dt><dd class="mt-1">{{ item.fingerprint_similarity?.toFixed(3) ?? '—' }}</dd></div>
        <div><dt>{{ t('codexModelQuality.samples') }}</dt><dd class="mt-1">{{ item.sample_count }} / {{ item.requests }}</dd></div>
        <div><dt>{{ t('codexModelQuality.capturedAt') }}</dt><dd class="mt-1">{{ date(item.ticket_captured_at) }}</dd></div>
        <div><dt>{{ t('codexModelQuality.expiresAt') }}</dt><dd class="mt-1">{{ date(item.ticket_expires_at) }}</dd></div>
      </dl>
      <p class="mt-3">{{ t('codexModelQuality.fingerprintHint') }}</p>
    </details>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ModelQualityStatus } from '@/api/admin/codexModelQuality'
const props = withDefaults(defineProps<{ item: ModelQualityStatus; now?: number }>(), { now: () => Date.now() })
const { t, locale } = useI18n()
const qualityPaused = computed(() => !!props.item.quality_paused_until && Date.parse(props.item.quality_paused_until) > props.now)
const statusClass = computed(() => {
  if (props.item.status === 'passed') return 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/30 dark:text-emerald-300'
  if (props.item.status === 'quarantined') return 'bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-300'
  if (props.item.status === 'suspect') return 'bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-300'
  return 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300'
})
const reasonText = computed(() => {
  if (!props.item.reason) return '—'
  const key = `codexModelQuality.reasons.${props.item.reason}`
  const result = t(key)
  return result === key ? props.item.reason : result
})
const score = computed(() => props.item.capability_score === undefined ? '—' : `${props.item.capability_score.toFixed(0)} / 100`)
const probability = computed(() => props.item.fingerprint_probability === undefined ? '—' : `${(props.item.fingerprint_probability * 100).toFixed(1)}%`)
function date(value?: string) {
  if (!value) return '—'
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString(locale?.value)
}
</script>
