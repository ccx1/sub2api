<template>
  <section class="min-w-0 space-y-4 border-t border-gray-200 pt-5 dark:border-dark-700" aria-labelledby="quality-results-title">
    <h3 id="quality-results-title" class="text-base font-semibold text-gray-900 dark:text-white">{{ t('codexModelQuality.resultsTitle') }}</h3>
    <CodexModelQualityAccountSelect v-model="accountId" />
    <div v-if="accountId" class="space-y-3">
      <div class="flex min-w-0 flex-wrap items-end gap-2">
        <label class="w-full min-w-0 space-y-1 sm:w-auto sm:flex-1">
          <span class="input-label">{{ t('codexModelQuality.targetModel') }}</span>
          <select v-model="model" data-testid="quality-model" class="input w-full">
            <option v-for="name in models" :key="name" :value="name">{{ name }}</option>
          </select>
        </label>
        <button type="button" class="btn btn-secondary" data-testid="quality-refresh" :disabled="loading" @click="refresh">{{ t('codexModelQuality.refresh') }}</button>
        <button type="button" class="btn btn-primary" data-testid="quality-retest" :disabled="!enabled || !model || loading || submitting || selectedRunning || selectedPaused" @click="schedule">{{ t(submitting ? 'codexModelQuality.scheduling' : 'codexModelQuality.retest') }}</button>
      </div>
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ t(enabled ? 'codexModelQuality.retestHint' : 'codexModelQuality.disabledHint') }}</p>
      <p v-if="loading" role="status" class="text-sm text-gray-500">{{ t('codexModelQuality.loading') }}</p>
      <p v-if="error" role="alert" class="break-words text-sm text-red-600 dark:text-red-400">{{ error }}</p>
      <p v-if="notice" role="status" data-testid="quality-schedule-notice" class="break-words text-sm text-cyan-700 dark:text-cyan-300">{{ notice }}</p>
      <div v-if="!loading && !error && !items.length" class="py-3 text-sm text-gray-500">{{ t('codexModelQuality.noResults') }}</div>
      <CodexModelQualityResult v-for="item in items" :key="item.model" :item="item" :now="currentTime" />
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getCodexModelQuality, scheduleCodexModelQuality, type ModelQualityStatus } from '@/api/admin/codexModelQuality'
import { extractApiErrorMessage } from '@/utils/apiError'
import CodexModelQualityAccountSelect from './CodexModelQualityAccountSelect.vue'
import CodexModelQualityResult from './CodexModelQualityResult.vue'

const props = defineProps<{ models: string[]; enabled: boolean }>()
const { t } = useI18n()
const accountId = ref<number | null>(null)
const model = ref('')
const items = ref<ModelQualityStatus[]>([])
const currentTime = ref(Date.now())
const loading = ref(false)
const error = ref('')
const notice = ref('')
const pendingKeys = ref(new Set<string>())
const scheduledChecks = ref(new Map<string, { expiresAt: number; previous?: ModelQualityStatus }>())
const actionKey = computed(() => `${accountId.value}:${model.value}`)
const submitting = computed(() => pendingKeys.value.has(actionKey.value))
const selectedRunning = computed(() => scheduledChecks.value.has(model.value) || items.value.some(item => item.model === model.value && isActive(item)))
const selectedPaused = computed(() => items.value.some(item => item.model === model.value && isPaused(item)))
let generation = 0
let requestSequence = 0
let poll: ReturnType<typeof setTimeout> | undefined

function stopPoll() { if (poll) clearTimeout(poll); poll = undefined }
function isActive(item: ModelQualityStatus) {
  return item.status === 'running' || (item.status === 'pending' && ['scheduled', 'checking'].includes(item.reason))
}
function isPaused(item: ModelQualityStatus) {
  return !!item.quality_paused_until && Date.parse(item.quality_paused_until) > currentTime.value
}
function reconcileScheduledChecks() {
  for (const [name, pending] of scheduledChecks.value) {
    const item = items.value.find(candidate => candidate.model === name)
    const changed = item && (item.checked_at !== pending.previous?.checked_at ||
      item.status !== pending.previous?.status || item.reason !== pending.previous?.reason)
    if (Date.now() >= pending.expiresAt || (item && isActive(item)) || (changed && item.status !== 'pending')) {
      scheduledChecks.value.delete(name)
    }
  }
}
function queuePoll() {
  stopPoll()
  reconcileScheduledChecks()
  let delay = scheduledChecks.value.size || items.value.some(isActive) ? 5000 : Infinity
  for (const item of items.value) {
    if (isPaused(item)) delay = Math.min(delay, Date.parse(item.quality_paused_until!) - Date.now() + 50)
  }
  if (Number.isFinite(delay)) poll = setTimeout(() => { void refresh() }, delay)
}
async function refresh() {
  const id = accountId.value
  if (!id) return
  stopPoll()
  const request = ++requestSequence
  const current = generation
  loading.value = true
  currentTime.value = Date.now()
  error.value = ''
  try {
    const result = await getCodexModelQuality(id, { schedule: false })
    if (current !== generation || request !== requestSequence) return
    items.value = result.filter(item => item.account_id === id)
  } catch (cause) {
    if (current === generation && request === requestSequence) error.value = extractApiErrorMessage(cause, t('codexModelQuality.resultsFailed'))
  } finally {
    if (current === generation && request === requestSequence) {
      loading.value = false
      queuePoll()
    }
  }
}
function scheduledNotice(reason: string) {
  const key = `codexModelQuality.reasons.${reason}`
  const text = reason ? t(key) : ''
  return `${t('codexModelQuality.notScheduled')}${text === key ? reason : text}`
}
async function schedule() {
  const id = accountId.value
  if (!id || !model.value || loading.value || submitting.value || selectedRunning.value || selectedPaused.value || !props.enabled) return
  const key = actionKey.value
  const targetModel = model.value
  const previous = items.value.find(item => item.model === targetModel)
  const current = generation
  pendingKeys.value.add(key)
  notice.value = ''
  error.value = ''
  try {
    const result = await scheduleCodexModelQuality(id, targetModel)
    if (current !== generation) return
    // 调度成功到 worker 写入状态之间，保留有时限的本地等待。
    if (result.scheduled) scheduledChecks.value.set(targetModel, { previous, expiresAt: Date.now() + 180000 })
    if (key === actionKey.value) notice.value = result.scheduled ? t('codexModelQuality.scheduled') : scheduledNotice(result.reason)
    await refresh()
  } catch (cause) {
    if (current === generation && key === actionKey.value) error.value = extractApiErrorMessage(cause, t('codexModelQuality.scheduleFailed'))
  } finally { pendingKeys.value.delete(key) }
}
watch(() => props.models, names => { if (!names.includes(model.value)) model.value = names[0] ?? '' }, { immediate: true })
watch(model, () => { notice.value = '' })
watch(accountId, () => {
  ++generation
  ++requestSequence
  stopPoll()
  scheduledChecks.value.clear()
  items.value = []
  error.value = ''
  notice.value = ''
  loading.value = false
  void refresh()
})
onUnmounted(() => { ++generation; stopPoll() })
</script>
