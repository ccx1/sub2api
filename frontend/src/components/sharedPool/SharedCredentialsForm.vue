<template>
  <div class="space-y-4">
    <div v-if="type === 'apikey'">
      <label class="input-label" for="shared-api-key">{{ t('sharedPool.apiKey') }}</label>
      <input id="shared-api-key" v-model="apiKey" :disabled="disabled" type="password" class="input" autocomplete="new-password" :placeholder="editing ? t('sharedPool.apiKeyHint') : ''" @input="updateKey" />
    </div>
    <template v-else>
      <div>
        <label id="shared-authorization-label" class="input-label">{{ t('admin.accounts.oauth.authMethod') }}</label>
        <div class="mt-2 flex flex-wrap gap-4" role="group" aria-labelledby="shared-authorization-label">
          <label class="flex cursor-pointer items-center gap-2 text-sm"><input v-model="mode" type="radio" value="browser" :disabled="disabled" class="text-primary-600" />{{ t('sharedPool.browserAuthorization') }}</label>
          <label class="flex cursor-pointer items-center gap-2 text-sm"><input v-model="mode" type="radio" value="json" :disabled="disabled" class="text-primary-600" />{{ t('sharedPool.importCredentials') }}</label>
        </div>
      </div>
      <template v-if="mode === 'browser'">
        <fieldset v-if="!authorized" :disabled="busy || disabled" class="shared-oauth min-w-0">
          <OAuthAuthorizationFlow ref="oauthFlow" add-method="oauth" :platform="platform" :auth-url="authorizationURL" :session-id="sessionID" :loading="busy" :error="error"
            :show-cookie-option="false" :show-refresh-token-option="false" :show-mobile-refresh-token-option="false" :show-session-token-option="false" :show-access-token-option="false"
            :show-codex-session-import-option="false" :show-agent-identity-option="false" :show-codex-pat-option="false" :show-sso-option="false" :show-email-password-option="false"
            :show-manual-option="true" :show-project-id="false" :show-help="false" :show-proxy-warning="!!proxyUrl" @generate-url="start" />
        </fieldset>
        <div v-if="authorized" class="flex flex-wrap items-center gap-3 rounded-lg border border-emerald-200 bg-emerald-50 p-4 dark:border-emerald-800 dark:bg-emerald-900/20">
          <p class="flex-1 text-sm text-emerald-700 dark:text-emerald-300" role="status">{{ t('sharedPool.authorized') }}</p>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="disabled" @click="restart">{{ t('sharedPool.reauthorize') }}</button>
        </div>
        <div v-else class="flex flex-wrap items-center justify-end gap-3">
          <a v-if="authorizationURL" :href="authorizationURL" target="_blank" rel="noopener noreferrer" class="text-sm text-primary-600 dark:text-primary-400">{{ t('sharedPool.openAuthorization') }}</a>
          <button type="button" data-action="finish-authorization" class="btn btn-primary" :disabled="!canFinish" @click="finish">{{ busy ? t('common.loading') : t('sharedPool.finishAuthorization') }}</button>
        </div>
      </template>
      <template v-else>
        <label for="shared-credentials" class="input-label">{{ t('sharedPool.credentials') }}</label>
        <textarea id="shared-credentials" v-model="json" :disabled="disabled" rows="5" spellcheck="false" autocomplete="off" class="input font-mono text-xs" @input="updateJSON" />
        <p class="input-hint">{{ t('sharedPool.credentialsHint') }}</p>
        <p v-if="error" class="break-words text-sm text-red-600 dark:text-red-400" role="alert">{{ error }}</p>
      </template>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import OAuthAuthorizationFlow from '@/components/account/OAuthAuthorizationFlow.vue'
import { sharedPoolAPI, type SharedPlatform } from '@/api/sharedPool'

