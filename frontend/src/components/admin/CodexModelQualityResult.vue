<template>
  <article class="min-w-0 space-y-2 border-t border-gray-200 py-4 dark:border-dark-700" data-testid="quality-result">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <h4 class="break-all font-mono text-sm font-medium text-gray-900 dark:text-white">{{ item.model }}</h4>
      <div class="flex flex-wrap items-center gap-1">
        <span class="rounded px-2 py-1 text-xs font-medium" :class="statusClass">{{ t(`codexModelQuality.status.${item.status}`) }}</span>
        <span v-if="item.ticket_replaced" data-testid="quality-ticket-replaced" class="rounded bg-cyan-50 px-2 py-1 text-xs text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-300">{{ t('codexModelQuality.ticketReplaced') }}</span>
        <span v-else-if="item.previous_status" data-testid="quality-previous-status" class="rounded bg-gray-50 px-2 py-1 text-xs text-gray-600 dark:bg-dark-700 dark:text-gray-300">{{ t('codexModelQuality.previousStatus', { status: t(`codexModelQuality.status.${item.previous_status}`) }) }}</span>
      </div>
    </div>
    <p class="break-words text-sm text-gray-600 dark:text-gray-300" data-testid="quality-reason">{{ reasonText }}</p>
    <p v-if="['skipped', 'inconclusive'].includes(item.status)" class="text-xs text-gray-500 dark:text-gray-400">{{ t('codexModelQuality.incompleteHint') }}</p>
    <p v-if="item.ticket_replaced" class="text-xs text-cyan-700 dark:text-cyan-300">{{ t('codexModelQuality.ticketReplacedHint', { time: date(item.next_check_at) }) }}</p>
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
    <details v-if="item.history?.length" class="pt-1 text-xs text-gray-500 dark:text-gray-400" data-testid="quality-history">
      <summary class="cursor-pointer select-none">{{ t('codexModelQuality.historyTitle', { count: item.history.length }) }}</summary>
      <div class="mt-3 overflow-x-auto">
        <table class="min-w-full text-left">
          <thead>
            <tr class="text-gray-400">
              <th class="py-1 pr-4 font-normal">{{ t('codexModelQuality.checkedAt') }}</th>
              <th class="py-1 pr-4 font-normal">{{ t('codexModelQuality.historyResult') }}</th>
              <th class="py-1 pr-4 font-normal">{{ t('codexModelQuality.capabilityScore') }}</th>
              <th class="py-1 pr-4 font-normal">{{ t('codexModelQuality.duration') }}</th>
              <th class="py-1 pr-4 font-normal">{{ t('codexModelQuality.sourceTitle') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(entry, index) in item.history" :key="`${entry.checked_at}-${index}`" data-testid="quality-history-entry" class="border-t border-gray-100 align-top dark:border-dark-700">
              <td class="whitespace-nowrap py-1 pr-4">{{ date(entry.checked_at) }}</td>
              <td class="py-1 pr-4">
                <span :class="historyStatusClass(entry.status)">{{ t(`codexModelQuality.status.${entry.status}`) }}</span>
                <span class="text-gray-400"> · {{ reasonLabel(entry.reason) }}</span>
              </td>
              <td class="whitespace-nowrap py-1 pr-4">{{ entry.capability_score === undefined ? '—' : `${entry.capability_score.toFixed(0)} / 100` }}</td>
              <td class="whitespace-nowrap py-1 pr-4">{{ entry.duration_ms === undefined ? '—' : `${(entry.duration_ms / 1000).toFixed(1)} s` }}</td>
              <td class="whitespace-nowrap py-1 pr-4">{{ t(`codexModelQuality.source.${entry.source}`) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
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
const reasonText = computed(() => props.item.reason ? reasonLabel(props.item.reason) : '—')
function reasonLabel(reason: string) {
  const key = `codexModelQuality.reasons.${reason}`
  const result = t(key)
  return result === key ? reason : result
}
function historyStatusClass(status: ModelQualityStatus['status']) {
  if (status === 'passed') return 'text-emerald-700 dark:text-emerald-300'
  if (status === 'quarantined') return 'text-red-700 dark:text-red-300'
  if (status === 'suspect') return 'text-amber-700 dark:text-amber-300'
  return 'text-gray-700 dark:text-gray-300'
}
const score = computed(() => props.item.capability_score === undefined ? '—' : `${props.item.capability_score.toFixed(0)} / 100`)
const probability = computed(() => props.item.fingerprint_probability === undefined ? '—' : `${(props.item.fingerprint_probability * 100).toFixed(1)}%`)
function date(value?: string) {
  if (!value) return '—'
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString(locale?.value)
}
</script>
