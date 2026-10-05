<template>
  <fieldset :disabled="disabled" :data-testid="`${testIdPrefix}-options`" class="min-w-0 space-y-2">
    <legend class="input-label">{{ t('admin.accounts.openai.prismBrowserModels') }}</legend>
    <div class="grid gap-2 sm:grid-cols-2">
      <label v-for="model in PRISM_BROWSER_MODELS" :key="model" class="flex min-w-0 items-center gap-2 text-sm">
        <input v-model="models" type="checkbox" :value="model" :data-testid="`${testIdPrefix}-model-${model}`" />
        <span class="break-all">{{ model }}</span>
      </label>
    </div>
    <p class="input-hint">{{ t('admin.accounts.openai.prismBrowserModelsHint') }}</p>
  </fieldset>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { normalizePrismBrowserOptions, PRISM_BROWSER_MODELS, type PrismBrowserOptions } from '@/utils/prismBrowserOptions'

withDefaults(defineProps<{ disabled?: boolean; testIdPrefix?: string }>(), { testIdPrefix: 'prism' })
const options = defineModel<PrismBrowserOptions>({ required: true })
const { t } = useI18n()
const models = computed({
  get: () => normalizePrismBrowserOptions(options.value).models,
  set: models => { options.value = normalizePrismBrowserOptions({ models }) }
})
</script>
