<template>
  <fieldset :disabled="disabled" class="min-w-0 space-y-3" data-testid="codex-ticket-credential-settings">
    <legend class="input-label">{{ t('admin.accounts.codexTicketCredential.title') }}</legend>
    <label class="block space-y-1">
      <span class="sr-only">{{ t('admin.accounts.codexTicketCredential.title') }}</span>
      <select :value="modelValue.mode" :disabled="disabled" class="input w-full" data-testid="codex-ticket-credential-mode" @change="setMode">
        <option v-for="mode in codexTicketCredentialModes" :key="mode" :value="mode">{{ t(`admin.accounts.codexTicketCredential.${mode}`) }}</option>
      </select>
    </label>
    <p class="input-hint">{{ t('admin.accounts.codexTicketCredential.inheritHint') }}</p>
    <p v-if="modelValue.mode === 'state'" class="input-hint">{{ t('admin.accounts.codexTicketCredential.stateHint') }}</p>
    <template v-if="usesCookie">
      <p class="input-hint">{{ t('admin.accounts.codexTicketCredential.experimentalHint') }}</p>
      <div class="grid gap-3 sm:grid-cols-2">
        <label v-for="field in fields" :key="field.key" class="min-w-0 space-y-1">
          <span class="input-label">{{ t(`admin.accounts.codexTicketCredential.${field.key}`) }}</span>
          <input :value="modelValue[field.key] ?? ''" :disabled="disabled" type="number" :min="field.min" :max="field.max" step="1" :placeholder="t('admin.accounts.codexTicketCredential.useGlobal')" :data-testid="`codex-ticket-credential-${field.key}`" class="input w-full" @input="setTiming(field.key, $event)" />
        </label>
      </div>
      <p class="input-hint">{{ t('admin.accounts.codexTicketCredential.timingHint') }}</p>
    </template>
    <p v-if="error" role="alert" class="text-sm text-amber-700 dark:text-amber-400">{{ t(error) }}</p>
  </fieldset>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { codexTicketCredentialModes, codexTicketCredentialValidationError, type CodexTicketCredentialMode, type CodexTicketCredentialPolicy } from '@/utils/codexTicketCredential'

const props = withDefaults(defineProps<{ modelValue: CodexTicketCredentialPolicy; disabled?: boolean }>(), { disabled: false })
const emit = defineEmits<{ 'update:modelValue': [value: CodexTicketCredentialPolicy] }>()
const { t } = useI18n()
const usesCookie = computed(() => props.modelValue.mode === 'cookie' || props.modelValue.mode === 'cookie_state')
const error = computed(() => codexTicketCredentialValidationError(props.modelValue))
const fields = [{ key: 'ttl_seconds', min: 1, max: 3600 }, { key: 'refresh_before_seconds', min: 0, max: 3599 }] as const

function setMode(event: Event) {
  const mode = (event.target as HTMLSelectElement).value as CodexTicketCredentialMode
  if (props.disabled || !codexTicketCredentialModes.includes(mode)) return
  emit('update:modelValue', mode === 'inherit' || mode === 'state' ? { mode } : { ...props.modelValue, mode })
}

function setTiming(key: 'ttl_seconds' | 'refresh_before_seconds', event: Event) {
  if (props.disabled) return
  const input = event.target as HTMLInputElement
  const next = { ...props.modelValue }
  if (input.value === '' && !input.validity.badInput) delete next[key]
  else next[key] = input.valueAsNumber
  emit('update:modelValue', next)
}
</script>
