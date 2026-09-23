<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-6">
      <header class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('codexTicketSettings.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.description') }}</p>
        </div>
        <RouterLink to="/admin/settings#gateway" class="btn btn-secondary">{{ t('codexTicketSettings.proxySettings') }}</RouterLink>
      </header>
      <p v-if="loading" role="status" class="py-12 text-center text-sm text-gray-500">{{ t('codexTicketSettings.loading') }}</p>
      <div v-else-if="loadError" role="alert" class="card flex flex-wrap items-center justify-between gap-3 p-5">
        <p class="text-sm text-red-600 dark:text-red-400">{{ loadError }}</p>
        <button type="button" class="btn btn-secondary" data-testid="load-retry" @click="load">{{ t('codexTicketSettings.reload') }}</button>
      </div>
      <form v-else-if="form" class="space-y-6" novalidate @submit.prevent="save">
        <fieldset :disabled="saving" class="min-w-0 space-y-6">
          <section class="card space-y-4 p-5" aria-labelledby="ticket-policy-title">
            <h2 id="ticket-policy-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('codexTicketSettings.policy') }}</h2>
            <label class="flex items-start gap-3">
              <input v-model="form.enabled" type="checkbox" data-testid="enabled" class="mt-1 h-4 w-4" />
              <span class="text-sm text-gray-900 dark:text-white">{{ t('codexTicketSettings.enabled') }}<span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.enabledHint') }}</span></span>
            </label>
            <label class="block space-y-1">
              <span class="input-label">{{ t('codexTicketSettings.credentialMode') }}</span>
              <select v-model="form.credential_mode" data-testid="credential-mode" class="input w-full">
                <option value="state">{{ t('codexTicketSettings.credentialState') }}</option>
                <option value="cookie_state">{{ t('codexTicketSettings.credentialCookieState') }}</option>
                <option value="cookie">{{ t('codexTicketSettings.credentialCookie') }}</option>
              </select>
              <span class="block input-hint">{{ t('codexTicketSettings.credentialModeHint') }}</span>
            </label>
            <label class="flex items-start gap-3">
              <input v-model="form.fail_closed" type="checkbox" data-testid="fail-closed" :disabled="saving" class="mt-1 h-4 w-4" />
              <span class="text-sm text-gray-900 dark:text-white">{{ t('codexTicketSettings.failClosed') }}<span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.failClosedHint') }}</span></span>
            </label>
            <label for="ticket-session-mode" class="block space-y-1">
              <span class="input-label">{{ t('codexTicketSettings.sessionMode') }}</span>
              <select id="ticket-session-mode" v-model="form.session_mode" data-testid="session-mode" aria-describedby="ticket-session-mode-hint" class="input w-full">
                <option value="random">{{ t('codexTicketSettings.sessionModeRandom') }}</option>
                <option value="account">{{ t('codexTicketSettings.sessionModeAccount') }}</option>
                <option value="account_model">{{ t('codexTicketSettings.sessionModeAccountModel') }}</option>
              </select>
              <span id="ticket-session-mode-hint" class="block text-xs text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.sessionModeHint') }}</span>
            </label>
            <label for="ticket-refresh-strategy" class="block space-y-1">
              <span class="input-label">{{ t('codexTicketSettings.refreshStrategy') }}</span>
              <select id="ticket-refresh-strategy" v-model="form.refresh_strategy" data-testid="refresh-strategy" aria-describedby="ticket-refresh-strategy-hint" class="input w-full" :disabled="saving">
                <option value="replace">{{ t('codexTicketSettings.refreshStrategyReplace') }}</option>
                <option value="revalidate">{{ t('codexTicketSettings.refreshStrategyRevalidate') }}</option>
              </select>
              <span id="ticket-refresh-strategy-hint" data-testid="refresh-strategy-hint" class="block text-xs text-gray-500 dark:text-gray-400">{{ t(form.refresh_strategy === 'replace' ? 'codexTicketSettings.refreshStrategyReplaceHint' : 'codexTicketSettings.refreshStrategyRevalidateHint') }}</span>
              <span class="block text-xs text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.refreshStrategyManualHint') }}</span>
            </label>
            <label class="flex items-start gap-3">
              <input v-model="form.verify_business" type="checkbox" data-testid="verify-business" aria-describedby="ticket-verify-business-hint" class="mt-1 h-4 w-4" />
              <span class="text-sm text-gray-900 dark:text-white">{{ t('codexTicketSettings.verifyBusiness') }}<span id="ticket-verify-business-hint" class="mt-1 block text-xs text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.verifyBusinessHint') }}</span></span>
            </label>
            <label for="ticket-business-verification-rounds" class="block space-y-1">
              <span class="input-label">{{ t('codexTicketSettings.businessVerificationRounds') }}</span>
              <input id="ticket-business-verification-rounds" v-model.number="form.business_verification_rounds" data-testid="business-verification-rounds" aria-describedby="ticket-business-verification-rounds-hint" type="number" min="1" max="10" step="1" class="input w-full" :disabled="saving || !form.verify_business" />
              <span id="ticket-business-verification-rounds-hint" class="block text-xs text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.businessVerificationRoundsHint') }}</span>
            </label>
            <div class="space-y-1">
              <CodexTicketTagSelect v-model="form.models" data-testid="models" :options="modelOptions" :disabled="saving" :max="32" :label="t('codexTicketSettings.models')" :placeholder="t('codexTicketSettings.selectModels')" />
              <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.modelsHint') }}</p>
            </div>
            <label class="block space-y-1">
              <span class="input-label">{{ t('codexTicketSettings.lengthMode') }}</span>
              <select v-model="form.length_mode" data-testid="length-mode" class="input w-full" :disabled="saving || usesCookie">
                <option value="auto">{{ t('codexTicketSettings.lengthModeAuto') }}</option>
                <option value="strict">{{ t('codexTicketSettings.lengthModeStrict') }}</option>
              </select>
            </label>
            <p data-testid="length-mode-hint" class="text-sm text-gray-500 dark:text-gray-400">{{ t(usesCookie ? 'codexTicketSettings.cookieLengthHint' : isAutoLength ? 'codexTicketSettings.lengthModeAutoHint' : 'codexTicketSettings.lengthModeStrictHint') }}</p>
          </section>
          <section class="card space-y-4 p-5" aria-labelledby="ticket-cookie-title">
            <h2 id="ticket-cookie-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('codexTicketSettings.cookieTitle') }}</h2>
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.cookieTimingHint') }}</p>
            <div class="grid gap-4 sm:grid-cols-2">
              <label v-for="field in ticketCookieFields" :key="field.key" class="min-w-0 space-y-1">
                <span class="input-label">{{ numericFieldLabel(field.key) }}</span>
                <input v-model.number="form[field.key]" :data-testid="field.key" type="number" :min="field.min" :max="field.max" step="1" class="input w-full" />
                <span class="block text-xs text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.range', { min: field.min, max: field.max }) }}</span>
              </label>
            </div>
          </section>
          <fieldset :disabled="saving || isAutoLength || usesCookie" data-testid="length-rules" class="card min-w-0 space-y-4 p-5" :class="(isAutoLength || usesCookie) && 'opacity-60'" aria-labelledby="ticket-tier-title">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <h2 id="ticket-tier-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('codexTicketSettings.tiers') }}</h2>
              <button type="button" class="btn btn-secondary" data-testid="add-tier" :disabled="saving || isAutoLength || usesCookie || rules.length >= 32" @click="addRule">{{ t('codexTicketSettings.addTier') }}</button>
            </div>
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.tiersHint') }}</p>
            <div v-for="(rule, index) in rules" :key="rule.id" data-testid="tier-rule" class="grid min-w-0 gap-3 border-b border-gray-100 pb-4 dark:border-dark-700 sm:grid-cols-12 sm:items-end">
              <CodexTicketTagSelect v-model="rule.tiers" :data-testid="`tier-${index}`" class="sm:col-span-8" :options="ticketTierOptions(rule, rules, tierSelections.bindings, tierOptions)" :disabled="saving || isAutoLength || usesCookie" :max="544" :label="t('codexTicketSettings.tier')" :placeholder="t('codexTicketSettings.selectTiers')" />
              <label class="min-w-0 space-y-1 sm:col-span-2">
                <span class="input-label">{{ t('codexTicketSettings.length') }}</span>
                <input v-model.number="rule.target_length" :data-testid="`length-${index}`" type="number" min="16" max="8192" step="1" class="input w-full" />
              </label>
              <button type="button" class="btn btn-secondary sm:col-span-2" :data-testid="`remove-tier-${index}`" :aria-label="t('codexTicketSettings.removeTier', { tier: index + 1 })" @click="rules.splice(index, 1)">{{ t('codexTicketSettings.remove') }}</button>
            </div>
            <p v-if="!rules.length" class="text-sm text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.noTiers') }}</p>
            <div class="grid gap-4 sm:grid-cols-2">
              <label class="space-y-1">
                <span class="input-label">{{ t('codexTicketSettings.fields.target_length') }}</span>
                <input v-model.number="form.target_length" data-testid="target_length" type="number" min="16" max="8192" step="1" class="input w-full" />
                <span class="block text-xs text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.fallbackHint') }}</span>
              </label>
              <label class="space-y-1">
                <span class="input-label">{{ t('codexTicketSettings.rejectedLengths') }}</span>
                <input v-model="rejectedText" data-testid="rejected-lengths" class="input w-full font-mono" />
                <span class="block text-xs text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.rejectedHint') }}</span>
              </label>
            </div>
          </fieldset>
          <section class="card space-y-4 p-5" aria-labelledby="ticket-protection-title">
            <h2 id="ticket-protection-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('codexTicketSettings.protectionTitle') }}</h2>
            <label class="flex items-start gap-3">
              <input v-model="protection.enabled" type="checkbox" data-testid="protection-enabled" class="mt-1 h-4 w-4" />
              <span class="text-sm text-gray-900 dark:text-white">{{ t('codexTicketSettings.protectionEnabled') }}</span>
            </label>
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.protectionHint') }}</p>
            <p v-if="isAutoLength && protection.enabled && !usesCookie" role="status" data-testid="protection-auto-warning" class="text-sm text-amber-700 dark:text-amber-300">{{ t('codexTicketSettings.protectionAutoHint') }}</p>
            <p v-if="usesCookie" class="text-sm text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.cookieProtectionHint') }}</p>
            <label class="block space-y-1">
              <span class="input-label">{{ t('codexTicketSettings.protectionLengths') }}</span>
              <input v-model="protectionLengthsText" data-testid="protection-lengths" :disabled="saving || usesCookie" class="input w-full font-mono" />
            </label>
            <div class="grid gap-4 sm:grid-cols-2">
              <label v-for="field in ticketProtectionFields" :key="field.key" class="min-w-0 space-y-1">
                <span class="input-label">{{ t(`codexTicketSettings.fields.${field.key}`) }}</span>
                <input v-model.number="protection[field.key]" :data-testid="field.key" type="number" :min="field.min" :max="field.max" step="1" class="input w-full" />
              </label>
            </div>
          </section>
          <section class="card space-y-4 p-5" data-testid="rejection-retry-settings" aria-labelledby="ticket-rejection-retry-title">
            <h2 id="ticket-rejection-retry-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('codexTicketSettings.rejectionRetryTitle') }}</h2>
            <p id="ticket-rejection-retry-hint" class="text-sm text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.rejectionRetryHint') }}</p>
            <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              <label v-for="field in ticketRejectionRetryFields" :key="field.key" class="min-w-0 space-y-1">
                <span class="input-label">{{ t(`codexTicketSettings.fields.${field.key}`) }}</span>
                <input v-model.number="protection[field.key]" :data-testid="field.key" type="number" :min="field.min" :max="field.max" step="1" class="input w-full" aria-describedby="ticket-rejection-retry-hint" />
                <span class="block text-xs text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.range', { min: field.min, max: field.max }) }}</span>
              </label>
            </div>
          </section>
          <section v-for="group in ticketNumericGroups" :key="group.key" class="card space-y-4 p-5" :aria-labelledby="`ticket-${group.key}-title`">
            <div>
              <h2 :id="`ticket-${group.key}-title`" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t(`codexTicketSettings.${group.key}`) }}</h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t(`codexTicketSettings.${group.key}Hint`) }}</p>
            </div>
            <label v-if="group.key === 'retry'" class="block space-y-1">
              <span class="input-label">{{ t('codexTicketSettings.backoff') }}</span>
              <input v-model="backoffText" data-testid="backoff" class="input w-full font-mono" />
              <span class="block text-xs text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.backoffHint') }}</span>
            </label>
            <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              <label v-for="field in group.fields" :key="field.key" class="min-w-0 space-y-1">
                <span class="input-label">{{ numericFieldLabel(field.key) }}</span>
                <input v-model.number="form[field.key]" :data-testid="field.key" type="number" :min="field.min" :max="field.max" step="1" class="input w-full" :aria-describedby="field.key === 'pool_capacity' ? 'ticket-pool-capacity-hint' : undefined" />
                <span class="block text-xs text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.range', { min: field.min, max: field.max }) }}</span>
                <span v-if="field.key === 'pool_capacity'" id="ticket-pool-capacity-hint" class="block text-xs text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.poolCapacityHint') }}</span>
                <span v-if="field.key === 'proxy_failure_threshold'" class="block text-xs text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.proxyFailureThresholdHint') }}</span>
              </label>
            </div>
            <label v-if="group.key === 'retry'" class="flex items-start gap-3">
              <input v-model="form.respect_retry_after" type="checkbox" class="mt-1 h-4 w-4" />
              <span class="text-sm text-gray-900 dark:text-white">{{ t('codexTicketSettings.retryAfter') }}<span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.retryAfterHint') }}</span></span>
            </label>
          </section>
        </fieldset>
        <footer class="space-y-3">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.applyHint') }}</p>
          <p v-if="validationError" role="alert" data-testid="validation-error" class="text-sm text-amber-700 dark:text-amber-300">{{ validationError }}</p>
          <p v-if="saveError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ saveError }}</p>
          <p v-if="saved" role="status" class="text-sm text-emerald-700 dark:text-emerald-300">{{ t('codexTicketSettings.saved') }}</p>
          <div class="flex justify-end"><button type="submit" class="btn btn-primary" data-testid="save-settings" :disabled="saving || !!validationError">{{ t(saving ? 'codexTicketSettings.saving' : 'codexTicketSettings.save') }}</button></div>
        </footer>
      </form>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import CodexTicketTagSelect from '@/components/admin/CodexTicketTagSelect.vue'
