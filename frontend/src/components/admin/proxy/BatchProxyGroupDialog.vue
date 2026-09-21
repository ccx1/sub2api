<template>
  <BaseDialog :show="show" :title="t('proxyGroups.batch')" width="normal" @close="close">
    <form id="batch-proxy-group-form" class="space-y-4" @submit.prevent="submit">
      <p class="text-sm text-gray-500">{{ t('proxyGroups.batchHint', { count: ids.length }) }}</p>
      <ProxyGroupSelect v-model="groupId" :groups="groups" :disabled="busy" />
      <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
    </form>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="busy" @click="close">{{ t('common.cancel') }}</button>
        <button type="submit" form="batch-proxy-group-form" class="btn btn-primary" :disabled="busy || !ids.length || ids.length > 1000">{{ t('common.save') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { ProxyGroup } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ProxyGroupSelect from './ProxyGroupSelect.vue'

const props = defineProps<{ show: boolean; ids: number[]; groups: ProxyGroup[] }>()
const emit = defineEmits<{ close: []; changed: [] }>()
const { t } = useI18n()
const appStore = useAppStore()
const groupId = ref<number | null>(null)
const busy = ref(false)
const error = ref('')
watch(() => props.show, () => {
  groupId.value = null
  error.value = props.ids.length > 1000 ? t('proxyGroups.batchLimit') : ''
})
const close = () => { if (!busy.value) emit('close') }
async function submit() {
  if (busy.value || !props.ids.length || props.ids.length > 1000) return
  busy.value = true
  error.value = ''
  try {
    const result = await adminAPI.proxies.batchSetGroup(props.ids, groupId.value)
    appStore.showSuccess(t('proxyGroups.batchSaved', { count: result.updated_count }))
    emit('changed')
    emit('close')
  } catch (cause) {
    const response = (cause as { response?: { data?: { message?: string; detail?: string } } }).response
    error.value = response?.data?.detail || response?.data?.message || (cause as { message?: string }).message || t('proxyGroups.batchFailed')
  } finally { busy.value = false }
}
</script>
