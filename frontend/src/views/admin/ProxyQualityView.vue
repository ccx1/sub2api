<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('admin.proxyQuality.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.proxyQuality.description') }}</p>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <span class="badge" :class="form.enabled ? 'badge-success' : 'badge-gray'">
            {{ form.enabled ? t('admin.proxyQuality.guardOn') : t('admin.proxyQuality.guardOff') }}
          </span>
          <span class="text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.proxyQuality.lastRun') }}: {{ lastRun ? formatDateTime(lastRun) : t('admin.proxyQuality.neverRun') }}
          </span>
          <button type="button" class="btn btn-secondary inline-flex items-center gap-2" :disabled="loading" @click="loadAll">
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
            {{ t('common.refresh') }}
          </button>
          <button type="button" class="btn btn-secondary inline-flex items-center gap-2" :disabled="running" @click="runNow">
            <Icon name="play" size="sm" />
            {{ running ? t('admin.proxyQuality.running') : t('admin.proxyQuality.runNow') }}
          </button>
          <button type="button" class="btn btn-primary inline-flex items-center gap-2" @click="openSettings">
            <Icon name="cog" size="sm" />
            {{ t('admin.proxyQuality.settings') }}
          </button>
        </div>
      </div>

      <div v-if="!form.enabled && !loading" class="card border-dashed px-4 py-3 text-sm text-amber-700 dark:text-amber-300">
        {{ t('admin.proxyQuality.guardOffHint') }}
      </div>

      <div class="grid grid-cols-2 gap-3 md:grid-cols-5">
        <button
          v-for="card in summaryCards"
          :key="card.key"
          type="button"
          class="card px-4 py-3 text-left transition"
          :class="filter === card.key ? 'ring-2 ring-primary-500' : ''"
          @click="filter = filter === card.key ? 'total' : card.key"
        >
          <div class="text-xs text-gray-500 dark:text-gray-400">{{ t(`admin.proxyQuality.summary.${card.key}`) }}</div>
          <div class="mt-1 text-2xl font-semibold tabular-nums" :class="card.color">{{ card.value }}</div>
        </button>
      </div>

      <div class="flex flex-wrap items-center gap-3">
        <input v-model.trim="keyword" class="input max-w-xs" type="search" :placeholder="t('admin.proxyQuality.filters.search')" />
      </div>

      <div v-if="loading && !items.length" class="flex justify-center py-16">
        <div class="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-600"></div>
      </div>
      <div v-else-if="filteredItems.length === 0" class="card border-dashed px-6 py-12 text-center">
        <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.proxyQuality.noData') }}</p>
      </div>
      <div v-else class="table-container overflow-x-auto">
        <table class="min-w-full text-sm">
          <thead>
            <tr class="border-b border-gray-100 text-left dark:border-dark-700">
              <th class="px-4 py-3">{{ t('admin.proxyQuality.columns.proxy') }}</th>
              <th class="px-4 py-3">{{ t('admin.proxyQuality.columns.quality') }}</th>
              <th class="px-4 py-3">{{ t('admin.proxyQuality.columns.state') }}</th>
              <th class="px-4 py-3 text-center">{{ t('admin.proxyQuality.columns.rounds') }}</th>
              <th class="px-4 py-3">{{ t('admin.proxyQuality.columns.lastChecked') }}</th>
              <th class="px-4 py-3">{{ t('admin.proxyQuality.columns.bindings') }}</th>
              <th class="px-4 py-3">{{ t('admin.proxyQuality.columns.lastError') }}</th>
              <th class="px-4 py-3 text-right">{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in filteredItems" :key="item.proxy_id" class="border-b border-gray-100 align-top dark:border-dark-700">
              <td class="px-4 py-3">
                <div class="font-medium text-gray-900 dark:text-white">{{ item.name || `#${item.proxy_id}` }}</div>
                <div class="text-xs text-gray-500 dark:text-gray-400">
                  {{ item.protocol }}://{{ item.host }}:{{ item.port }}
                  <span v-if="item.group_name" class="ml-1">· {{ item.group_name }}</span>
                </div>
                <div v-if="item.ip_address" class="text-xs text-gray-400">
                  {{ item.ip_address }}<span v-if="item.country_code"> · {{ item.country_code }}</span>
                </div>
              </td>
              <td class="px-4 py-3">
                <div class="flex items-center gap-2">
                  <span v-if="item.quality_score != null" class="font-semibold tabular-nums">{{ item.quality_score }}</span>
                  <span v-if="item.quality_grade" class="badge badge-gray">{{ item.quality_grade }}</span>
                  <span v-if="item.quality_score == null && !item.quality_grade" class="text-gray-400">-</span>
                </div>
                <div class="mt-1 text-xs" :class="item.pre_use_ready ? 'text-emerald-600 dark:text-emerald-400' : 'text-gray-400'">
                  {{ item.pre_use_ready ? t('admin.proxyQuality.preUseReady') : t('admin.proxyQuality.preUseNotReady') }}
                  <span v-if="item.latency_ms != null"> · {{ item.latency_ms }}ms</span>
                </div>
              </td>
              <td class="px-4 py-3">
                <span class="badge" :class="stateBadge(item).cls">{{ stateBadge(item).label }}</span>
                <div v-if="item.state === 'disabled' && item.disabled_until" class="mt-1 text-xs text-gray-500">
                  {{ t('admin.proxyQuality.disabledUntil', { time: formatDateTime(item.disabled_until) }) }}
                </div>
                <div v-if="!item.managed" class="mt-1 text-xs text-gray-400">{{ t('admin.proxyQuality.unmanagedHint') }}</div>
              </td>
              <td class="px-4 py-3 text-center tabular-nums">
                {{ t('admin.proxyQuality.roundsValue', { rounds: item.rounds, max: form.max_rounds }) }}
              </td>
              <td class="px-4 py-3 text-xs text-gray-500 dark:text-gray-400">
                {{ item.last_checked_at ? formatDateTime(item.last_checked_at) : '-' }}
              </td>
              <td class="px-4 py-3 text-xs text-gray-600 dark:text-gray-300">
                <div>{{ t('admin.proxyQuality.fixedBound', { count: item.fixed_bound_count }) }}</div>
                <div>{{ t('admin.proxyQuality.dynamicBound', { count: item.dynamic_bound_count }) }}</div>
              </td>
              <td class="max-w-xs px-4 py-3 text-xs text-red-600 dark:text-red-400">
                <span class="line-clamp-3" :title="item.last_error">{{ item.last_error || '-' }}</span>
              </td>
              <td class="px-4 py-3 text-right">
                <button
                  v-if="item.rounds > 0 || item.consecutive_failures > 0 || item.state === 'disabled'"
                  type="button"
                  class="btn btn-sm btn-secondary"
                  @click="resetTarget = item"
                >
                  {{ t('admin.proxyQuality.reset') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="card">
        <div class="border-b border-gray-100 px-4 py-3 text-sm font-semibold dark:border-dark-700">{{ t('admin.proxyQuality.events') }}</div>
        <div v-if="events.length === 0" class="px-4 py-6 text-center text-sm text-gray-500 dark:text-gray-400">
          {{ t('admin.proxyQuality.noEvents') }}
        </div>
        <div v-else class="max-h-[28rem] divide-y divide-gray-100 overflow-y-auto dark:divide-dark-700">
          <div v-for="event in events" :key="event.id" class="flex flex-wrap items-start justify-between gap-2 px-4 py-3 text-sm">
            <div class="min-w-0 flex-1">
              <span class="badge mr-2" :class="eventBadge(event.action)">{{ t(`admin.proxyQuality.actions.${event.action}`) }}</span>
              <span class="font-medium text-gray-900 dark:text-white">{{ event.proxy_name || `#${event.proxy_id}` }}</span>
              <span v-if="event.round > 0" class="ml-2 text-xs text-gray-400">{{ t('admin.proxyQuality.round', { round: event.round }) }}</span>
              <div class="mt-1 break-all text-xs text-gray-500 dark:text-gray-400">{{ event.reason }}</div>
            </div>
            <span class="text-xs text-gray-400">{{ formatDateTime(event.created_at) }}</span>
          </div>
        </div>
      </div>
    </div>

    <BaseDialog :show="showSettings" :title="t('admin.proxyQuality.settings')" width="wide" @close="showSettings = false">
      <div class="space-y-6">
        <div class="flex items-start gap-3">
          <Toggle v-model="draft.enabled" />
          <div>
            <div class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.proxyQuality.form.enabled') }}</div>
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.proxyQuality.form.enabledHint') }}</p>
          </div>
        </div>

        <section class="space-y-3">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.proxyQuality.form.sectionSchedule') }}</h3>
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <label class="space-y-1">
              <span class="input-label">{{ t('admin.proxyQuality.form.checkMode') }}</span>
              <Select v-model="draft.check_mode" :options="checkModeOptions" />
            </label>
            <label class="space-y-1">
              <span class="input-label">{{ t('admin.proxyQuality.form.minScore') }}</span>
              <input v-model.number="draft.min_score" class="input" type="number" min="0" max="100" :disabled="draft.check_mode !== 'full'" />
              <span class="input-hint">{{ t('admin.proxyQuality.form.minScoreHint') }}</span>
            </label>
            <label class="space-y-1">
              <span class="input-label">{{ t('admin.proxyQuality.form.checkIntervalMinutes') }}</span>
              <input v-model.number="draft.check_interval_minutes" class="input" type="number" min="1" />
              <span class="input-hint">{{ t('admin.proxyQuality.form.checkIntervalMinutesHint') }}</span>
            </label>
            <label class="space-y-1">
              <span class="input-label">{{ t('admin.proxyQuality.form.intervalSeconds') }}</span>
              <input v-model.number="draft.interval_seconds" class="input" type="number" min="15" step="15" />
              <span class="input-hint">{{ t('admin.proxyQuality.form.intervalSecondsHint') }}</span>
            </label>
            <label class="space-y-1">
              <span class="input-label">{{ t('admin.proxyQuality.form.maxChecksPerRun') }}</span>
              <input v-model.number="draft.max_checks_per_run" class="input" type="number" min="1" max="500" />
            </label>
            <label class="space-y-1">
              <span class="input-label">{{ t('admin.proxyQuality.form.concurrency') }}</span>
              <input v-model.number="draft.concurrency" class="input" type="number" min="1" max="32" />
            </label>
          </div>
          <label class="flex items-center gap-3">
            <Toggle v-model="draft.fail_on_challenge" />
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.proxyQuality.form.failOnChallenge') }}</span>
          </label>
        </section>

        <section class="space-y-3">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.proxyQuality.form.sectionPolicy') }}</h3>
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <label class="space-y-1">
              <span class="input-label">{{ t('admin.proxyQuality.form.failureThreshold') }}</span>
              <input v-model.number="draft.failure_threshold" class="input" type="number" min="1" max="100" />
            </label>
            <label class="space-y-1">
              <span class="input-label">{{ t('admin.proxyQuality.form.runtimeFailureThreshold') }}</span>
              <input v-model.number="draft.runtime_failure_threshold" class="input" type="number" min="0" max="1000" />
              <span class="input-hint">{{ t('admin.proxyQuality.form.runtimeFailureThresholdHint') }}</span>
            </label>
            <label class="space-y-1">
              <span class="input-label">{{ t('admin.proxyQuality.form.disableMinutes') }}</span>
              <input v-model.number="draft.disable_minutes" class="input" type="number" min="1" />
              <span class="input-hint">{{ t('admin.proxyQuality.form.disableMinutesHint') }}</span>
            </label>
            <label class="space-y-1">
              <span class="input-label">{{ t('admin.proxyQuality.form.maxRounds') }}</span>
              <input v-model.number="draft.max_rounds" class="input" type="number" min="1" max="100" />
              <span class="input-hint">{{ t('admin.proxyQuality.form.maxRoundsHint') }}</span>
            </label>
            <label class="space-y-1">
              <span class="input-label">{{ t('admin.proxyQuality.form.stableResetHours') }}</span>
              <input v-model.number="draft.stable_reset_hours" class="input" type="number" min="0" max="720" />
              <span class="input-hint">{{ t('admin.proxyQuality.form.stableResetHoursHint') }}</span>
            </label>
          </div>
          <div class="flex items-start gap-3">
            <Toggle v-model="draft.auto_delete" />
            <div>
              <div class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.proxyQuality.form.autoDelete') }}</div>
              <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.proxyQuality.form.autoDeleteHint') }}</p>
            </div>
          </div>
          <label class="flex items-center gap-3">
            <Toggle v-model="draft.include_fixed_bound" />
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.proxyQuality.form.includeFixedBound') }}</span>
          </label>
        </section>

        <section class="space-y-3">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.proxyQuality.form.sectionUsage') }}</h3>
          <label class="space-y-1">
            <span class="input-label">{{ t('admin.proxyQuality.form.preUseCheck') }}</span>
            <Select v-model="draft.pre_use_check" :options="preUseOptions" />
          </label>
        </section>
      </div>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="showSettings = false">{{ t('common.cancel') }}</button>
          <button type="button" class="btn btn-primary" :disabled="saving" @click="saveSettings">
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="!!resetTarget"
      :title="t('admin.proxyQuality.reset')"
      :message="t('admin.proxyQuality.resetConfirm', { name: resetTarget?.name || `#${resetTarget?.proxy_id}` })"
      @confirm="confirmReset"
      @cancel="resetTarget = null"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import type {
  ProxyQualityGuardEvent,
  ProxyQualityGuardItem,
  ProxyQualityGuardSettings,
  ProxyQualityGuardSummary
} from '@/types'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'

type SummaryKey = keyof ProxyQualityGuardSummary

const { t } = useI18n()
const appStore = useAppStore()

const defaultSettings = (): ProxyQualityGuardSettings => ({
  enabled: false,
  interval_seconds: 60,
  check_interval_minutes: 30,
  check_mode: 'full',
  min_score: 60,
  fail_on_challenge: false,
  failure_threshold: 1,
  runtime_failure_threshold: 3,
  disable_minutes: 10,
  max_rounds: 3,
  auto_delete: true,
  pre_use_check: 'prefer',
  include_fixed_bound: false,
  stable_reset_hours: 24,
  max_checks_per_run: 20,
  concurrency: 4
})

const items = ref<ProxyQualityGuardItem[]>([])
const events = ref<ProxyQualityGuardEvent[]>([])
const summary = ref<ProxyQualityGuardSummary>({ total: 0, healthy: 0, pending: 0, failing: 0, disabled: 0 })
const lastRun = ref<string | null>(null)
const loading = ref(false)
const running = ref(false)
const saving = ref(false)
const showSettings = ref(false)
const filter = ref<SummaryKey>('total')
const keyword = ref('')
const resetTarget = ref<ProxyQualityGuardItem | null>(null)
const form = reactive<ProxyQualityGuardSettings>(defaultSettings())
const draft = reactive<ProxyQualityGuardSettings>(defaultSettings())

const checkModeOptions = computed(() => [
  { value: 'full', label: t('admin.proxyQuality.form.checkModeFull') },
  { value: 'basic', label: t('admin.proxyQuality.form.checkModeBasic') }
])
const preUseOptions = computed(() => [
  { value: 'off', label: t('admin.proxyQuality.form.preUseOff') },
  { value: 'prefer', label: t('admin.proxyQuality.form.preUsePrefer') },
  { value: 'strict', label: t('admin.proxyQuality.form.preUseStrict') }
])

const summaryCards = computed(() => [
  { key: 'total' as const, value: summary.value.total, color: 'text-gray-900 dark:text-white' },
  { key: 'healthy' as const, value: summary.value.healthy, color: 'text-emerald-600 dark:text-emerald-400' },
  { key: 'pending' as const, value: summary.value.pending, color: 'text-gray-500 dark:text-gray-400' },
  { key: 'failing' as const, value: summary.value.failing, color: 'text-amber-600 dark:text-amber-400' },
  { key: 'disabled' as const, value: summary.value.disabled, color: 'text-red-600 dark:text-red-400' }
])

// 与后端 Overview 的汇总口径保持一致。
function summaryKey(item: ProxyQualityGuardItem): SummaryKey {
  if (item.state === 'disabled') return 'disabled'
  if (item.consecutive_failures > 0) return 'failing'
  if (item.pre_use_ready && item.proxy_status === 'active') return 'healthy'
  return 'pending'
}

const filteredItems = computed(() => {
  const q = keyword.value.toLowerCase()
  return items.value.filter((item) => {
    if (filter.value !== 'total' && summaryKey(item) !== filter.value) return false
    if (!q) return true
    return [item.name, item.host, item.ip_address, item.group_name]
      .some((value) => (value || '').toLowerCase().includes(q))
  })
})

function stateBadge(item: ProxyQualityGuardItem): { cls: string; label: string } {
  if (item.state === 'disabled') return { cls: 'badge-danger', label: t('admin.proxyQuality.state.disabled') }
  if (item.proxy_status !== 'active') return { cls: 'badge-gray', label: t('admin.proxyQuality.state.manualInactive') }
  if (!item.managed) return { cls: 'badge-gray', label: t('admin.proxyQuality.state.unmanaged') }
  if (item.consecutive_failures > 0) {
    return { cls: 'badge-warning', label: t('admin.proxyQuality.state.failing', { count: item.consecutive_failures }) }
  }
  if (!item.last_checked_at) return { cls: 'badge-gray', label: t('admin.proxyQuality.state.pending') }
  return { cls: 'badge-success', label: t('admin.proxyQuality.state.active') }
}

function eventBadge(action: ProxyQualityGuardEvent['action']): string {
  switch (action) {
    case 'disabled':
    case 'deleted':
      return 'badge-danger'
    case 'restored':
      return 'badge-success'
    case 'check_failed':
    case 'delete_skipped':
      return 'badge-warning'
    default:
      return 'badge-gray'
  }
}

async function loadAll() {
  loading.value = true
  try {
    const [overview, eventResponse] = await Promise.all([
      adminAPI.proxyQuality.overview(),
      adminAPI.proxyQuality.events()
    ])
    items.value = overview.items || []
    summary.value = overview.summary
    lastRun.value = overview.last_run || null
    Object.assign(form, overview.settings)
    events.value = eventResponse.items || []
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.proxyQuality.loadFailed')))
  } finally {
    loading.value = false
  }
}

function openSettings() {
  Object.assign(draft, form)
  showSettings.value = true
}

async function saveSettings() {
  saving.value = true
  try {
    const saved = await adminAPI.proxyQuality.updateSettings({ ...draft })
    Object.assign(form, saved)
    showSettings.value = false
    appStore.showSuccess(t('admin.proxyQuality.saveSuccess'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.proxyQuality.saveFailed')))
  } finally {
    saving.value = false
  }
}

async function runNow() {
  running.value = true
  try {
    const result = await adminAPI.proxyQuality.run()
    appStore.showSuccess(t('admin.proxyQuality.runResult', { ...result }))
    await loadAll()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.proxyQuality.runFailed')))
  } finally {
    running.value = false
  }
}

async function confirmReset() {
  const target = resetTarget.value
  resetTarget.value = null
  if (!target) return
  try {
    await adminAPI.proxyQuality.reset(target.proxy_id)
    appStore.showSuccess(t('admin.proxyQuality.resetSuccess'))
    await loadAll()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.proxyQuality.resetFailed')))
  }
}

onMounted(loadAll)
</script>