import { getCodexTicketSettings, saveCodexTicketSettings, type CodexTicketSettings } from '@/api/admin/codexTicketSettings'
import { defaultTicketProtection, readTicketProtection, splitTicketList, ticketCookieFields, ticketNumericGroups, ticketProtectionFields, ticketRejectionRetryFields, validateTicketSettings } from '@/components/admin/codexTicketSettingsForm'
import { readTicketTierSelections, writeTicketTierSelections, ticketTierOptions, ticketModelOptions, type TicketTierRow } from '@/components/admin/codexTicketSelections'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const loading = ref(true)
const saving = ref(false)
const saved = ref(false)
const loadError = ref('')
const saveError = ref('')
const form = ref<CodexTicketSettings | null>(null)
const isAutoLength = computed(() => form.value?.length_mode === 'auto')
const usesCookie = computed(() => form.value?.credential_mode === 'cookie' || form.value?.credential_mode === 'cookie_state')
const backoffText = ref('')
const rejectedText = ref('')
const protection = ref(defaultTicketProtection())
const protectionLengthsText = ref('312')
const rules = ref<TicketTierRow[]>([])
const tierSelections = ref(readTicketTierSelections([]))
const savedModels = ref<string[]>([])
const knownModels = new Set(ticketModelOptions([]).map(option => option.value))
const modelOptions = computed(() => ticketModelOptions(savedModels.value).map(option => ({
  ...option, label: knownModels.has(option.value) ? option.label : t('codexTicketSettings.existingSelection', { value: option.label })
})))
const tierOptions = computed(() => tierSelections.value.options.map(option => ({
  ...option, label: option.value.startsWith('legacy:') ? t('codexTicketSettings.existingSelection', { value: option.label }) : option.label
})))
let ruleSequence = 0
const payload = computed<CodexTicketSettings | null>(() => form.value && ({
  ...form.value,
  models: [...form.value.models],
  retry_backoff_seconds: splitTicketList(backoffText.value).map(Number),
  rejected_lengths: splitTicketList(rejectedText.value).map(Number),
  protection: { ...protection.value, reject_and_silence_lengths: splitTicketList(protectionLengthsText.value).map(Number) },
  tier_rules: writeTicketTierSelections(rules.value, tierSelections.value.bindings)
}))
const validationError = computed(() => {
  if (!payload.value) return ''
  if (rules.value.some(rule => !rule.tiers.length)) return t('codexTicketSettings.errors.selectTier')
  const error = validateTicketSettings(payload.value)
  return error ? t(`codexTicketSettings.errors.${error.key}`, { ...error, field: error.field ? numericFieldLabel(error.field) : '' }) : ''
})

