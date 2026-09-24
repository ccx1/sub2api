<template>
  <fieldset :disabled="disabled" class="min-w-0 space-y-5" data-testid="import-settings-fields">
    <div class="grid gap-4 sm:grid-cols-2">
      <div class="flex items-center justify-between gap-3">
        <span id="import-default-protection" class="text-sm">{{ t('admin.accounts.dataImportProtection') }}</span>
        <Toggle v-model="settings.protection_enabled" :disabled="disabled" aria-labelledby="import-default-protection" />
      </div>
      <div class="flex items-center justify-between gap-3">
        <span id="import-default-ticket" class="text-sm">{{ t('admin.accounts.dataImportCodexTicket') }}</span>
        <Toggle v-model="settings.codex_ticket_enabled" :disabled="disabled" aria-labelledby="import-default-ticket" />
      </div>
    </div>
    <p class="input-hint">{{ t('admin.accountImportSettings.protectionHint') }}</p>
    <label class="block">
      <span class="input-label">{{ t('admin.accountImportSettings.proxyMode') }}</span>
      <select v-model="settings.proxy_mode" class="input w-full" data-testid="import-settings-proxy-mode" :disabled="disabled">
        <option v-for="mode in proxyModes" :key="mode" :value="mode">{{ t(`admin.accountImportSettings.proxyModes.${mode}`) }}</option>
      </select>
    </label>
    <ProxySelector v-if="settings.proxy_mode === 'fixed'" v-model="settings.proxy_id" :proxies="availableProxies" :allow-direct="false" :disabled="disabled" :aria-label="t('admin.accountImportSettings.proxyMode')" />
    <RandomProxySettings v-if="settings.proxy_mode === 'random'" v-model:enabled="randomEnabled" v-model:scope="randomScope" v-model:ids="randomIds" v-model:group-id="randomGroupId" v-model:group-error="groupError" v-model:policy="randomPolicy" v-model:region-fallback="randomRegionFallback" v-model:max-reuse-minutes="randomReuseMinutes" :proxies="proxies" :disabled="disabled" :region-country="regionCountry" />
    <div class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-700">
      <label class="flex items-center gap-2 text-sm">
        <input v-model="regionConfigured" type="checkbox" data-testid="import-settings-region-configured" :disabled="disabled" />
        {{ t('admin.accountImportSettings.regionConfigured') }}
      </label>
      <p v-if="!regionConfigured" class="input-hint">{{ t('admin.accountImportSettings.regionPreserveHint') }}</p>
      <p v-else-if="region.mode === 'billing'" class="input-hint">{{ t('admin.accountImportSettings.regionFallbackHint') }}</p>
      <label v-if="regionConfigured && region.mode === 'billing'" class="block">
        <span class="input-label">{{ t('admin.accountImportSettings.regionFallbackCountry') }}</span>
        <select v-model="regionFallbackCountry" class="input w-full" data-testid="import-settings-region-fallback-country" :disabled="disabled">
          <option value="">{{ t('admin.accountImportSettings.regionFallbackCountryOptional') }}</option>
          <option v-for="country in fallbackCountries" :key="country" :value="country">{{ country }}</option>
        </select>
      </label>
      <AccountProxyRegionSettings v-model="region" :proxies="proxies" :proxy-id="settings.proxy_mode === 'fixed' ? settings.proxy_id : undefined" :random-enabled="randomEnabled" :empty-pool-policy="randomPolicy" billing-pending :disabled="disabled" />
    </div>
    <div class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-700">
      <label class="flex items-center gap-2 text-sm">
        <input v-model="ticketConfigured" type="checkbox" data-testid="import-settings-ticket-configured" :disabled="disabled" />
        {{ t('admin.accountImportSettings.ticketConfigured') }}
      </label>
      <p class="input-hint">{{ t('admin.accountImportSettings.ticketHint') }}</p>
      <CodexTicketProxySettings v-if="ticketConfigured" v-model="ticket" :proxies="proxies" :region-enabled="regionConfigured && region.mode !== 'off'" :region-country="regionCountry" :disabled="disabled" />
    </div>
  </fieldset>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Proxy } from '@/types'
import type { AccountImportSettings } from '@/api/admin/accountImportSettings'
import Toggle from '@/components/common/Toggle.vue'
import ProxySelector from '@/components/common/ProxySelector.vue'
import RandomProxySettings from '@/components/account/RandomProxySettings.vue'
import AccountProxyRegionSettings from '@/components/account/AccountProxyRegionSettings.vue'
import CodexTicketProxySettings from '@/components/account/CodexTicketProxySettings.vue'
import { accountProxyRegionExtra, accountProxyRegionValidationError, BILLING_CURRENCY_COUNTRIES, filterProxiesByRegion, normalizeProxyRegionCountry, readAccountProxyRegion, resolveAccountProxyRegion } from '@/utils/accountProxyRegion'
import { codexTicketProxyExtra, codexTicketProxyValidationError, isAvailableCodexTicketProxy, readCodexTicketProxy } from '@/utils/codexTicketProxy'
import { isValidRandomProxyReuseMinutes, normalizeRandomProxyEmptyPoolPolicy, normalizeRandomProxyGroupId, normalizeRandomProxyPoolIds, normalizeRandomProxyPoolScope, normalizeRandomProxyRegionFallback, type RandomProxyRegionFallback } from '@/utils/randomProxy'

