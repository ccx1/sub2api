<template>
  <section class="space-y-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600" data-testid="ticket-runtime-panel">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ t(`${prefix}.title`) }}</h3>
      <button type="button" class="btn btn-secondary px-2 py-1 text-xs" :disabled="loading" @click="refresh">{{ t('common.refresh') }}</button>
    </div>
    <p v-if="loading" role="status" class="text-xs text-gray-500">{{ t(`${prefix}.loading`) }}</p>
    <p v-else-if="error" role="alert" class="text-xs text-red-600 dark:text-red-400">{{ t(`${prefix}.loadFailed`) }}</p>
    <CodexTicketProtectionStatus v-else-if="status" :status="status" />
  </section>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getCodexTicketRuntimeStatus, type CodexTicketRuntimeStatus } from '@/api/admin/codexTicketDiagnostics'
import CodexTicketProtectionStatus from './CodexTicketProtectionStatus.vue'

const props = defineProps<{ accountId: number }>()
const { t } = useI18n()
const prefix = 'admin.accounts.codexTicketRuntime'
const status = ref<CodexTicketRuntimeStatus | null>(null)
const loading = ref(false)
const error = ref(false)
let sequence = 0
async function refresh() {
  const current = ++sequence
  loading.value = true
  error.value = false
  try {
    const result = await getCodexTicketRuntimeStatus(props.accountId)
    if (current === sequence) status.value = result
  } catch { if (current === sequence) error.value = true }
  finally { if (current === sequence) loading.value = false }
}
watch(() => props.accountId, () => { status.value = null; void refresh() }, { immediate: true })
onBeforeUnmount(() => { sequence++ })
defineExpose({ refresh })
</script>
