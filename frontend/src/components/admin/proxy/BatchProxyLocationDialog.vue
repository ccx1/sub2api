<template>
  <BaseDialog :show="show" :title="t('admin.proxies.batchLocationTitle')" width="normal" @close="close">
    <form id="batch-proxy-location-form" class="space-y-4" @submit.prevent="submit">
      <p class="text-sm text-gray-500 dark:text-gray-400">
        {{ t('admin.proxies.batchLocationHint', { count: ids.length }) }}
      </p>
      <label class="block">
        <span class="input-label">{{ t('admin.proxies.batchCountry') }}</span>
        <Select v-model="countryCode" :options="countryOptions" :disabled="busy" searchable="auto" />
      </label>
      <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
    </form>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="busy" @click="close">{{ t('common.cancel') }}</button>
        <button type="submit" form="batch-proxy-location-form" class="btn btn-primary" :disabled="busy || !ids.length">
          {{ busy ? t('common.saving') : t('common.save') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'

type CountryOption = { code: string; label: string }

const props = defineProps<{ show: boolean; ids: number[]; countries: CountryOption[] }>()
const emit = defineEmits<{ close: []; changed: [] }>()
const { t } = useI18n()
const appStore = useAppStore()
const countryCode = ref<string | null>(null)
const busy = ref(false)
const error = ref('')

const countryOptions = computed(() => [
  { value: null, label: t('admin.proxies.countryOptional') },
  ...props.countries.map(country => ({ value: country.code, label: country.label }))
])

watch(() => props.show, visible => {
  if (!visible) return
  countryCode.value = null
  error.value = props.ids.length > 1000 ? t('proxyGroups.batchLimit') : ''
})

const close = () => { if (!busy.value) emit('close') }

async function submit() {
  if (busy.value || !props.ids.length || props.ids.length > 1000) return
  busy.value = true
  error.value = ''
  try {
    await Promise.all(props.ids.map(id => adminAPI.proxies.update(id, { country_code: countryCode.value ?? '' })))
    appStore.showSuccess(t('admin.proxies.batchLocationSaved', { count: props.ids.length }))
    emit('changed')
    emit('close')
  } catch (cause) {
    const response = (cause as { response?: { data?: { message?: string; detail?: string } } }).response
    error.value = response?.data?.detail || response?.data?.message || (cause as { message?: string }).message || t('admin.proxies.batchLocationFailed')
  } finally {
    busy.value = false
  }
}
</script>
