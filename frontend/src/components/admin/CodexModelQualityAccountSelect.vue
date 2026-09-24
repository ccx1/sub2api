<template>
  <div class="min-w-0 space-y-3">
    <form class="flex flex-wrap items-end gap-2" @submit.prevent="search">
      <label class="min-w-0 flex-1 space-y-1">
        <span class="input-label">{{ t('codexModelQuality.accountSearch') }}</span>
        <input v-model="keyword" data-testid="quality-account-search" type="search" class="input w-full" :placeholder="t('codexModelQuality.searchPlaceholder')" />
      </label>
      <button type="submit" class="btn btn-secondary" :disabled="loading">{{ t('codexModelQuality.search') }}</button>
    </form>
    <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
    <div class="flex min-w-0 flex-wrap items-end gap-3">
      <label class="w-full min-w-0 space-y-1 sm:w-auto sm:flex-1">
        <span class="input-label">{{ t('codexModelQuality.account') }}</span>
        <select :value="selectedId ?? ''" data-testid="quality-account" class="input w-full" :disabled="loading" @change="selectAccount">
          <option value="">{{ t(loading ? 'codexModelQuality.loading' : 'codexModelQuality.selectAccount') }}</option>
          <option v-if="selected && !accounts.some(account => account.id === selected?.id)" :value="selected.id">{{ selected.name }} (#{{ selected.id }})</option>
          <option v-for="account in accounts" :key="account.id" :value="account.id">{{ account.name }} (#{{ account.id }})</option>
        </select>
      </label>
      <div class="flex items-center gap-2">
        <button type="button" class="btn btn-secondary" :disabled="loading || page <= 1" @click="loadPage(page - 1)">{{ t('codexModelQuality.previous') }}</button>
        <span class="text-xs text-gray-500">{{ page }} / {{ pages }}</span>
        <button type="button" class="btn btn-secondary" :disabled="loading || page >= pages" @click="loadPage(page + 1)">{{ t('codexModelQuality.next') }}</button>
      </div>
    </div>
    <p v-if="!loading && !error && !accounts.length" class="text-sm text-gray-500">{{ t('codexModelQuality.noAccounts') }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { list } from '@/api/admin/accounts'
import { extractApiErrorMessage } from '@/utils/apiError'

interface AccountOption { id: number; name: string }
const selectedId = defineModel<number | null>({ default: null })
const { t } = useI18n()
const keyword = ref('')
const appliedSearch = ref('')
const selected = ref<AccountOption | null>(null)
const accounts = ref<AccountOption[]>([])
const loading = ref(false)
const error = ref('')
const page = ref(1)
const total = ref(0)
const pages = computed(() => Math.max(1, Math.ceil(total.value / 20)))
let sequence = 0

async function loadPage(target: number) {
  const request = ++sequence
  loading.value = true
  error.value = ''
  try {
    const result = await list(target, 20, { platform: 'openai', search: appliedSearch.value, lite: 'true' })
    if (request !== sequence) return
    accounts.value = result.items
      .filter(account => !account.parent_account_id && ['oauth', 'setup-token'].includes(account.type))
      .map(({ id, name }) => ({ id, name }))
    total.value = result.total
    page.value = target
  } catch (cause) {
    if (request === sequence) error.value = extractApiErrorMessage(cause, t('codexModelQuality.accountsFailed'))
  } finally { if (request === sequence) loading.value = false }
}
function search() {
  appliedSearch.value = keyword.value.trim()
  void loadPage(1)
}
function selectAccount(event: Event) {
  const value = Number((event.target as HTMLSelectElement).value) || null
  selected.value = accounts.value.find(account => account.id === value) ?? (selected.value?.id === value ? selected.value : null)
  selectedId.value = value
}
onMounted(() => { void loadPage(1) })
onUnmounted(() => { ++sequence })
</script>
