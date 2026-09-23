<template>
  <Teleport to="body">
    <div class="modal-overlay" style="z-index: 80" role="dialog" aria-modal="true" :aria-labelledby="titleId" data-testid="ticket-preview-dialog">
      <div ref="panel" class="modal-content min-w-0 max-w-5xl" tabindex="-1">
        <header class="modal-header"><h2 :id="titleId" class="modal-title">{{ t(`${prefix}.title`) }}</h2><button type="button" class="btn btn-secondary" @click="emit('close')">{{ t('common.close') }}</button></header>
        <div class="modal-body min-w-0 space-y-4">
          <p class="text-sm text-gray-600 dark:text-gray-300">{{ t(`${prefix}.hint`) }}</p>
          <form class="space-y-4" novalidate data-testid="ticket-preview-form" @submit.prevent="preview">
            <fieldset :disabled="loading" class="min-w-0 space-y-3">
              <label class="block space-y-1"><span class="input-label">{{ t(`${prefix}.model`) }}</span><input v-model="model" class="input w-full" data-testid="preview-model" /></label>
              <div class="flex flex-wrap items-center justify-between gap-2"><h3 class="input-label">{{ t(`${prefix}.set`) }}</h3><button type="button" class="btn btn-secondary px-2 py-1 text-xs" data-testid="preview-add-set" :disabled="sets.length >= 32" @click="sets.push({ name: '', value: '' })">{{ t(`${prefix}.add`) }}</button></div>
              <div v-for="(row, index) in sets" :key="index" class="grid min-w-0 gap-2 sm:grid-cols-[1fr_2fr_auto]">
                <label class="min-w-0"><span class="sr-only">{{ t(`${prefix}.name`) }} {{ index + 1 }}</span><textarea v-model="row.name" rows="1" class="input w-full font-mono text-xs" :data-testid="`preview-set-name-${index}`" :placeholder="t(`${prefix}.name`)" /></label>
                <label class="min-w-0"><span class="sr-only">{{ t(`${prefix}.value`) }} {{ index + 1 }}</span><textarea v-model="row.value" rows="1" class="input w-full font-mono text-xs" :data-testid="`preview-set-value-${index}`" :placeholder="t(`${prefix}.value`)" /></label>
                <button type="button" class="btn btn-secondary px-2 py-1 text-xs" @click="sets.splice(index, 1)">{{ t(`${prefix}.delete`) }}</button>
              </div>
              <div class="flex flex-wrap items-center justify-between gap-2"><h3 class="input-label">{{ t(`${prefix}.remove`) }}</h3><button type="button" class="btn btn-secondary px-2 py-1 text-xs" data-testid="preview-add-remove" :disabled="removes.length >= 32" @click="removes.push('')">{{ t(`${prefix}.add`) }}</button></div>
              <div v-for="(_, index) in removes" :key="index" class="flex min-w-0 items-start gap-2"><label class="min-w-0 flex-1"><span class="sr-only">{{ t(`${prefix}.remove`) }} {{ index + 1 }}</span><textarea v-model="removes[index]" rows="1" class="input w-full font-mono text-xs" :data-testid="`preview-remove-${index}`" /></label><button type="button" class="btn btn-secondary px-2 py-1 text-xs" @click="removes.splice(index, 1)">{{ t(`${prefix}.delete`) }}</button></div>
              <button type="submit" class="btn btn-primary" data-testid="preview-submit" :disabled="loading">{{ t(`${prefix}.${loading ? 'loading' : 'submit'}`) }}</button>
            </fieldset>
          </form>
          <p v-if="error" role="alert" class="break-all text-sm text-red-600 dark:text-red-400">{{ error }}</p>
          <section v-if="result" class="min-w-0 space-y-3" data-testid="preview-result">
            <p role="status" class="text-sm text-emerald-700 dark:text-emerald-300">{{ t(`${prefix}.notSent`) }}</p>
            <ul v-if="result.validation_errors?.length" role="alert" class="space-y-1 break-all text-xs text-amber-700 dark:text-amber-300"><li v-for="(issue, index) in result.validation_errors" :key="index">{{ issue.field }}: {{ issue.message }}</li></ul>
            <div class="grid min-w-0 gap-3 md:grid-cols-2"><div v-for="kind in ['before_headers', 'after_headers'] as const" :key="kind" class="min-w-0"><h3 class="mb-1 text-sm font-medium">{{ t(`${prefix}.${kind}`) }}</h3><pre class="max-h-64 overflow-y-auto whitespace-pre-wrap break-all rounded bg-gray-50 p-3 font-mono text-xs dark:bg-dark-800">{{ headers(result[kind]) }}</pre></div></div>
            <h3 class="text-sm font-medium">{{ t(`${prefix}.changes`) }}</h3>
            <p v-if="!result.changes?.length" class="text-xs text-gray-500">{{ t(`${prefix}.noChanges`) }}</p>
            <dl v-for="(change, index) in result.changes" :key="index" class="min-w-0 break-all text-xs"><dt class="font-mono font-semibold">{{ change.name }} · {{ change.kind }}</dt><dd class="whitespace-pre-wrap">{{ t(`${prefix}.before_headers`) }}: {{ change.before?.join('\n') || '—' }}</dd><dd class="whitespace-pre-wrap">{{ t(`${prefix}.after_headers`) }}: {{ change.after?.join('\n') || '—' }}</dd></dl>
            <h3 class="text-sm font-medium">{{ t(`${prefix}.sources`) }}</h3>
            <dl v-for="(source, index) in result.header_sources" :key="index" class="break-all text-xs"><dt class="font-mono">{{ source.name }} · {{ source.source }}</dt><dd>{{ source.reason }}</dd></dl>
          </section>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { previewCodexTicketRequest, type CodexTicketPreview } from '@/api/admin/codexTicketDiagnostics'
import { extractApiErrorMessage } from '@/utils/apiError'
import { useCodexTicketDialog } from '@/utils/codexTicketDialog'

const props = defineProps<{ accountId: number; initialModel?: string; opener?: HTMLElement | null }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const prefix = 'admin.accounts.codexTicketPreview'
const titleId = useId()
const panel = ref<HTMLElement | null>(null)
const model = ref(props.initialModel ?? '')
const sets = ref([{ name: '', value: '' }])
const removes = ref<string[]>([])
const loading = ref(false)
const error = ref('')
const result = ref<CodexTicketPreview | null>(null)
let sequence = 0
const headers = (value: Record<string, string[]> | undefined) => Object.entries(value ?? {}).flatMap(([name, values]) => values.map(value => `${name}: ${value}`)).join('\n') || '—'
async function preview() {
  if (loading.value) return
  const current = ++sequence
  loading.value = true
  result.value = null
  error.value = ''
  try {
    const response = await previewCodexTicketRequest(props.accountId, {
      model: model.value, set: sets.value.filter(row => row.name !== '' || row.value !== '').map(row => ({ ...row })), remove: removes.value.filter(name => name !== '')
    })
    if (current !== sequence) return
    if (response.sent !== false) throw new Error(t(`${prefix}.invalidResponse`))
    result.value = response
  } catch (cause) { if (current === sequence) error.value = extractApiErrorMessage(cause, t(`${prefix}.failed`)) }
  finally { if (current === sequence) loading.value = false }
}
watch([model, sets, removes], () => { result.value = null; error.value = '' }, { deep: true })
watch(() => props.accountId, () => { sequence++; result.value = null; error.value = ''; loading.value = false })
onBeforeUnmount(() => { sequence++ })
useCodexTicketDialog(panel, props.opener ?? null, () => emit('close'))
</script>
