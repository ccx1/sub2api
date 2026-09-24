<template>
  <fieldset :disabled="disabled" class="min-w-0 space-y-4">
    <label class="flex items-start gap-3 text-sm text-gray-900 dark:text-white">
      <input v-model="policy.enabled" type="checkbox" data-testid="quality-enabled" class="mt-1 h-4 w-4" />
      <span>{{ t('codexModelQuality.enabled') }}<span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">{{ t('codexModelQuality.enabledHint') }}</span></span>
    </label>
    <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <label v-for="field in qualityNumericFields" :key="field.key" class="min-w-0 space-y-1">
        <span class="input-label">{{ t(`codexModelQuality.fields.${field.key}`) }}</span>
        <input v-model.number="policy[field.key]" :data-testid="`quality-${field.key}`" type="number" :min="field.min" :max="field.max" step="1" class="input w-full" />
        <span class="block text-xs text-gray-500 dark:text-gray-400">{{ t('codexModelQuality.range', { min: field.min, max: field.max }) }}</span>
      </label>
      <label class="min-w-0 space-y-1">
        <span class="input-label">{{ t('codexModelQuality.reasoningEffort') }}</span>
        <select v-model="policy.reasoning_effort" class="input w-full" data-testid="quality-reasoning-effort">
          <option v-for="effort in ['low', 'medium', 'high']" :key="effort" :value="effort">{{ t(`codexModelQuality.effort.${effort}`) }}</option>
        </select>
      </label>
    </div>
    <section v-if="models.length" class="space-y-3" data-testid="quality-priorities" aria-labelledby="quality-priority-title">
      <div>
        <h3 id="quality-priority-title" class="text-sm font-medium text-gray-900 dark:text-white">{{ t('codexModelQuality.priorityTitle') }}</h3>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('codexModelQuality.priorityHint') }}</p>
      </div>
      <div class="grid gap-3 sm:grid-cols-2">
        <label v-for="model in models" :key="model" class="min-w-0 space-y-1">
          <span class="input-label">{{ model }}</span>
          <input :value="priorityValue(model)" @input="setPriority(model, $event)" type="number" min="0" max="100" step="1" class="input w-full" :data-testid="`quality-priority-${model}`" />
        </label>
      </div>
    </section>
    <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('codexModelQuality.budgetHint') }}</p>
    <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('codexModelQuality.concurrencyHint') }}</p>
    <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('codexModelQuality.qualityCircuitHint') }}</p>
    <label class="flex items-start gap-3 text-sm text-gray-900 dark:text-white">
      <input v-model="policy.quarantine_on_failure" type="checkbox" data-testid="quality-quarantine" class="mt-1 h-4 w-4" />
      <span>{{ t('codexModelQuality.quarantine') }}<span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">{{ t('codexModelQuality.quarantineHint') }}</span></span>
    </label>
    <label class="flex items-start gap-3 text-sm text-gray-900 dark:text-white">
      <input v-model="policy.fingerprint_enabled" type="checkbox" data-testid="quality-fingerprint" class="mt-1 h-4 w-4" />
      <span>{{ t('codexModelQuality.fingerprint') }}<span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">{{ t('codexModelQuality.fingerprintHint') }}</span></span>
    </label>
  </fieldset>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { qualityNumericFields, type CodexModelQualityPolicy } from '@/api/admin/codexModelQuality'
defineProps<{ disabled: boolean; models: string[] }>()
const policy = defineModel<CodexModelQualityPolicy>({ required: true })
const { t } = useI18n()

function priorityValue(model: string): number | '' {
  return policy.value.model_priorities?.[model] ?? ''
}

function setPriority(model: string, event: Event) {
  const input = event.target as HTMLInputElement
  const raw = input.value.trim()
  const priorities = { ...(policy.value.model_priorities ?? {}) }
  if (raw === '') delete priorities[model]
  else priorities[model] = Number(raw)
  policy.value.model_priorities = Object.keys(priorities).length ? priorities : undefined
}
</script>
