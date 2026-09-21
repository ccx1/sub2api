<template>
  <BaseDialog :show="show" :title="t('proxyGroups.manage')" width="normal" @close="close">
    <p class="mb-4 text-sm text-gray-500">{{ t('proxyGroups.hint') }}</p>
    <form class="flex flex-wrap items-end gap-2" @submit.prevent="save">
      <label class="min-w-0 flex-1">
        <span class="input-label">{{ t('proxyGroups.name') }}</span>
        <input v-model="name" class="input" required maxlength="100" :disabled="busy" :placeholder="t('proxyGroups.namePlaceholder')" />
      </label>
      <button class="btn btn-primary" :disabled="busy || !name.trim()">{{ editingId ? t('common.save') : t('common.create') }}</button>
      <button v-if="editingId" type="button" class="btn btn-secondary" :disabled="busy" @click="reset">{{ t('common.cancel') }}</button>
    </form>
    <div v-if="error" role="alert" class="mt-3 text-sm text-red-600 dark:text-red-400">{{ error }}</div>
    <div class="mt-5 max-h-80 space-y-2 overflow-y-auto">
      <div v-for="group in groups" :key="group.id" class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 py-3 dark:border-dark-600">
        <div class="min-w-0 flex-1">
          <p class="break-all font-medium text-gray-900 dark:text-gray-100">{{ group.name }}</p>
          <p class="text-xs text-gray-500">{{ t('proxyGroups.counts', { active: group.active_proxy_count, total: group.proxy_count }) }}</p>
        </div>
        <div class="flex shrink-0 gap-2">
          <button type="button" class="btn btn-secondary" :disabled="busy" @click="edit(group)">{{ t('common.edit') }}</button>
          <button type="button" class="btn btn-danger" :disabled="busy" @click="deleting = group">{{ t('common.delete') }}</button>
        </div>
      </div>
      <p v-if="!groups.length" class="py-4 text-sm text-gray-500">{{ t('proxyGroups.empty') }}</p>
    </div>
    <div v-if="deleting" class="mt-4 space-y-3 rounded-lg border border-red-200 p-3 dark:border-red-900">
      <p class="break-words text-sm">{{ t('proxyGroups.deleteConfirm', { name: deleting.name }) }}</p>
      <div class="flex justify-end gap-2">
        <button type="button" class="btn btn-secondary" :disabled="busy" @click="deleting = null">{{ t('common.cancel') }}</button>
        <button type="button" class="btn btn-danger" :disabled="busy" @click="remove">{{ t('common.delete') }}</button>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { ProxyGroup } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'

const props = defineProps<{ show: boolean; groups: ProxyGroup[] }>()
const emit = defineEmits<{ close: []; changed: [] }>()
const { t } = useI18n()
const appStore = useAppStore()
const name = ref('')
const editingId = ref<number | null>(null)
const deleting = ref<ProxyGroup | null>(null)
const busy = ref(false)
const error = ref('')
const reset = () => { name.value = ''; editingId.value = null; error.value = '' }
watch(() => props.show, () => { reset(); deleting.value = null })
const close = () => { if (!busy.value) emit('close') }
const edit = (group: ProxyGroup) => { name.value = group.name; editingId.value = group.id; deleting.value = null; error.value = '' }
const errorMessage = (cause: unknown, key: string) => {
  const response = (cause as { response?: { data?: { message?: string; detail?: string } } }).response
  return response?.data?.detail || response?.data?.message || (cause as { message?: string }).message || t(key)
}

async function save() {
  if (busy.value || !name.value.trim()) return
  busy.value = true
  error.value = ''
  try {
    if (editingId.value) await adminAPI.proxies.updateGroup(editingId.value, name.value.trim())
    else await adminAPI.proxies.createGroup(name.value.trim())
    reset()
    appStore.showSuccess(t('proxyGroups.saved'))
    emit('changed')
  } catch (cause) {
    error.value = errorMessage(cause, 'proxyGroups.saveFailed')
  } finally { busy.value = false }
}

async function remove() {
  if (busy.value || !deleting.value) return
  busy.value = true
  error.value = ''
  try {
    await adminAPI.proxies.deleteGroup(deleting.value.id)
    if (editingId.value === deleting.value.id) reset()
    deleting.value = null
    appStore.showSuccess(t('proxyGroups.deleted'))
    emit('changed')
  } catch (cause) {
    error.value = errorMessage(cause, 'proxyGroups.deleteFailed')
  } finally { busy.value = false }
}
</script>
