<template>
  <fieldset :disabled="disabled" class="min-w-0 space-y-3" data-testid="random-proxy-settings">
    <label class="mt-2 flex cursor-pointer items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
      <input v-model="enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
      <span>{{ t('admin.accounts.randomProxy') }}</span>
    </label>
    <p class="input-hint">{{ t('admin.accounts.randomProxyHint') }}</p>
    <template v-if="enabled">
      <p class="input-hint">{{ t('admin.accounts.randomProxyBalanceHint') }}</p>
      <label class="block">
        <span class="input-label">{{ t('admin.accounts.randomProxyMaxReuseMinutes') }}</span>
        <input v-model.number="maxReuseMinutes" type="number" min="0" max="525600" step="1" required class="input" data-testid="random-proxy-reuse-minutes" />
      </label>
      <p class="input-hint">{{ t('admin.accounts.randomProxyMaxReuseHint') }}</p>
      <p v-if="!isValidRandomProxyReuseMinutes(maxReuseMinutes)" role="alert" class="text-xs text-red-600 dark:text-red-400">{{ t('admin.accounts.randomProxyMaxReuseInvalid') }}</p>
      <label class="block">
        <span class="input-label">{{ t('admin.accounts.randomProxyPoolScope') }}</span>
        <select v-model="scope" class="input" data-testid="random-proxy-scope">
          <option value="all">{{ t('admin.accounts.randomProxyPoolAll') }}</option>
          <option value="selected">{{ t('admin.accounts.randomProxyPoolSelected') }}</option>
        </select>
      </label>
      <div v-if="scope === 'selected'" class="space-y-2">
        <label class="block">
          <span class="input-label">{{ t('admin.accounts.randomProxyPoolChoose', { count: ids.length }) }}</span>
          <input v-model="search" type="search" class="input" :placeholder="t('admin.accounts.randomProxyPoolSearch')" />
        </label>
        <div class="max-h-56 overflow-y-auto rounded-lg border border-gray-200 dark:border-dark-600">
          <label v-for="proxy in filteredProxies" :key="proxy.id" class="flex cursor-pointer items-start gap-3 border-b border-gray-100 px-3 py-2 last:border-0 hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-700">
            <input v-model="ids" :value="proxy.id" type="checkbox" class="mt-1 h-4 w-4 shrink-0 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
            <span class="min-w-0 text-sm">
              <span class="block break-words text-gray-800 dark:text-gray-200">{{ proxy.name }}</span>
              <span class="block break-all text-xs text-gray-500">{{ proxy.protocol }}://{{ randomProxyAddress(proxy) }}</span>
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
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Proxy } from '@/types'
import { randomProxyAddress, isValidRandomProxyReuseMinutes, type RandomProxyEmptyPoolPolicy, type RandomProxyPoolScope } from '@/utils/randomProxy'

const props = defineProps<{ proxies: Proxy[]; disabled?: boolean }>()
const enabled = defineModel<boolean>('enabled', { required: true })
const scope = defineModel<RandomProxyPoolScope>('scope', { required: true })
const ids = defineModel<number[]>('ids', { required: true })
const policy = defineModel<RandomProxyEmptyPoolPolicy>('policy', { required: true })
const maxReuseMinutes = defineModel<number>('maxReuseMinutes', { default: 0 })
const { t } = useI18n()
const search = ref('')
const location = (proxy: Proxy) => [...new Set([proxy.country || proxy.country_code, proxy.region, proxy.city].filter(Boolean))].join(' / ')
const filteredProxies = computed(() => {
  const query = search.value.trim().toLowerCase()
  return props.proxies.filter(proxy => `${proxy.name} ${proxy.host} ${proxy.port} ${location(proxy)}`.toLowerCase().includes(query))
})
const missingIds = computed(() => ids.value.filter(id => !props.proxies.some(proxy => proxy.id === id)))
</script>
