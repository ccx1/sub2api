<template>
  <BaseDialog :show="show" :title="t('sharedPool.importAccounts')" width="wide" :close-on-escape="!importing" :show-close-button="!importing" @close="close">
    <form id="shared-import-form" class="space-y-5" :aria-busy="importing" @submit.prevent="submit">
      <p class="text-sm text-gray-600 dark:text-dark-300">{{ t('sharedPool.importHint') }}</p>
      <p class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-xs text-amber-600 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-400">{{ t('sharedPool.importPolicy') }}</p>
      <fieldset :disabled="importing" class="min-w-0 space-y-5" @input="result = null">
        <div>
          <label class="input-label" for="shared-import-files">{{ t('admin.accounts.dataImportFile') }}</label>
          <div class="flex items-center justify-between gap-3 rounded-lg border border-dashed px-4 py-3 transition-colors"
            :class="dragDepth ? 'border-primary-400 bg-primary-50/70 dark:border-primary-500 dark:bg-primary-900/20' : 'border-gray-300 bg-gray-50 dark:border-dark-600 dark:bg-dark-800'"
            @dragenter.prevent="!importing && dragDepth++" @dragover.prevent @dragleave.prevent="dragDepth = Math.max(0, dragDepth - 1)" @drop.prevent="drop">
            <div class="min-w-0">
              <p class="truncate text-sm text-gray-700 dark:text-dark-200" :title="fileNames">{{ fileNames || t('admin.accounts.dataImportSelectFile') }}</p>
              <p class="text-xs text-gray-500 dark:text-dark-400">JSON (.json) · {{ t('sharedPool.importTooLarge') }}</p>
            </div>
            <button type="button" class="btn btn-secondary shrink-0" @click="fileInput?.click()">{{ t('common.chooseFile') }}</button>
          </div>
          <input id="shared-import-files" ref="fileInput" type="file" class="sr-only" accept="application/json,.json" multiple @change="fileChanged" />
          <button v-if="files.length" type="button" class="mt-2 text-sm text-primary-600 dark:text-primary-400" @click="files = []; result = null">{{ t('sharedPool.importClear') }}</button>
        </div>
        <div>
          <label class="input-label" for="shared-import-content">{{ t('admin.accounts.dataImportJsonText') }}</label>
          <textarea id="shared-import-content" v-model="content" rows="7" autocomplete="off" spellcheck="false" class="input font-mono text-xs" :placeholder="placeholder" />
        </div>
        <div class="space-y-4 border-t border-gray-200 pt-4 dark:border-dark-700">
          <h4 class="text-sm font-medium text-gray-900 dark:text-white">{{ t('sharedPool.importOptions') }}</h4>
          <div class="grid gap-4 sm:grid-cols-2">
            <div>
              <label for="shared-import-name" class="input-label">{{ t('sharedPool.importDefaultName') }}</label>
              <input id="shared-import-name" v-model="defaults.name" maxlength="100" class="input" />
              <p class="input-hint">{{ t('sharedPool.importDefaultNameHint') }}</p>
            </div>
            <div>
              <label for="shared-import-concurrency" class="input-label">{{ t('sharedPool.concurrency') }}</label>
              <input id="shared-import-concurrency" v-model.number="defaults.concurrency" required type="number" min="1" :max="config.max_concurrency" step="1" class="input" />
            </div>
          </div>
          <div>
            <label for="shared-import-proxy" class="input-label">{{ t('sharedPool.proxy') }}</label>
            <input id="shared-import-proxy" v-model="defaults.proxy_url" type="password" autocomplete="new-password" class="input" :placeholder="t('sharedPool.proxyPlaceholder')" />
            <p class="input-hint">{{ t('sharedPool.proxyHint', { rate: config.proxy_rate_bps / 100 }) }}</p>
          </div>
          <SharedRevenueSplit :config="config" :use-random-proxy="!defaults.proxy_url?.trim()" inline />
          <SharedSettlementNotice :config="config" :platform="defaults.platform" />
          <div class="space-y-2">
            <DailyCooldownSettings v-model="dailyCooldown" :disabled="importing" @update:model-value="dailyCooldownChanged = true; result = null" />
            <p class="input-hint">{{ t('sharedPool.dailyCooldownHint') }}</p>
          </div>
          <div class="space-y-4">
            <div><label class="flex items-center gap-2 text-sm font-medium"><input v-model="defaults.protection_enabled" type="checkbox" class="h-4 w-4 rounded" />{{ t('sharedPool.protection') }}</label><p class="input-hint">{{ t('sharedPool.protectionHint') }}</p></div>
            <div class="flex items-start justify-between gap-4">
              <div><label for="shared-import-codex-ticket" class="input-label">{{ t('sharedPool.codexTicket') }}</label><p class="input-hint">{{ t('sharedPool.codexTicketHint') }}</p><p class="input-hint">{{ t('sharedPool.codexTicketRequiredHint') }}</p></div>
              <Toggle id="shared-import-codex-ticket" v-model="defaults.codex_ticket_enabled" :aria-label="t('sharedPool.codexTicket')" :disabled="importing" @update:model-value="result = null" />
            </div>
          </div>
        </div>
      </fieldset>
      <p v-if="error" role="alert" class="break-words text-sm text-red-600 dark:text-red-400">{{ error }}</p>
      <div v-if="result" role="status" class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-dark-700">
        <h4 class="font-medium">{{ t('sharedPool.importResult', { success: result.created, failed: result.failed }) }}</h4>
        <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('sharedPool.importRetryHint') }}</p>
        <ul v-if="result.failed" class="max-h-48 space-y-2 overflow-y-auto text-sm text-red-600 dark:text-red-400">
          <li v-for="item in result.items.filter(item => !item.account_id)" :key="item.index" class="break-words">{{ item.source }} #{{ item.index }} · {{ item.name }} — {{ item.message }}</li>
        </ul>
        <ul v-if="result.warnings.length" class="space-y-1 text-xs text-amber-600 dark:text-amber-400"><li v-for="(warning, index) in result.warnings" :key="index" class="break-words">{{ warning }}</li></ul>
      </div>
    </form>
    <template #footer>
      <button type="button" class="btn btn-secondary" :disabled="importing" @click="close">{{ t('common.close') }}</button>
      <button type="submit" form="shared-import-form" class="btn btn-primary" :disabled="importing || !config.platforms.length || (!!result && result.failed === 0)">{{ t(importing ? 'admin.accounts.dataImporting' : 'admin.accounts.dataImportButton') }}</button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Toggle from '@/components/common/Toggle.vue'
