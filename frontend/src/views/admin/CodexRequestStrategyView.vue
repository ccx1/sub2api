<template>
  <AppLayout>
    <div class="mx-auto max-w-4xl space-y-6">
      <header class="space-y-2">
        <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('codexRequestStrategy.title') }}</h1>
        <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('codexRequestStrategy.description') }}</p>
      </header>

      <div v-if="loading" class="flex justify-center py-12">
        <div class="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-600" />
      </div>

      <form v-else class="space-y-6" @submit.prevent="save">
        <section class="card space-y-4 p-5" aria-labelledby="request-strategy-policy-title">
          <div>
            <h2 id="request-strategy-policy-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('codexRequestStrategy.policyTitle') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('codexRequestStrategy.policyHint') }}</p>
          </div>
          <label class="flex items-start gap-3">
            <input v-model="form.enabled" type="checkbox" data-testid="strategy-enabled" class="mt-1 h-4 w-4" />
            <span class="text-sm text-gray-900 dark:text-white">
              {{ t('codexRequestStrategy.enabled') }}
              <span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">{{ t('codexRequestStrategy.enabledHint') }}</span>
            </span>
          </label>
          <div v-if="form.enabled" class="rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-200" role="note">
            {{ t('codexRequestStrategy.experimentalWarning') }}
          </div>
        </section>

        <section class="card space-y-4 p-5" aria-labelledby="request-strategy-options-title">
          <div>
            <h2 id="request-strategy-options-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('codexRequestStrategy.optionsTitle') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('codexRequestStrategy.optionsHint') }}</p>
          </div>
          <div class="grid gap-4 sm:grid-cols-2">
            <label class="space-y-1">
              <span class="input-label">{{ t('codexRequestStrategy.strategy') }}</span>
              <select v-model="form.strategy" class="input w-full" data-testid="strategy">
                <option value="native">{{ t('codexRequestStrategy.strategyNative') }}</option>
                <option value="cookie_previous_ws">{{ t('codexRequestStrategy.strategyCookiePreviousWS') }}</option>
              </select>
            </label>
            <label class="space-y-1">
              <span class="input-label">{{ t('codexRequestStrategy.scope') }}</span>
              <select v-model="form.scope" class="input w-full" data-testid="scope">
                <option value="dedicated">{{ t('codexRequestStrategy.scopeDedicated') }}</option>
                <option value="passthrough">{{ t('codexRequestStrategy.scopePassthrough') }}</option>
                <option value="all">{{ t('codexRequestStrategy.scopeAll') }}</option>
              </select>
            </label>
            <label class="space-y-1">
              <span class="input-label">{{ t('codexRequestStrategy.failureMode') }}</span>
              <select v-model="form.failure_mode" class="input w-full" data-testid="failure-mode">
                <option value="fallback">{{ t('codexRequestStrategy.failureFallback') }}</option>
                <option value="reject">{{ t('codexRequestStrategy.failureReject') }}</option>
              </select>
            </label>
            <label class="space-y-1">
              <span class="input-label">{{ t('codexRequestStrategy.probeTimeout') }}</span>
              <input v-model.number="form.probe_timeout_seconds" type="number" min="5" max="60" step="1" class="input w-full" data-testid="probe-timeout" />
              <span class="block text-xs text-gray-500 dark:text-gray-400">{{ t('codexRequestStrategy.range', { min: 5, max: 60 }) }}</span>
            </label>
            <label class="min-w-0 space-y-1 sm:col-span-2">
              <span class="input-label">{{ t('codexRequestStrategy.cookieMode') }}</span>
              <select v-model="form.cookie_mode" class="input w-full" data-testid="cookie-mode" aria-describedby="cookie-mode-detail cookie-mode-hint">
                <option value="preserve">{{ t('codexRequestStrategy.cookiePreserve') }}</option>
                <option value="strip_routing">{{ t('codexRequestStrategy.cookieStripRouting') }}</option>
                <option value="strip_cloudflare">{{ t('codexRequestStrategy.cookieStripCloudflare') }}</option>
                <option value="strip_infrastructure">{{ t('codexRequestStrategy.cookieStripInfrastructure') }}</option>
              </select>
              <span id="cookie-mode-detail" class="block break-words text-xs leading-5 text-gray-500 dark:text-gray-400">{{ cookieModeDetail }}</span>
              <span id="cookie-mode-hint" class="block text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('codexRequestStrategy.cookieModeHint') }}</span>
            </label>
            <label class="space-y-1">
              <span class="input-label">{{ t('codexRequestStrategy.regionMode') }}</span>
              <select v-model="form.region_mode" class="input w-full" data-testid="region-mode">
                <option value="strip">{{ t('codexRequestStrategy.regionStrip') }}</option>
                <option value="preserve">{{ t('codexRequestStrategy.regionPreserve') }}</option>
                <option value="override">{{ t('codexRequestStrategy.regionOverride') }}</option>
              </select>
            </label>
            <label v-if="form.region_mode === 'override'" class="space-y-1">
              <span class="input-label">{{ t('codexRequestStrategy.routingOverride') }}</span>
              <select v-model="form.account_routing_override" class="input w-full" data-testid="routing-override">
                <option value="">{{ t('codexRequestStrategy.noOverride') }}</option>
                <option value="NO_CONSTRAINT">NO_CONSTRAINT</option>
                <option value="us">us</option>
                <option value="us_cr">us_cr</option>
              </select>
            </label>
            <label v-if="form.region_mode === 'override'" class="space-y-1">
              <span class="input-label">{{ t('codexRequestStrategy.residency') }}</span>
              <select v-model="form.residency" class="input w-full" data-testid="residency">
                <option value="">{{ t('codexRequestStrategy.noOverride') }}</option>
                <option value="us">us</option>
              </select>
            </label>
            <label class="space-y-1">
              <span class="input-label">{{ t('codexRequestStrategy.timeContextMode') }}</span>
              <select v-model="form.time_context_mode" class="input w-full" data-testid="time-context-mode">
                <option value="strip">{{ t('codexRequestStrategy.timeStrip') }}</option>
                <option value="preserve">{{ t('codexRequestStrategy.timePreserve') }}</option>
                <option value="override">{{ t('codexRequestStrategy.timeOverride') }}</option>
              </select>
            </label>
            <label v-if="form.time_context_mode === 'override'" class="space-y-1">
              <span class="input-label">{{ t('codexRequestStrategy.timezoneLabel') }}</span>
              <input v-model="form.timezone" type="text" class="input w-full font-mono" :placeholder="t('codexRequestStrategy.timezonePlaceholder')" data-testid="policy-timezone" />
            </label>
            <label class="space-y-1">
              <span class="input-label">{{ t('codexRequestStrategy.complianceMode') }}</span>
              <select v-model="form.compliance_mode" class="input w-full" data-testid="compliance-mode">
                <option value="account">{{ t('codexRequestStrategy.complianceAccount') }}</option>
                <option value="preserve">{{ t('codexRequestStrategy.compliancePreserve') }}</option>
                <option value="strip">{{ t('codexRequestStrategy.complianceStrip') }}</option>
              </select>
            </label>
          </div>
        </section>

        <section class="card space-y-4 p-5" aria-labelledby="request-route-management-title">
          <div>
            <h2 id="request-route-management-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('codexRequestStrategy.routeManagementTitle') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('codexRequestStrategy.routeManagementHint') }}</p>
          </div>
          <div class="grid gap-4 sm:grid-cols-2">
            <label class="space-y-1 sm:col-span-2">
              <span class="input-label">{{ t('codexRequestStrategy.routeAffinityMode') }}</span>
              <select v-model="form.route_affinity_mode" class="input w-full" data-testid="route-affinity-mode" aria-describedby="route-affinity-hint">
                <option value="off">{{ t('codexRequestStrategy.routeAffinityOff') }}</option>
                <option value="prefer">{{ t('codexRequestStrategy.routeAffinityPrefer') }}</option>
                <option value="strict">{{ t('codexRequestStrategy.routeAffinityStrict') }}</option>
              </select>
              <span id="route-affinity-hint" class="block text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('codexRequestStrategy.routeAffinityHint') }}</span>
            </label>
            <label class="space-y-1">
              <span class="input-label">{{ t('codexRequestStrategy.routePrewarmConnections') }}</span>
              <input v-model.number="form.route_prewarm_connections" type="number" min="0" max="12" step="1" class="input w-full" data-testid="route-prewarm-connections" />
              <span class="block text-xs text-gray-500 dark:text-gray-400">{{ t('codexRequestStrategy.routePrewarmHint') }}</span>
            </label>
            <label class="space-y-1">
              <span class="input-label">{{ t('codexRequestStrategy.routeFailureCooldown') }}</span>
              <input v-model.number="form.route_failure_cooldown_seconds" type="number" min="30" max="900" step="1" class="input w-full" data-testid="route-failure-cooldown" />
              <span class="block text-xs text-gray-500 dark:text-gray-400">{{ t('codexRequestStrategy.routeCooldownHint') }}</span>
            </label>
          </div>
          <p class="text-xs leading-5 text-gray-500 dark:text-gray-400" role="note">{{ t('codexRequestStrategy.routeQualityHint') }}</p>
        </section>

        <section id="timezone" class="card space-y-4 p-5" aria-labelledby="request-timezone-title">
          <div>
            <h2 id="request-timezone-title" class="text-lg font-semibold text-gray-900 dark:text-white">
              {{ t('codexRequestStrategy.timezoneTitle') }}
            </h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
              {{ t('codexRequestStrategy.timezoneHint') }}
            </p>
          </div>
          <label for="openai-request-timezone" class="block space-y-1">
            <span class="input-label">{{ t('codexRequestStrategy.timezoneLabel') }}</span>
            <input
              id="openai-request-timezone"
              v-model="timezone"
              type="text"
              class="input w-full font-mono"
              :placeholder="t('codexRequestStrategy.timezonePlaceholder')"
              autocomplete="off"
              spellcheck="false"
              data-testid="openai-request-timezone"
            />
          </label>
          <p class="rounded-lg border border-sky-200 bg-sky-50 p-3 text-xs leading-5 text-sky-800 dark:border-sky-800 dark:bg-sky-900/20 dark:text-sky-200" role="note">
            {{ t('codexRequestStrategy.timezoneAttestationHint') }}
          </p>
          <div class="flex flex-wrap items-center justify-end gap-3">
            <button type="button" class="btn btn-secondary" data-testid="clear-timezone" :disabled="timezoneSaving || !timezone" @click="timezone = ''">
              {{ t('codexRequestStrategy.timezoneClear') }}
            </button>
            <button type="button" class="btn btn-primary" data-testid="save-timezone" :disabled="timezoneSaving" @click="saveTimezone">
              {{ t(timezoneSaving ? 'codexRequestStrategy.timezoneSaving' : 'codexRequestStrategy.timezoneSave') }}
            </button>
          </div>
          <p v-if="timezoneSaveError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ timezoneSaveError }}</p>
          <p v-if="timezoneSaved" role="status" class="text-sm text-emerald-700 dark:text-emerald-300">{{ t('codexRequestStrategy.timezoneSaved') }}</p>
        </section>

        <footer class="space-y-3">
          <p v-if="loadError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ loadError }}</p>
          <p v-if="saveError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ saveError }}</p>
          <p v-if="saved" role="status" class="text-sm text-emerald-700 dark:text-emerald-300">{{ t('codexRequestStrategy.saved') }}</p>
          <div class="flex justify-end">
            <button type="submit" class="btn btn-primary" data-testid="save-strategy" :disabled="saving || !!validationError">
              {{ t(saving ? 'codexRequestStrategy.saving' : 'codexRequestStrategy.save') }}
            </button>
          </div>
          <p v-if="validationError" role="alert" class="text-sm text-amber-700 dark:text-amber-300">{{ validationError }}</p>
        </footer>
      </form>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { getCodexRequestStrategyPolicy, saveCodexRequestStrategyPolicy, type CodexRequestStrategyPolicy } from '@/api/admin/codexRequestStrategy'