const props = defineProps<{ platform: SharedPlatform; type: 'oauth' | 'apikey'; proxyUrl?: string; accountId?: number; editing: boolean; disabled?: boolean }>()
const emit = defineEmits<{ change: [credentials: Record<string, unknown> | undefined]; valid: [valid: boolean]; busy: [busy: boolean] }>()
const { t } = useI18n()
const oauthFlow = ref<InstanceType<typeof OAuthAuthorizationFlow> | null>(null)
const mode = ref<'browser' | 'json'>('browser')
const apiKey = ref('')
const json = ref('')
const sessionID = ref('')
const authorizationURL = ref('')
const authorized = ref(false)
const busy = ref(false)
const error = ref('')
const canFinish = computed(() => !props.disabled && !busy.value && !!sessionID.value && !!oauthFlow.value?.authCode.trim())
let generation = 0

function reset() {
  generation++
  apiKey.value = ''; json.value = ''; sessionID.value = ''; authorizationURL.value = ''
  authorized.value = false; error.value = ''; busy.value = false
  oauthFlow.value?.reset()
  emit('change', undefined); emit('valid', true)
}
watch(() => [props.platform, props.type, props.proxyUrl, props.accountId, mode.value], reset, { flush: 'sync' })
watch(busy, value => emit('busy', value), { flush: 'sync' })
function updateKey() { emit('change', apiKey.value.trim() ? { api_key: apiKey.value.trim() } : undefined) }
function updateJSON() {
  error.value = ''
  if (!json.value.trim()) { emit('change', undefined); emit('valid', true); return }
  try {
    const value = JSON.parse(json.value)
    if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error()
    emit('change', value); emit('valid', true)
  } catch { error.value = t('sharedPool.invalidCredentials'); emit('change', undefined); emit('valid', false) }
}
async function restart() { reset(); await nextTick(); await start() }
async function start() {
  if (busy.value || props.disabled) return
  reset()
  const current = generation
  busy.value = true
  emit('valid', false)
  try {
    const result = await sharedPoolAPI.oauthStart(props.platform, props.proxyUrl, props.accountId)
    if (generation !== current) return
    const url = new URL(result.auth_url)
    if (url.protocol !== 'https:' && url.protocol !== 'http:') throw new Error(t('sharedPool.actionFailed'))
    authorizationURL.value = url.href; sessionID.value = result.session_id
  } catch (e: unknown) { if (generation === current) error.value = (e as Error).message || t('sharedPool.actionFailed') }
  finally { if (generation === current) busy.value = false }
}
function authorizationInput() {
  let code = oauthFlow.value?.authCode.trim() || ''
  let state = oauthFlow.value?.oauthState || new URL(authorizationURL.value).searchParams.get('state') || undefined
  if (/^https?:\/\//i.test(code)) {
    const callback = new URL(code)
    code = callback.searchParams.get('code') || ''
    state = callback.searchParams.get('state') || undefined
  }
  if (!code) throw new Error(t('sharedPool.invalidCode'))
  return { session_id: sessionID.value, code, state }
}
async function finish() {
  if (!canFinish.value) return
  const current = generation
  busy.value = true; error.value = ''
  try {
    const result = await sharedPoolAPI.oauthFinish(props.platform, authorizationInput())
    if (current !== generation) return
    emit('change', result.credentials); emit('valid', true)
    authorized.value = true; sessionID.value = ''; authorizationURL.value = ''
    oauthFlow.value?.reset()
  } catch (e: unknown) {
    if (current === generation) {
      error.value = (e as Error).message || t('sharedPool.actionFailed')
      sessionID.value = ''
    }
  } finally { if (current === generation) busy.value = false }
}
onBeforeUnmount(() => { generation++; apiKey.value = ''; json.value = ''; oauthFlow.value?.reset(); emit('busy', false) })
</script>

<style scoped>
.shared-oauth :deep(.flex-1) { min-width: 0; }
.shared-oauth :deep(input[readonly]) { min-width: 0; }
</style>
