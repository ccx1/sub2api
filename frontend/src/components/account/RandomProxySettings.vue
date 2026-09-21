<template>
  <fieldset :disabled="disabled" class="min-w-0 space-y-3" data-testid="random-proxy-settings">
    <label class="mt-2 flex cursor-pointer items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
      <input v-model="enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
      <span>{{ t('admin.accounts.randomProxy') }}</span>
    </label>
    <p class="input-hint">{{ t('admin.accounts.randomProxyHint') }}</p>
    <template v-if="enabled">
      <p class="input-hint">{{ t('admin.accounts.randomProxyBalanceHint') }}</p>
      <p class="input-hint">{{ t('admin.accounts.randomProxyMaxReuseHint') }}</p>
      <label class="block">
        <span class="input-label">{{ t('admin.accounts.randomProxyPoolScope') }}</span>
        <select v-model="scope" class="input" data-testid="random-proxy-scope">
          <option value="all">{{ t('admin.accounts.randomProxyPoolAll') }}</option>
          <option value="group">{{ t('accountProxyGroups.scope') }}</option>
          <option value="selected">{{ t('admin.accounts.randomProxyPoolSelected') }}</option>
        </select>
      </label>
      <div v-if="scope === 'group'" class="space-y-2" data-testid="random-proxy-group-settings">
        <label class="block">
          <span class="input-label">{{ t('accountProxyGroups.label') }}</span>
          <select v-model="groupId" class="input" data-testid="random-proxy-group" :disabled="groupsLoading || !!groupsLoadError" :aria-invalid="!!groupError">
            <option :value="null">{{ t('accountProxyGroups.choose') }}</option>
            <option v-if="missingGroup" :value="groupId" disabled>{{ t('accountProxyGroups.unavailableId', { id: groupId }) }}</option>
            <option v-for="group in proxyGroups" :key="group.id" :value="group.id">
              {{ t('accountProxyGroups.option', { name: group.name, count: group.active_proxy_count, total: group.proxy_count }) }}
            </option>
          </select>
        </label>
        <p v-if="groupsLoading" role="status" class="input-hint">{{ t('accountProxyGroups.loading') }}</p>
        <div v-else-if="groupsLoadError" role="alert" class="flex flex-wrap items-center gap-2 text-xs text-red-600 dark:text-red-400">
          <span>{{ t('accountProxyGroups.loadFailed') }}</span>
          <button type="button" class="underline" @click="loadGroups">{{ t('accountProxyGroups.retry') }}</button>
        </div>
        <template v-else>
          <p v-if="!proxyGroups.length" class="input-hint">{{ t('accountProxyGroups.empty') }}</p>
          <p v-if="groupError" role="alert" class="text-xs text-red-600 dark:text-red-400">{{ t(groupError) }}</p>
          <p v-else-if="selectedGroup?.active_proxy_count === 0" class="text-xs text-amber-700 dark:text-amber-400">{{ t('accountProxyGroups.emptyPool') }}</p>
        </template>
        <p class="input-hint">{{ t('accountProxyGroups.dynamicHint') }}</p>
      </div>
      <div v-if="scope === 'selected'" class="space-y-2">
        <label class="block">
          <span class="input-label">{{ t('admin.accounts.randomProxyPoolChoose', { count: ids.length }) }}</span>
          <input v-model="search" type="search" class="input" :placeholder="t('admin.accounts.randomProxyPoolSearch')" />
        </label>
        <div class="max-h-56 overflow-y-auto rounded-lg border border-gray-200 dark:border-dark-600">
          <label v-for="proxy in filteredProxies" :key="proxy.id" class="flex cursor-pointer items-start gap-3 border-b border-gray-100 px-3 py-2 last:border-0 hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-700">
            <input v-model="ids" :value="proxy.id" type="checkbox" class="mt-1 h-4 w-4 shrink-0 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
            <span class="min-w-0 text-sm">
              <span class="block break-all text-gray-800 dark:text-gray-200">{{ proxyOptionLabel(proxy) }}</span>
              <span v-if="location(proxy)" class="block text-xs text-gray-500">{{ location(proxy) }}</span>
            </span>
          </label>
          <p v-if="filteredProxies.length === 0" class="px-3 py-4 text-sm text-gray-500">{{ t('admin.accounts.randomProxyPoolNoMatches') }}</p>
        </div>
        <div v-if="missingIds.length" class="space-y-1 text-xs text-amber-700 dark:text-amber-400">
          <p>{{ t('admin.accounts.randomProxyPoolUnavailable') }}</p>
          <label v-for="id in missingIds" :key="id" class="flex items-center gap-2">
            <input v-model="ids" :value="id" type="checkbox" class="rounded border-gray-300" />
            <span>{{ t('admin.accounts.randomProxyPoolUnavailableItem', { id }) }}</span>
          </label>
        </div>
        <p v-if="ids.length === 0" role="alert" class="text-xs text-red-600 dark:text-red-400">{{ t('admin.accounts.randomProxyPoolRequired') }}</p>
        <p class="input-hint">{{ t('admin.accounts.randomProxyPoolSelectedHint') }}</p>
      </div>
      <label class="block">
        <span class="input-label">{{ t('admin.accounts.randomProxyEmptyPoolPolicy') }}</span>
        <select v-model="policy" class="input">
          <option value="reject">{{ t('admin.accounts.randomProxyEmptyPoolPolicies.reject') }}</option>
          <option value="disable">{{ t('admin.accounts.randomProxyEmptyPoolPolicies.disable') }}</option>
          <option value="direct">{{ t('admin.accounts.randomProxyEmptyPoolPolicies.direct') }}</option>
        </select>
      </label>
      <p class="input-hint">{{ t(`admin.accounts.randomProxyEmptyPoolPolicyHints.${policy}`) }}</p>
    </template>
  </fieldset>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { Proxy, ProxyGroup } from '@/types'