import { getSettings, updateSettings } from '@/api/admin/settings'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const loading = ref(true)
const saving = ref(false)
const saved = ref(false)
const loadError = ref('')
const saveError = ref('')
const timezone = ref('')
const timezoneSaving = ref(false)
const timezoneSaved = ref(false)
const timezoneSaveError = ref('')
const form = ref<CodexRequestStrategyPolicy>({
  enabled: false,
  strategy: 'native',
  scope: 'dedicated',
  failure_mode: 'fallback',
  probe_timeout_seconds: 10,
  cookie_mode: 'preserve',
  route_affinity_mode: 'off',
  route_prewarm_connections: 4,
  route_failure_cooldown_seconds: 120,
  region_mode: 'strip',
  account_routing_override: '',
  residency: '',
  time_context_mode: 'strip',
  timezone: '',
  compliance_mode: 'account',
})

const cookieModeDetail = computed(() => t(`codexRequestStrategy.cookieDetail.${form.value.cookie_mode}`))

const validationError = computed(() => {
  if (form.value.route_affinity_mode === 'strict' && ['strip_routing', 'strip_infrastructure'].includes(form.value.cookie_mode)) return t('codexRequestStrategy.cookieStrictConflict')
  if (!isIntegerInRange(form.value.probe_timeout_seconds, 5, 60)) return t('codexRequestStrategy.invalidProbeTimeout')
  if (!isIntegerInRange(form.value.route_prewarm_connections, 0, 12)) return t('codexRequestStrategy.invalidRoutePrewarm')
  if (!isIntegerInRange(form.value.route_failure_cooldown_seconds, 30, 900)) return t('codexRequestStrategy.invalidRouteCooldown')
  return ''
})

