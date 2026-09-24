<template>
  <BaseDialog :show="show" :title="t('admin.accounts.codexModelQualityRecord')" width="extra-wide" @close="emit('close')">
    <div class="space-y-4" data-testid="codex-model-quality-record">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <p class="break-all text-sm font-medium text-gray-900 dark:text-gray-100">{{ account?.name }}</p>
        <div class="flex flex-wrap gap-2">
          <button type="button" class="btn btn-secondary" :disabled="loading || diagnosing || !account" @click="refresh">{{ t('codexModelQuality.refresh') }}</button>
          <button type="button" class="btn btn-primary" data-testid="codex-model-quality-diagnose" :disabled="loading || diagnosing || diagnosisRunning || !account" @click="diagnose">
            {{ t(diagnosing ? 'codexModelQuality.diagnosing' : 'codexModelQuality.diagnose') }}
          </button>
        </div>
      </div>
      <p v-if="loading" role="status" class="text-sm text-gray-500">{{ t('codexModelQuality.loading') }}</p>
      <p v-else-if="notice" role="status" class="text-sm text-cyan-700 dark:text-cyan-300">{{ notice }}</p>
      <p v-else-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
      <ul v-if="diagnosisReasons.length" data-testid="codex-model-quality-diagnosis-reasons" class="space-y-1 text-xs text-amber-700 dark:text-amber-300">
        <li v-for="item in diagnosisReasons" :key="item.model">
          {{ item.model }}：{{ reasonText(item.reason) }}
        </li>
      </ul>
      <p v-else-if="!items.length" class="py-8 text-center text-sm text-gray-500">{{ t('codexModelQuality.noResults') }}</p>
      <div v-else class="divide-y divide-gray-200 dark:divide-dark-700">
        <CodexModelQualityResult v-for="item in items" :key="item.model" :item="item" />
      </div>
    </div>
    <template #footer>
      <div class="flex justify-end">
        <button type="button" class="btn btn-primary" @click="emit('close')">{{ t('common.close') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import CodexModelQualityResult from '@/components/admin/CodexModelQualityResult.vue'
import { diagnoseCodexModelQuality, getCodexModelQuality, type ModelQualityStatus } from '@/api/admin/codexModelQuality'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{ show: boolean; account: { id: number; name: string } | null }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const loading = ref(false)
const diagnosing = ref(false)
const error = ref('')
const notice = ref('')
const items = ref<ModelQualityStatus[]>([])
const diagnosisReasons = ref<Array<{ model: string; reason: string }>>([])
const activeDiagnosis = ref(new Map<string, { baseline?: ModelQualityStatus; expiresAt: number }>())
const diagnosisRunning = computed(() => activeDiagnosis.value.size > 0)
let requestSequence = 0
let diagnosisPoll: ReturnType<typeof setTimeout> | undefined
let diagnosisPollRevision = 0

const diagnosisPollInterval = 2000
const diagnosisPollTimeout = 180000

function stopDiagnosisPoll() {
  if (diagnosisPoll) clearTimeout(diagnosisPoll)
  diagnosisPoll = undefined
  diagnosisPollRevision++
}

function diagnosisResultChanged(item: ModelQualityStatus, baseline?: ModelQualityStatus) {
  if (item.status === 'running' || (item.status === 'pending' && ['scheduled', 'checking'].includes(item.reason))) return false
  if (!baseline) return Boolean(item.checked_at)
  return item.checked_at !== baseline.checked_at || item.status !== baseline.status || item.reason !== baseline.reason
}

function runningStatus(model: string, fallback?: ModelQualityStatus): ModelQualityStatus {
  return {
    account_id: props.account?.id ?? fallback?.account_id ?? 0,
    model,
    status: 'running',
    reason: 'checking',
    sample_count: fallback?.sample_count ?? 0,
    requests: fallback?.requests ?? 0,
    model_identity: fallback?.model_identity ?? 'unknown',
    source: 'diagnostic',
    checked_at: fallback?.checked_at,
    ticket_captured_at: fallback?.ticket_captured_at,
    ticket_expires_at: fallback?.ticket_expires_at
  }
}

function mergeActiveDiagnosis(snapshot: ModelQualityStatus[]) {
  if (!activeDiagnosis.value.size) return snapshot
  const next = [...snapshot]
  const active = new Map(activeDiagnosis.value)
  const now = Date.now()
  for (const [model, run] of active) {
    const item = next.find(candidate => candidate.model === model)
    if (now >= run.expiresAt || (item && diagnosisResultChanged(item, run.baseline))) {
      active.delete(model)
      continue
    }
    const index = item ? next.indexOf(item) : -1
    const status = runningStatus(model, item ?? run.baseline)
    if (index >= 0) next[index] = status
    else next.push(status)
  }
  activeDiagnosis.value = active
  if (!active.size) stopDiagnosisPoll()
  return next
}

function scheduleDiagnosisPoll() {
  stopDiagnosisPoll()
  const revision = diagnosisPollRevision
  const poll = async () => {
    if (revision !== diagnosisPollRevision || !props.show || !props.account || !activeDiagnosis.value.size) return
    await load({ preserveNotice: true })
    if (revision !== diagnosisPollRevision || !activeDiagnosis.value.size) return
    diagnosisPoll = setTimeout(() => void poll(), diagnosisPollInterval)
  }
  diagnosisPoll = setTimeout(() => void poll(), diagnosisPollInterval)
}

async function load(options: { preserveNotice?: boolean } = {}) {
  if (!props.show || !props.account) return
  const sequence = ++requestSequence
  loading.value = true
  error.value = ''
  if (!options.preserveNotice) notice.value = ''
  try {
    const result = await getCodexModelQuality(props.account.id, { schedule: false })
    if (sequence === requestSequence) items.value = mergeActiveDiagnosis(result)
  } catch (cause) {
    if (sequence === requestSequence) error.value = extractApiErrorMessage(cause, t('codexModelQuality.resultsFailed'))
  } finally {
    if (sequence === requestSequence) loading.value = false
  }
}

async function refresh() {
  await load()
}

function reasonText(reason: string) {
  const key = `codexModelQuality.reasons.${reason}`
  const translated = t(key)
  return translated === key ? reason : translated
}

async function diagnose() {
  if (!props.show || !props.account || diagnosing.value || diagnosisRunning.value) return
  diagnosing.value = true
  error.value = ''
  notice.value = ''
  diagnosisReasons.value = []
  const baselines = new Map(items.value.map(item => [item.model, item]))
  try {
    const result = await diagnoseCodexModelQuality(props.account.id, items.value.map(item => item.model))
    const scheduled = result.items.filter(item => item.scheduled).length
    const active = new Map(activeDiagnosis.value)
    const expiresAt = Date.now() + diagnosisPollTimeout
    for (const item of result.items) {
      if (item.scheduled) active.set(item.model, { baseline: baselines.get(item.model), expiresAt })
    }
    activeDiagnosis.value = active
    diagnosisReasons.value = result.items.filter(item => !item.scheduled)
      .map(item => ({ model: item.model, reason: item.reason }))
    notice.value = scheduled > 0
      ? t('codexModelQuality.diagnosed', { count: scheduled })
      : t('codexModelQuality.diagnoseNoop')
    await load({ preserveNotice: true })
    if (activeDiagnosis.value.size) scheduleDiagnosisPoll()
  } catch (cause) {
    error.value = extractApiErrorMessage(cause, t('codexModelQuality.diagnoseFailed'))
  } finally {
    diagnosing.value = false
  }
}

watch(() => [props.show, props.account?.id], () => {
  stopDiagnosisPoll()
  activeDiagnosis.value = new Map()
  items.value = []
  error.value = ''
  notice.value = ''
  diagnosisReasons.value = []
  if (props.show) void load()
}, { immediate: true })

onUnmounted(stopDiagnosisPoll)
</script>
