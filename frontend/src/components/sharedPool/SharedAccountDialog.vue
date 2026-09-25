<template>
  <BaseDialog :show="show" :title="t(account ? 'sharedPool.edit' : 'sharedPool.create')" :width="account ? 'normal' : 'wide'" :close-on-escape="!saving" :show-close-button="!saving" @close="close">
    <form id="shared-account-form" @submit.prevent="submit">
      <fieldset :disabled="saving" class="min-w-0 space-y-5">
        <div v-if="account" class="flex min-w-0 items-center gap-3 border-b border-gray-200 pb-4 dark:border-dark-700">
          <PlatformIcon :platform="account.platform" size="md" />
          <div class="min-w-0"><p class="break-words font-semibold text-gray-900 dark:text-white">{{ account.name }}</p><p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ platformNames[account.platform] }}</p></div>
        </div>
        <p v-if="!account && !platforms.length" class="text-sm text-amber-600">{{ t('sharedPool.noPlatforms') }}</p>
        <div>
          <label for="shared-name" class="input-label">{{ t('admin.accounts.accountName') }}</label>
          <input id="shared-name" v-model="form.name" required maxlength="100" class="input" :placeholder="t('admin.accounts.enterAccountName')" />
        </div>
        <div v-if="!account">
          <label id="shared-platform-label" class="input-label">{{ t('admin.accounts.platform') }}</label>
          <div class="mt-2 flex flex-wrap rounded-lg bg-gray-100 p-1 dark:bg-dark-700" role="group" aria-labelledby="shared-platform-label">
            <button v-for="platform in platforms" :key="platform" type="button" :data-platform="platform" :disabled="!!account" :aria-pressed="form.platform === platform"
              class="flex flex-1 items-center justify-center gap-2 rounded-md px-4 py-2.5 text-sm font-medium transition-all disabled:cursor-default"
              :class="form.platform === platform ? `bg-white shadow-sm dark:bg-dark-600 ${platformColors[platform]}` : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-200'"
              @click="form.platform = platform">
              <PlatformIcon :platform="platform" size="sm" />{{ platformNames[platform] }}
            </button>
          </div>
        </div>
        <div v-if="!account">
          <label id="shared-type-label" class="input-label">{{ t('admin.accounts.accountType') }}</label>
          <div class="mt-2 grid grid-cols-1 gap-3 sm:grid-cols-2" role="group" aria-labelledby="shared-type-label">
            <button v-for="type in accountTypes" :key="type" type="button" :data-account-type="type" :disabled="!!account" :aria-pressed="form.type === type"
              class="flex items-center gap-3 rounded-lg border-2 p-3 text-left transition-all disabled:cursor-default"
              :class="form.type === type ? 'border-primary-500 bg-primary-50 dark:bg-primary-900/20' : 'border-gray-200 hover:border-primary-300 dark:border-dark-600 dark:hover:border-primary-700'"
              @click="form.type = type">
              <span class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg" :class="form.type === type ? 'bg-primary-500 text-white' : 'bg-gray-100 text-gray-500 dark:bg-dark-600 dark:text-gray-400'">
                <Icon :name="type === 'oauth' ? 'sparkles' : 'key'" size="sm" />
              </span>
              <span><span class="block text-sm font-medium text-gray-900 dark:text-white">{{ t(`sharedPool.${type}`) }}</span><span class="text-xs text-gray-500 dark:text-gray-400">{{ t(type === 'oauth' ? 'sharedPool.oauthDescription' : 'sharedPool.apiKeyDescription') }}</span></span>
            </button>
          </div>
        </div>
        <div>
          <label for="shared-concurrency" class="input-label">{{ t('sharedPool.concurrency') }}</label>
          <input id="shared-concurrency" v-model.number="form.concurrency" required type="number" min="1" :max="config.max_concurrency" step="1" class="input" />
        </div>
        <div class="space-y-2">
          <label for="shared-proxy" class="input-label">{{ t('sharedPool.proxy') }}</label>
          <label v-if="account" class="flex items-center gap-2 text-sm"><input v-model="changeProxy" type="checkbox" />{{ t('sharedPool.replaceProxy') }}</label>
          <input v-if="!account || changeProxy" id="shared-proxy" v-model="form.proxy_url" type="password" autocomplete="new-password" class="input" :placeholder="t('sharedPool.proxyPlaceholder')" />
          <p v-else class="text-sm text-gray-500">{{ t('sharedPool.keepProxy') }} · {{ t(account.proxy_mode === 'random' ? 'sharedPool.randomProxy' : 'sharedPool.customProxy') }}</p>
          <p class="input-hint">{{ t('sharedPool.proxyHint', { rate: config.proxy_rate_bps / 100 }) }}</p>
        </div>
        <SharedRevenueSplit :config="config" :use-random-proxy="account && !changeProxy ? account.proxy_mode === 'random' : !form.proxy_url?.trim()" inline />
        <SharedSettlementNotice v-if="!account" :config="config" :platform="form.platform" />
        <p v-if="!account" class="input-hint">{{ t('sharedPool.autoDispatchHint') }}</p>
        <div class="space-y-2">
          <DailyCooldownSettings v-model="dailyCooldown" :disabled="saving" @update:model-value="dailyCooldownChanged = true" />
          <p class="input-hint">{{ t('sharedPool.dailyCooldownHint') }}</p>
        </div>
        <div v-if="!account">
          <label class="flex cursor-pointer items-start gap-3 rounded-lg border border-gray-200 p-4 dark:border-dark-600">
            <input v-model="form.protection_enabled" type="checkbox" class="mt-1 h-4 w-4 rounded text-primary-600" />
            <span><span class="block text-sm font-medium">{{ t('sharedPool.protection') }}</span><span class="mt-1 block text-xs text-gray-500 dark:text-dark-400">{{ t('sharedPool.protectionHint') }}</span></span>
          </label>
        </div>
        <div v-if="!account && supportsCodexTicket && !excelBPSEnabled" class="flex items-start justify-between gap-4">
          <div><label for="shared-codex-ticket" class="input-label">{{ t('sharedPool.codexTicket') }}</label><p class="input-hint">{{ t(codexTicketRequired ? 'sharedPool.codexTicketRequiredHint' : 'sharedPool.codexTicketHint') }}</p></div>
          <Toggle id="shared-codex-ticket" :model-value="codexTicketRequired || codexTicketEnabled" :aria-label="t('sharedPool.codexTicket')" :disabled="saving || codexTicketRequired" @update:model-value="!codexTicketRequired && (codexTicketEnabled = $event)" />
        </div>
        <div v-if="!account && supportsCodexTicket" class="flex items-start justify-between gap-4">
          <div><label for="shared-excel-bps" class="input-label">{{ t('sharedPool.excelBPS') }}</label><p class="input-hint">{{ t('sharedPool.excelBPSHint') }}</p></div>
          <Toggle id="shared-excel-bps" v-model="excelBPSEnabled" :aria-label="t('sharedPool.excelBPS')" :disabled="saving" />
        </div>
        <SharedCredentialsForm v-if="!account" :platform="form.platform" :type="form.type" :proxy-url="form.proxy_url" :editing="false" :disabled="saving" @change="credentials = $event" @valid="credentialsValid = $event" @busy="authorizing = $event" />
        <p v-if="error" role="alert" class="break-words text-sm text-red-600 dark:text-red-400">{{ error }}</p>
      </fieldset>
    </form>
    <template #footer>
      <div class="flex w-full flex-wrap items-center justify-end gap-3">
        <button v-if="!account" type="button" class="btn btn-secondary mr-auto" :disabled="saving || authorizing || !platforms.length" @click="openImport"><Icon name="upload" size="sm" class="mr-2" />{{ t('sharedPool.importAccounts') }}</button>
        <button type="button" class="btn btn-secondary" :disabled="saving" @click="close">{{ t('common.cancel') }}</button>
        <button type="submit" form="shared-account-form" class="btn btn-primary" :disabled="saving || authorizing || !platforms.length">{{ saving ? t('common.saving') : t('common.save') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Toggle from '@/components/common/Toggle.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import SharedCredentialsForm from './SharedCredentialsForm.vue'
import SharedRevenueSplit from './SharedRevenueSplit.vue'
import SharedSettlementNotice from './SharedSettlementNotice.vue'
import { hasSettlementPolicy } from './settlementPolicy'
import { normalizePlanType } from '@/utils/planType'
import DailyCooldownSettings from '@/components/account/DailyCooldownSettings.vue'
import { dailyCooldownValidationError, normalizeDailyCooldown, withDailyCooldownExtra } from '@/utils/dailyCooldown'
import { sharedPoolAPI, type SharedAccount, type SharedAccountInput, type SharedAccountUpdateInput, type SharedConfig, type SharedImportDefaults, type SharedPlatform } from '@/api/sharedPool'

const props = defineProps<{ show: boolean; account?: SharedAccount | null; config: SharedConfig }>()
const emit = defineEmits<{ close: []; saved: []; import: [defaults: SharedImportDefaults] }>()
const { t } = useI18n()
const platforms = computed(() => Array.from(new Set([...props.config.platforms, ...(props.account ? [props.account.platform] : [])])))
const platformNames: Record<SharedPlatform, string> = { anthropic: 'Anthropic', openai: 'OpenAI', gemini: 'Gemini', antigravity: 'Antigravity' }
const platformColors: Record<SharedPlatform, string> = {
  anthropic: 'text-orange-600 dark:text-orange-400', openai: 'text-green-600 dark:text-green-400',
  gemini: 'text-blue-600 dark:text-blue-400', antigravity: 'text-purple-600 dark:text-purple-400'
}
const form = reactive<SharedAccountInput>({
  name: props.account?.name || '', platform: props.account?.platform || props.config.platforms[0] || 'openai',
  type: props.account?.type || 'oauth', concurrency: props.account?.concurrency || 1, proxy_url: '',
  enabled: props.account?.enabled ?? true, protection_enabled: props.account?.protection_enabled ?? true
})
const accountTypes = computed<SharedAccountInput['type'][]>(() => form.platform === 'antigravity' ? ['oauth'] : ['oauth', 'apikey'])
const supportsCodexTicket = computed(() => form.platform === 'openai' && form.type === 'oauth')
const canConsent = computed(() => hasSettlementPolicy(props.config))
const codexTicketEnabled = ref(true)
const excelBPSEnabled = ref(false)
const dailyCooldown = ref(normalizeDailyCooldown(props.account?.daily_cooldown))
const dailyCooldownChanged = ref(false)
function cooldownInput() {
  return dailyCooldownChanged.value || props.account?.daily_cooldown !== undefined
    ? { daily_cooldown: withDailyCooldownExtra(undefined, dailyCooldown.value).daily_cooldown } : {}
}
watch(() => form.platform, () => { if (!accountTypes.value.includes(form.type)) form.type = 'oauth' })
const changeProxy = ref(false)
const credentials = ref<Record<string, unknown>>()
const codexTicketRequired = computed(() => supportsCodexTicket.value && typeof credentials.value?.plan_type === 'string'
  && ['pro', 'chatgptpro', 'prolite'].includes(normalizePlanType(credentials.value.plan_type)))
const credentialsValid = ref(true)
const authorizing = ref(false)
const saving = ref(false)
const error = ref('')
function close() { if (!saving.value) emit('close') }
function validateCooldown() {
  const validationError = dailyCooldownValidationError(dailyCooldown.value)
  if (validationError) error.value = t(validationError)
  return !validationError
}
function openImport() {
  if (saving.value || authorizing.value) return
  error.value = ''
  if (validateCooldown()) emit('import', { ...form, enabled: true, dispatch_consent: true, codex_ticket_enabled: codexTicketRequired.value || codexTicketEnabled.value, excel_bps_enabled: excelBPSEnabled.value, ...cooldownInput() })
}
function submit() {
  if (saving.value || authorizing.value) return
  error.value = ''
  if (props.account && !form.name.trim()) { error.value = t('admin.accounts.enterAccountName'); return }
  if (!validateCooldown()) return
  if (!props.account && !canConsent.value) { error.value = t('sharedPool.settlementRequired'); return }
  if (!props.account && (!credentialsValid.value || !credentials.value)) { error.value = t('sharedPool.credentialsRequired'); return }
  void save()
}
async function save() {
  if (saving.value) return
  saving.value = true; error.value = ''
  try {
    if (props.account) {
      const input: SharedAccountUpdateInput = {
        name: form.name.trim(), platform: props.account.platform, type: props.account.type,
        enabled: props.account.enabled, protection_enabled: props.account.protection_enabled,
        concurrency: form.concurrency, ...(changeProxy.value ? { proxy_url: form.proxy_url } : {}), ...cooldownInput()
      }
      await sharedPoolAPI.update(props.account.id, input)
    } else {
      await sharedPoolAPI.create({ ...form, enabled: true, dispatch_consent: true, ...cooldownInput(), name: form.name.trim(), credentials: credentials.value, confirm_disable: false,
        ...(supportsCodexTicket.value ? { codex_ticket_enabled: codexTicketRequired.value || codexTicketEnabled.value, excel_bps_enabled: excelBPSEnabled.value } : {}) })
    }
    emit('saved')
  } catch (e: unknown) { error.value = (e as Error).message || t('sharedPool.actionFailed') }
  finally { saving.value = false }
}
</script>