function isIntegerInRange(value: number, min: number, max: number) {
  return Number.isFinite(value) && Number.isInteger(value) && value >= min && value <= max
}

function setForm(policy: CodexRequestStrategyPolicy) {
  form.value = {
    ...form.value,
    ...policy,
    strategy: policy.strategy ?? 'native',
    scope: policy.scope ?? 'dedicated',
    failure_mode: policy.failure_mode ?? 'fallback',
    probe_timeout_seconds: policy.probe_timeout_seconds ?? 10,
    cookie_mode: policy.cookie_mode ?? 'preserve',
    route_affinity_mode: policy.route_affinity_mode ?? 'off',
    route_prewarm_connections: policy.route_prewarm_connections ?? 4,
    route_failure_cooldown_seconds: policy.route_failure_cooldown_seconds ?? 120,
    region_mode: policy.region_mode ?? 'strip',
    account_routing_override: policy.account_routing_override ?? '',
    residency: policy.residency ?? '',
    time_context_mode: policy.time_context_mode ?? 'strip',
    timezone: policy.timezone ?? '',
    compliance_mode: policy.compliance_mode ?? 'account',
  }
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const [policy, settings] = await Promise.all([getCodexRequestStrategyPolicy(), getSettings()])
    setForm(policy)
    timezone.value = settings.openai_request_timezone ?? ''
  } catch (error) {
    loadError.value = extractApiErrorMessage(error, t('codexRequestStrategy.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function saveTimezone() {
  if (timezoneSaving.value) return
  timezoneSaving.value = true
  timezoneSaved.value = false
  timezoneSaveError.value = ''
  try {
    const settings = await updateSettings({ openai_request_timezone: timezone.value.trim() })
    timezone.value = settings.openai_request_timezone ?? ''
    timezoneSaved.value = true
  } catch (error) {
    timezoneSaveError.value = extractApiErrorMessage(error, t('codexRequestStrategy.timezoneSaveFailed'))
  } finally {
    timezoneSaving.value = false
  }
}

async function save() {
  if (saving.value || validationError.value) return
  saving.value = true
  saved.value = false
  saveError.value = ''
  try {
    setForm(await saveCodexRequestStrategyPolicy({ ...form.value }))
    saved.value = true
  } catch (error) {
    saveError.value = extractApiErrorMessage(error, t('codexRequestStrategy.saveFailed'))
  } finally {
    saving.value = false
  }
}

watch(form, () => { saved.value = false }, { deep: true, flush: 'sync' })
watch(timezone, () => { if (!timezoneSaving.value) timezoneSaved.value = false })
onMounted(load)
</script>