import SharedRevenueSplit from './SharedRevenueSplit.vue'
import SharedSettlementNotice from './SharedSettlementNotice.vue'
import { hasSettlementPolicy } from './settlementPolicy'
import DailyCooldownSettings from '@/components/account/DailyCooldownSettings.vue'
import { dailyCooldownValidationError, normalizeDailyCooldown, withDailyCooldownExtra } from '@/utils/dailyCooldown'
import { sharedPoolAPI, type SharedConfig, type SharedImportDefaults, type SharedImportInput, type SharedImportResult } from '@/api/sharedPool'
import { useAppStore } from '@/stores/app'

const props = defineProps<{ show: boolean; config: SharedConfig; initialDefaults?: SharedImportDefaults }>()
const emit = defineEmits<{ close: []; imported: [] }>()
const { t } = useI18n()
const app = useAppStore()
const defaults = reactive<SharedImportDefaults & { codex_ticket_enabled: boolean }>({ name: '', concurrency: 1, proxy_url: '', protection_enabled: true, ...props.initialDefaults, enabled: true, codex_ticket_enabled: props.initialDefaults?.codex_ticket_enabled ?? true })
const canConsent = computed(() => hasSettlementPolicy(props.config))
const dailyCooldown = ref(normalizeDailyCooldown(props.initialDefaults?.daily_cooldown))
const dailyCooldownChanged = ref(false)
const fileInput = ref<HTMLInputElement>()
const files = ref<File[]>([])
const content = ref('')
const dragDepth = ref(0)
const importing = ref(false)
const error = ref('')
const result = ref<SharedImportResult | null>(null)
const fileNames = computed(() => files.value.map(file => file.name).join(', '))
const placeholder = computed(() => `${t('admin.accounts.dataImportJsonPlaceholder')}\n[{ "name": "...", "platform": "openai", "type": "oauth", "credentials": {...} }]`)
const maxBytes = 2 * 1024 * 1024
let lastPayload = ''
let operationKey = ''