import { type RandomProxyEmptyPoolPolicy, type RandomProxyPoolScope } from '@/utils/randomProxy'
import { proxyOptionLabel } from '@/utils/proxyLabel'

const props = defineProps<{ proxies: Proxy[]; disabled?: boolean }>()
const enabled = defineModel<boolean>('enabled', { required: true })
const scope = defineModel<RandomProxyPoolScope>('scope', { required: true })
const ids = defineModel<number[]>('ids', { required: true })
const groupId = defineModel<number | null>('groupId', { default: null })
const groupError = defineModel<string | null>('groupError', { default: null })
const policy = defineModel<RandomProxyEmptyPoolPolicy>('policy', { required: true })
// 保留旧表单绑定与存储值，健康代理不再按时间轮换。
defineModel<number>('maxReuseMinutes', { default: 0 })
const { t } = useI18n()
const search = ref('')
const proxyGroups = ref<ProxyGroup[]>([])
const groupsLoading = ref(false)
const groupsLoaded = ref(false)
const groupsLoadError = ref(false)
const selectedGroup = computed(() => proxyGroups.value.find(group => group.id === groupId.value))
const missingGroup = computed(() => groupsLoaded.value && groupId.value !== null && !selectedGroup.value)
const groupValidationError = computed(() => {
  if (!enabled.value || scope.value !== 'group') return null
  if (groupsLoading.value || !groupsLoaded.value) return 'accountProxyGroups.loading'
  if (groupsLoadError.value) return 'accountProxyGroups.loadFailed'
  if (groupId.value === null) return 'accountProxyGroups.required'
  return missingGroup.value ? 'accountProxyGroups.unavailable' : null
})

async function loadGroups() {
  if (groupsLoading.value) return
  groupsLoading.value = true
  groupsLoadError.value = false
  try {
    proxyGroups.value = await adminAPI.proxies.listGroups()
  } catch {
    groupsLoadError.value = true
  } finally {
    groupsLoaded.value = true
    groupsLoading.value = false
  }
}

watch(() => enabled.value && scope.value === 'group', active => {
  if (active) void loadGroups()
}, { immediate: true })
watch(groupValidationError, error => { groupError.value = error }, { immediate: true })
const location = (proxy: Proxy) => [...new Set([proxy.country || proxy.country_code, proxy.region, proxy.city].filter(Boolean))].join(' / ')
const filteredProxies = computed(() => {
  const query = search.value.trim().toLowerCase()
  return props.proxies.filter(proxy => `${proxyOptionLabel(proxy)} ${location(proxy)}`.toLowerCase().includes(query))
})
const missingIds = computed(() => ids.value.filter(id => !props.proxies.some(proxy => proxy.id === id)))
</script>
