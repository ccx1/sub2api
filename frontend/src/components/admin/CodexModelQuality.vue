<template>
  <section class="card min-w-0 space-y-5 p-5" aria-labelledby="codex-model-quality-title">
    <header>
      <h2 id="codex-model-quality-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('codexModelQuality.title') }}</h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('codexModelQuality.description') }}</p>
    </header>
    <p v-if="loading" role="status" class="text-sm text-gray-500">{{ t('codexModelQuality.loading') }}</p>
    <div v-else-if="loadError" class="flex flex-wrap items-center gap-3">
      <p role="alert" class="text-sm text-red-600 dark:text-red-400">{{ loadError }}</p>
      <button type="button" class="btn btn-secondary" @click="load">{{ t('codexModelQuality.reload') }}</button>
    </div>
    <form v-else-if="form" data-testid="quality-policy-form" class="space-y-4" novalidate @submit.prevent="save">
      <CodexModelQualityFields v-model="form" :disabled="saving" :models="models" />
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('codexModelQuality.modelsHint', { models: models.join(', ') || '—' }) }}</p>
      <p v-if="validationError" role="alert" class="text-sm text-amber-700 dark:text-amber-300">{{ validationError }}</p>
      <p v-if="saveError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ saveError }}</p>
      <p v-if="saved" role="status" class="text-sm text-emerald-700 dark:text-emerald-300">{{ t('codexModelQuality.saved') }}</p>
      <div class="flex justify-end"><button type="submit" data-testid="quality-save" class="btn btn-primary" :disabled="saving || !!validationError">{{ t(saving ? 'codexModelQuality.saving' : 'codexModelQuality.save') }}</button></div>
    </form>
    <CodexModelQualityResults :models="models" :enabled="savedEnabled" />
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getCodexModelQualityPolicy, saveCodexModelQualityPolicy, qualityNumericFields, type CodexModelQualityPolicy } from '@/api/admin/codexModelQuality'
import { extractApiErrorMessage } from '@/utils/apiError'
import CodexModelQualityFields from './CodexModelQualityFields.vue'
import CodexModelQualityResults from './CodexModelQualityResults.vue'

const { t } = useI18n()
const loading = ref(true)
const saving = ref(false)
const saved = ref(false)
const savedEnabled = ref(false)
const form = ref<CodexModelQualityPolicy | null>(null)
const loadError = ref('')
const saveError = ref('')
const props = defineProps<{ models: string[] }>()
const models = computed(() => props.models)
const validationError = computed(() => {
  if (!form.value) return ''
  const invalid = qualityNumericFields.find(field => {
    const value = form.value![field.key]
    return !Number.isInteger(value) || value < field.min || value > field.max
  })
  if (invalid) return t('codexModelQuality.invalidRange', { field: t(`codexModelQuality.fields.${invalid.key}`), min: invalid.min, max: invalid.max })
  if (!['low', 'medium', 'high'].includes(form.value.reasoning_effort)) return t('codexModelQuality.invalidEffort')
  const invalidPriority = models.value.find(model => {
    const priority = form.value?.model_priorities?.[model]
    return priority !== undefined && (!Number.isInteger(priority) || priority < 0 || priority > 100)
  })
  return invalidPriority ? t('codexModelQuality.invalidPriority', { model: invalidPriority }) : ''
})

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const policy = await getCodexModelQualityPolicy()
    form.value = { ...policy, model_priorities: policy.model_priorities ? { ...policy.model_priorities } : undefined }
    savedEnabled.value = form.value.enabled
  } catch (error) { loadError.value = extractApiErrorMessage(error, t('codexModelQuality.loadFailed')) }
  finally { loading.value = false }
}
async function save() {
  if (saving.value || !form.value || validationError.value) return
  saving.value = true
  saved.value = false
  saveError.value = ''
  try {
    form.value = await saveCodexModelQualityPolicy({ ...form.value })
    savedEnabled.value = form.value.enabled
    saved.value = true
  } catch (error) { saveError.value = extractApiErrorMessage(error, t('codexModelQuality.saveFailed')) }
  finally { saving.value = false }
}
watch(form, () => { saved.value = false }, { deep: true, flush: 'sync' })
onMounted(load)
</script>