const props = defineProps<{ proxies: Proxy[]; disabled?: boolean }>()
const settings = defineModel<AccountImportSettings>({ required: true })
const validationError = defineModel<string | null>('validationError', { default: null })
const { t } = useI18n()
const proxyModes = ['preserve', 'direct', 'fixed', 'random'] as const
const groupError = ref<string | null>(null)
const updateExtra = (patch: Record<string, unknown>) => { settings.value.extra = { ...settings.value.extra, ...patch } }
function clearExtra(prefix: string) {
  const extra = { ...settings.value.extra }
  for (const key of Object.keys(extra)) if (key.startsWith(prefix)) delete extra[key]
  settings.value.extra = extra
}
const randomEnabled = computed({
  get: () => settings.value.proxy_mode === 'random',
  set: value => { if (!props.disabled) settings.value.proxy_mode = value ? 'random' : 'preserve' }
})
const randomScope = computed({
  get: () => normalizeRandomProxyPoolScope(settings.value.extra.random_proxy_pool_scope),
  set: value => updateExtra({ random_proxy_pool_scope: value })
})
const randomIds = computed({
  get: () => normalizeRandomProxyPoolIds(settings.value.extra.random_proxy_pool_ids),
  set: value => updateExtra({ random_proxy_pool_ids: value })
})
const randomGroupId = computed({
  get: () => normalizeRandomProxyGroupId(settings.value.extra.random_proxy_group_id),
  set: value => updateExtra({ random_proxy_group_id: value })
})
const randomPolicy = computed({
  get: () => normalizeRandomProxyEmptyPoolPolicy(settings.value.extra.random_proxy_empty_pool_policy),
  set: value => updateExtra({ random_proxy_empty_pool_policy: value })
})
const randomRegionFallback = computed<RandomProxyRegionFallback>({
  get: () => normalizeRandomProxyRegionFallback(settings.value.extra.random_proxy_region_fallback),
  set: value => updateExtra({ random_proxy_region_fallback: value })
})
const randomReuseMinutes = computed({
  get: () => settings.value.extra.random_proxy_max_reuse_minutes === undefined ? 0 : settings.value.extra.random_proxy_max_reuse_minutes as number,
  set: value => updateExtra({ random_proxy_max_reuse_minutes: value })
})
const region = computed({
  get: () => readAccountProxyRegion(settings.value.extra),
  set: value => {
    updateExtra(accountProxyRegionExtra(value))
    if (value.mode !== 'billing') clearExtra('proxy_region_fallback_country')
  }
})
const regionConfigured = computed({
  get: () => typeof settings.value.extra.proxy_region_mode === 'string',
  set: value => { if (value) updateExtra(accountProxyRegionExtra({ mode: 'off', country: '' })); else clearExtra('proxy_region_') }
})
const regionFallbackCountry = computed({
  get: () => normalizeProxyRegionCountry(settings.value.extra.proxy_region_fallback_country),
  set: value => updateExtra({ proxy_region_fallback_country: normalizeProxyRegionCountry(value) })
})
const fallbackCountries = computed(() => [...new Set([
  'US', 'DE', 'FR', ...Object.values(BILLING_CURRENCY_COUNTRIES), regionFallbackCountry.value,
  ...props.proxies.map(proxy => normalizeProxyRegionCountry(proxy.country_code))
])].filter(Boolean).sort())
const regionCountry = computed(() => {
  if (!regionConfigured.value) return ''
  const resolved = resolveAccountProxyRegion(region.value).country
  return resolved || (region.value.mode === 'billing' ? regionFallbackCountry.value : '')
})
const availableProxies = computed(() => filterProxiesByRegion(props.proxies.filter(isAvailableCodexTicketProxy), regionCountry.value, settings.value.proxy_id))
const ticket = computed({ get: () => readCodexTicketProxy(settings.value.extra), set: value => updateExtra(codexTicketProxyExtra(value)) })
const ticketConfigured = computed({
  get: () => typeof settings.value.extra.codex_ticket_proxy_mode === 'string',
  set: value => { if (value) updateExtra(codexTicketProxyExtra(readCodexTicketProxy())); else clearExtra('codex_ticket_proxy_') }
})
const error = computed(() => {
  if (settings.value.proxy_mode === 'fixed' && !availableProxies.value.some(proxy => proxy.id === settings.value.proxy_id)) return 'admin.accountImportSettings.fixedProxyRequired'
  if (randomEnabled.value && !isValidRandomProxyReuseMinutes(randomReuseMinutes.value)) return 'admin.accounts.randomProxyMaxReuseInvalid'
  if (randomEnabled.value && randomScope.value === 'selected' && !randomIds.value.length) return 'admin.accounts.randomProxyPoolRequired'
  if (randomEnabled.value && randomScope.value === 'group' && (groupError.value || !randomGroupId.value)) return groupError.value || 'accountProxyGroups.required'
  if (regionConfigured.value && accountProxyRegionValidationError(region.value)) return accountProxyRegionValidationError(region.value)
  return ticketConfigured.value ? codexTicketProxyValidationError(ticket.value, props.proxies) : null
})
watch(error, value => { validationError.value = value }, { immediate: true })
</script>