function selectFiles(incoming: File[]) {
  if (importing.value) return
  result.value = null; error.value = ''
  if (incoming.length > 20 || incoming.reduce((total, file) => total + file.size, 0) > maxBytes) {
    files.value = []; error.value = t('sharedPool.importTooLarge'); return
  }
  if (incoming.some(file => !file.name.toLowerCase().endsWith('.json') && file.type !== 'application/json')) {
    files.value = []; error.value = t('sharedPool.importUnsupported'); return
  }
  files.value = incoming
}
function fileChanged(event: Event) {
  const target = event.target as HTMLInputElement
  selectFiles(Array.from(target.files || [])); target.value = ''
}
function drop(event: DragEvent) { dragDepth.value = 0; selectFiles(Array.from(event.dataTransfer?.files || [])) }
function readFile(file: File): Promise<string> {
  if (typeof file.text === 'function') return file.text()
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result || ''))
    reader.onerror = () => reject(new Error(t('sharedPool.importUnsupported')))
    reader.readAsText(file)
  })
}
async function getSources(): Promise<SharedImportInput['sources']> {
  const text = content.value.trim()
  if (text && files.value.length) throw new Error(t('sharedPool.importSourceConflict'))
  if (!text && !files.value.length) throw new Error(t('admin.accounts.dataImportSelectFile'))
  const sources = text ? [{ name: t('admin.accounts.dataImportJsonText'), content: text }]
    : await Promise.all(files.value.map(async file => ({ name: file.name, content: await readFile(file) })))
  if (sources.reduce((size, source) => size + new TextEncoder().encode(source.content).length, 0) > maxBytes) throw new Error(t('sharedPool.importTooLarge'))
  return sources
}
async function submit() {
  if (importing.value) return
  if (!canConsent.value) { error.value = t('sharedPool.settlementRequired'); return }
  const cooldownError = dailyCooldownValidationError(dailyCooldown.value)
  if (cooldownError) { error.value = t(cooldownError); result.value = null; return }
  importing.value = true; error.value = ''; result.value = null
  try {
    const cooldown = dailyCooldownChanged.value || props.initialDefaults?.daily_cooldown !== undefined
      ? { daily_cooldown: withDailyCooldownExtra(undefined, dailyCooldown.value).daily_cooldown } : {}
    const input: SharedImportInput = { sources: await getSources(), defaults: { ...defaults, enabled: true, dispatch_consent: true, ...cooldown, name: defaults.name?.trim() } }
    const payload = JSON.stringify(input)
    // 网络失败重试沿用同一请求号；凭证只在弹窗内存中保留。
    if (payload !== lastPayload) {
      operationKey = `shared-import-${globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`}`
      lastPayload = payload
    }
    result.value = await sharedPoolAPI.importAccounts(input, operationKey)
    lastPayload = ''
    if (result.value.created) emit('imported')
    if (result.value.created > 0 && result.value.failed === 0) {
      app.showSuccess(t('sharedPool.importResult', { success: result.value.created, failed: 0 }))
      if (result.value.warnings.length) app.showWarning(result.value.warnings.join('\n'))
      emit('close')
    }
  } catch (cause: unknown) { error.value = (cause as Error).message || t('sharedPool.actionFailed') }
  finally { importing.value = false }
}
function close() { if (!importing.value) emit('close') }
onBeforeUnmount(() => { files.value = []; content.value = ''; lastPayload = ''; operationKey = '' })
</script>
