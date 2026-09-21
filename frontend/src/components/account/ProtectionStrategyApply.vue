<template>
  <section class="card min-w-0 space-y-4 p-5" aria-labelledby="protection-apply-title">
    <div>
      <h2 id="protection-apply-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('accountProtection.applyTitle') }}</h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('accountProtection.applyHint') }}</p>
    </div>
    <form class="grid gap-3 sm:grid-cols-2 xl:grid-cols-[1fr_1fr_auto] xl:items-end" @submit.prevent="inspect">
      <label class="min-w-0 space-y-1">
        <span class="input-label">{{ t('accountProtection.accountId') }}</span>
        <input v-model="accountId" data-testid="account-id" class="input" inputmode="numeric" :placeholder="t('accountProtection.accountPlaceholder')" :disabled="applying || disabled" />
      </label>
      <label class="min-w-0 space-y-1">
        <span class="input-label">{{ t('accountProtection.targetStrategy') }}</span>
        <select v-model="selectedMode" data-testid="target-mode" class="input" :disabled="applying || disabled">
          <option v-for="strategy in applicableStrategies" :key="strategy.id" :value="strategy.id">{{ strategy.name }}</option>
        </select>
      </label>
      <button type="submit" data-testid="inspect-account" class="btn btn-secondary" :disabled="inspecting || applying || disabled">
        {{ t(inspecting ? 'accountProtection.inspecting' : 'accountProtection.inspect') }}
      </button>
    </form>
    <p v-if="errorMessage" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ errorMessage }}</p>
    <div v-if="account" class="flex flex-wrap gap-x-8 gap-y-2 border-y border-gray-100 py-3 text-sm dark:border-dark-700">
      <p class="min-w-0 break-words text-gray-700 dark:text-gray-300">{{ t('accountProtection.accountLabel') }}: <strong>{{ account.name }}</strong> <span class="text-gray-500">#{{ account.id }} · {{ account.platform }} / {{ account.type }}</span></p>
      <p v-if="preview" data-testid="current-strategy" class="text-gray-700 dark:text-gray-300">{{ t('accountProtection.currentStrategy') }}: <strong>{{ currentStrategyName }}</strong></p>
    </div>
    <div v-if="preview" class="space-y-3" aria-live="polite">
      <p v-if="preview.reason || !preview.eligible" :class="preview.eligible ? 'text-gray-600 dark:text-gray-300' : 'text-amber-700 dark:text-amber-300'" class="text-sm">{{ preview.reason || t('accountProtection.notApplicable') }}</p>
      <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('accountProtection.changesTitle') }}</h3>
      <div v-if="preview.changes.length" class="overflow-x-auto">
        <table class="w-full min-w-[460px] text-left text-sm">
          <thead class="text-gray-500 dark:text-gray-400"><tr><th class="py-2 pr-4">{{ t('accountProtection.field') }}</th><th class="py-2 pr-4">{{ t('accountProtection.before') }}</th><th class="py-2">{{ t('accountProtection.after') }}</th></tr></thead>
          <tbody class="text-gray-700 dark:text-gray-300">
            <tr v-for="change in preview.changes" :key="change.key" class="border-t border-gray-100 dark:border-dark-700">
              <td class="py-3 pr-4"><span>{{ fieldLabel(change.key) }}</span><p v-if="change.note" class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ change.note }}</p></td>
              <td class="max-w-60 break-words py-3 pr-4">{{ valueLabel(change.key, change.from) }}</td>
              <td class="max-w-60 break-words py-3">{{ valueLabel(change.key, change.to) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-else class="text-sm text-gray-500 dark:text-gray-400">{{ t('accountProtection.noChanges') }}</p>
      <button type="button" data-testid="apply-strategy" class="btn btn-primary max-w-full" :disabled="!canApply" @click="apply">
        {{ t(applying ? 'accountProtection.applying' : 'accountProtection.apply') }}
      </button>
    </div>
    <p v-else-if="!inspecting" class="text-sm text-gray-500 dark:text-gray-400">{{ t('accountProtection.pendingPreview') }}</p>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { getById } from '@/api/admin/accounts'
import { applyProtection, previewProtection, type ProtectionPreview, type ProtectionStrategy } from '@/api/admin/accountProtection'
import type { Account } from '@/types'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{ strategies: ProtectionStrategy[]; defaultMode: string; disabled?: boolean; refreshToken?: number }>()
const emit = defineEmits<{ busy: [busy: boolean] }>()
const { t, te } = useI18n()
const route = useRoute()
const appStore = useAppStore()
const accountId = ref('')
const selectedMode = ref(props.defaultMode)
const account = ref<Account | null>(null)
const preview = ref<ProtectionPreview | null>(null)
const inspecting = ref(false)
const applying = ref(false)
const errorMessage = ref('')
let selectionVersion = 0
let queuedRouteInspection = false
const applicableStrategies = computed(() => props.strategies.filter(strategy => strategy.apply_supported))
const selectedStrategy = computed(() => applicableStrategies.value.find(strategy => strategy.id === selectedMode.value))
const currentStrategyName = computed(() => preview.value?.enabled
  ? strategyName(preview.value.active_mode) : t('accountProtection.protectionDisabled'))
const canApply = computed(() => !props.disabled && !inspecting.value && !applying.value && !!selectedStrategy.value
  && preview.value?.eligible === true && preview.value.account_id === Number(accountId.value) && !!account.value)

function strategyName(mode: string) {
  return props.strategies.find(strategy => strategy.id === mode)?.name || mode
}
function fieldLabel(key: string) {
  const translationKey = `accountProtection.fields.${key.replace(/\./g, '_')}`
  return te(translationKey) ? t(translationKey) : key
}
function valueLabel(key: string, value: unknown): string {
  if (value === null || value === undefined || value === '') return t('accountProtection.unset')
  if (key === 'strategy' && typeof value === 'string') return strategyName(value)
  const section = key.includes('fingerprint_mode') ? 'identityModes' : 'tlsModes'
  const translationKey = `accountProtection.${section}.${String(value)}`
  if (te(translationKey)) return t(translationKey)
  return typeof value === 'object' ? JSON.stringify(value) : String(value)
}
function validId(): number | null {
  const id = Number(accountId.value)
  return /^\d+$/.test(accountId.value) && Number.isSafeInteger(id) && id > 0 ? id : null
}

async function inspect() {
  if (inspecting.value || applying.value || props.disabled) return
  preview.value = null
  account.value = null
  errorMessage.value = ''
  const id = validId()
  if (!id || !selectedStrategy.value) {
    errorMessage.value = t(id ? 'accountProtection.missingStrategy' : 'accountProtection.invalidId')
    return
  }
  const version = selectionVersion
  inspecting.value = true
  try {
    const [loadedAccount, loadedPreview] = await Promise.all([getById(id), previewProtection(id, selectedMode.value)])
    if (version !== selectionVersion) return
    account.value = loadedAccount
    preview.value = loadedPreview
  } catch (error) {
    if (version === selectionVersion) errorMessage.value = extractApiErrorMessage(error, t('accountProtection.previewFailed'))
  } finally {
    inspecting.value = false
    if (queuedRouteInspection) {
      queuedRouteInspection = false
      void inspect()
    }
  }
}

async function apply() {
  if (!canApply.value || !account.value) return
  const id = account.value.id
  const mode = selectedMode.value
  const version = selectionVersion
  applying.value = true
  errorMessage.value = ''
  try {
    const updated = await applyProtection(id, mode)
    if (version !== selectionVersion) return
    account.value = updated
    preview.value = { account_id: id, active_mode: mode, enabled: true, eligible: false, changes: [], reason: t('accountProtection.applySuccess') }
    appStore.showSuccess(t('accountProtection.applySuccess'))
  } catch (error) {
    if (version === selectionVersion) {
      preview.value = null
      errorMessage.value = extractApiErrorMessage(error, t('accountProtection.applyFailed'))
    }
  } finally {
    applying.value = false
  }
}

watch([accountId, selectedMode], () => {
  selectionVersion++
  preview.value = null
  errorMessage.value = ''
}, { flush: 'sync' })
watch(accountId, () => { account.value = null }, { flush: 'sync' })
watch([applying, inspecting], values => emit('busy', values.some(Boolean)), { flush: 'sync' })
watch(() => props.refreshToken, () => {
  selectionVersion++
  preview.value = null
  selectedMode.value = props.defaultMode
  if (validId()) void inspect()
})
watch(() => route.query.account_id, value => {
  if (typeof value !== 'string' || !value) return
  accountId.value = value
  if (inspecting.value) queuedRouteInspection = true
  else void inspect()
}, { immediate: true })
onBeforeUnmount(() => { selectionVersion++; queuedRouteInspection = false })
</script>