function numericFieldLabel(key: string) {
  const timingFields = ['ttl_seconds', 'refresh_before_seconds', 'cookie_ttl_seconds', 'cookie_refresh_before_seconds']
  const group = form.value?.refresh_strategy === 'replace' && timingFields.includes(key) ? 'updateFields' : 'fields'
  return t(`codexTicketSettings.${group}.${key}`)
}

function setForm(settings: CodexTicketSettings) {
  form.value = {
    ...settings,
    credential_mode: settings.credential_mode ?? 'state',
    cookie_ttl_seconds: settings.cookie_ttl_seconds ?? 20,
    cookie_refresh_before_seconds: settings.cookie_refresh_before_seconds ?? 5,
    refresh_strategy: settings.refresh_strategy === undefined ? 'revalidate' : settings.refresh_strategy,
    pool_capacity: settings.pool_capacity ?? 5,
    verify_business: settings.verify_business === undefined ? true : settings.verify_business,
    business_verification_rounds: settings.business_verification_rounds === undefined || settings.business_verification_rounds === 0 ? 1 : settings.business_verification_rounds,
    session_mode: settings.session_mode === undefined ? 'random' : settings.session_mode,
    proxy_failure_threshold: settings.proxy_failure_threshold === undefined || settings.proxy_failure_threshold === 0 ? 3 : settings.proxy_failure_threshold,
    length_mode: settings.length_mode === undefined ? 'strict' : settings.length_mode,
    models: [...settings.models]
  }
  savedModels.value = [...settings.models]
  backoffText.value = settings.retry_backoff_seconds.join(', ')
  rejectedText.value = settings.rejected_lengths.join(', ')
  protection.value = readTicketProtection(settings.protection)
  protectionLengthsText.value = protection.value.reject_and_silence_lengths.join(', ')
  tierSelections.value = readTicketTierSelections(settings.tier_rules)
  rules.value = tierSelections.value.rows.map(rule => ({ ...rule, id: ++ruleSequence }))
}
function addRule() {
  if (!saving.value && !isAutoLength.value && !usesCookie.value && rules.value.length < 32) rules.value.push({ id: ++ruleSequence, tiers: [], target_length: form.value?.target_length ?? 292 })
}
async function load() {
  loading.value = true
  loadError.value = ''
  try { setForm(await getCodexTicketSettings()) }
  catch (error) { loadError.value = extractApiErrorMessage(error, t('codexTicketSettings.loadFailed')) }
  finally { loading.value = false }
}
async function save() {
  if (saving.value || !payload.value || validationError.value) return
  saving.value = true
  saved.value = false
  saveError.value = ''
  try {
    setForm(await saveCodexTicketSettings(payload.value))
    saved.value = true
  } catch (error) { saveError.value = extractApiErrorMessage(error, t('codexTicketSettings.saveFailed')) }
  finally { saving.value = false }
}
watch(payload, () => { saved.value = false }, { flush: 'sync', deep: true })
onMounted(load)
</script>
