<template>
  <fieldset :disabled="disabled" class="min-w-0 space-y-3" data-testid="account-proxy-region-settings">
    <legend class="input-label">{{ t('admin.accounts.proxyRegion.title') }}</legend>
    <label class="block">
      <span class="sr-only">{{ t('admin.accounts.proxyRegion.title') }}</span>
      <select :value="modelValue.mode" :disabled="disabled" class="input w-full" data-testid="proxy-region-mode" @change="setMode">
        <option v-for="mode in modes" :key="mode" :value="mode">{{ t(`admin.accounts.proxyRegion.${mode}`) }}</option>
      </select>
    </label>
    <template v-if="modelValue.mode !== 'off'">
      <p class="input-hint">{{ t('admin.accounts.proxyRegion.scopeHint') }}</p>
      <label v-if="modelValue.mode === 'manual'" class="block">
        <span class="input-label">{{ t('admin.accounts.proxyRegion.country') }}</span>
        <select :value="modelValue.country" :disabled="disabled" class="input w-full" data-testid="proxy-region-country" @change="setCountry">
          <option value="">{{ t('admin.accounts.proxyRegion.chooseCountry') }}</option>
          <option v-for="country in countries" :key="country" :value="country">{{ countryLabel(country) }}</option>
        </select>
      </label>
      <label v-if="modelValue.mode === 'billing' && fallbackCountry !== undefined" class="block">
        <span class="input-label">{{ t('admin.accountImportSettings.regionFallbackCountry') }}</span>
        <select :value="fallbackCountry" class="input w-full" data-testid="proxy-region-fallback-country" :disabled="disabled" @change="setFallbackCountry">
          <option value="">{{ t('admin.accountImportSettings.regionFallbackCountryOptional') }}</option>
          <option v-for="country in countries" :key="country" :value="country">{{ countryLabel(country) }}</option>
        </select>
      </label>
      <p v-if="resolution.country" class="input-hint" data-testid="proxy-region-resolution">
        {{ t(sourceKey, { country: countryLabel(resolution.country), currency: resolution.currency }) }}
      </p>
      <p v-else-if="modelValue.mode === 'billing'" role="status" class="text-xs text-amber-700 dark:text-amber-400">
        {{ t(billingPending ? 'admin.accounts.proxyRegion.billingPending' : 'admin.accounts.proxyRegion.unknownBilling', { currency: resolution.currency || '—' }) }}
      </p>
      <p v-if="validationError" role="alert" class="text-xs text-red-600 dark:text-red-400">{{ t(validationError) }}</p>
      <p v-if="resolution.country" class="input-hint">{{ t('admin.accounts.proxyRegion.matchingCount', { count: matchingCount }) }}</p>
      <p v-if="fixedMismatch" role="alert" class="text-xs text-amber-700 dark:text-amber-400">{{ t('admin.accounts.proxyRegion.fixedMismatch') }}</p>
      <p v-if="randomEnabled && emptyPoolPolicy === 'direct'" class="text-xs text-amber-700 dark:text-amber-400">{{ t('admin.accounts.proxyRegion.directFallback') }}</p>
      <p v-else-if="!randomEnabled && proxyId === null" class="text-xs text-amber-700 dark:text-amber-400">{{ t('admin.accounts.proxyRegion.directBlocked') }}</p>
    </template>
  </fieldset>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Proxy } from '@/types'
import { accountProxyRegionValidationError, BILLING_CURRENCY_COUNTRIES, normalizeProxyRegionCountry, resolveAccountProxyRegion, type AccountProxyRegionMode, type AccountProxyRegionSelection } from '@/utils/accountProxyRegion'

const props = defineProps<{
  modelValue: AccountProxyRegionSelection
  credentials?: Record<string, unknown> | null
  proxies: Proxy[]
  proxyId?: number | null
  randomEnabled?: boolean
  emptyPoolPolicy?: string
  billingPending?: boolean
  fallbackCountry?: string
  disabled?: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [value: AccountProxyRegionSelection]; 'update:fallbackCountry': [value: string] }>()
const { t, locale } = useI18n()
const modes: AccountProxyRegionMode[] = ['off', 'billing', 'manual']
const resolution = computed(() => resolveAccountProxyRegion(props.modelValue, props.credentials, props.fallbackCountry))
const validationError = computed(() => accountProxyRegionValidationError(props.modelValue))
const countries = computed(() => [...new Set(['US', 'DE', 'FR', ...Object.values(BILLING_CURRENCY_COUNTRIES), props.modelValue.country, normalizeProxyRegionCountry(props.fallbackCountry), ...props.proxies.map(proxy => normalizeProxyRegionCountry(proxy.country_code))])].filter(Boolean).sort())
const matchingCount = computed(() => props.proxies.filter(proxy => normalizeProxyRegionCountry(proxy.country_code) === resolution.value.country).length)
const fixedMismatch = computed(() => !props.randomEnabled && !!props.proxyId && !!resolution.value.country &&
  normalizeProxyRegionCountry(props.proxies.find(proxy => proxy.id === props.proxyId)?.country_code) !== resolution.value.country)
const sourceKey = computed(() => `admin.accounts.proxyRegion.${({ price_country: 'sourceCountry', billing_currency: 'sourceCurrency', fallback_country: 'sourceFallback' } as Record<string, string>)[resolution.value.source] || 'sourceManual'}`)

function countryLabel(country: string): string {
  const name = new Intl.DisplayNames([locale?.value || 'zh'], { type: 'region' }).of(country)
  return name && name !== country ? `${country} · ${name}` : country
}
function setMode(event: Event) {
  const mode = (event.target as HTMLSelectElement).value as AccountProxyRegionMode
  if (!props.disabled && modes.includes(mode)) emit('update:modelValue', { ...props.modelValue, mode })
}
function setCountry(event: Event) {
  if (!props.disabled) emit('update:modelValue', { ...props.modelValue, country: (event.target as HTMLSelectElement).value })
}
function setFallbackCountry(event: Event) {
  if (!props.disabled) emit('update:fallbackCountry', (event.target as HTMLSelectElement).value)
}
</script>
