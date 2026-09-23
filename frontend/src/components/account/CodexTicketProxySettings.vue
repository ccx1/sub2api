<template>
  <fieldset :disabled="disabled" class="space-y-3" data-testid="codex-ticket-proxy-settings">
    <legend class="input-label">{{ t('admin.accounts.codexTicketProxy.title') }}</legend>
    <div class="flex flex-wrap gap-x-5 gap-y-2">
      <label v-for="mode in modes" :key="mode" class="flex cursor-pointer items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
        <input
          type="radio"
          :name="groupName"
          :value="mode"
          :checked="modelValue.mode === mode"
          :disabled="disabled"
          :data-testid="`codex-ticket-proxy-${mode}`"
          class="text-primary-600 focus:ring-primary-500"
          @change="setMode(mode)"
        />
        {{ t(`admin.accounts.codexTicketProxy.${mode}`) }}
      </label>
    </div>
    <p class="input-hint">{{ t(`admin.accounts.codexTicketProxy.${modelValue.mode}Hint`) }}</p>
    <p v-if="regionEnabled" class="input-hint">{{ t('admin.accounts.proxyRegion.ticketHint') }}</p>
    <label v-if="modelValue.mode === 'random' || modelValue.mode === 'inherit'" class="block space-y-1">
      <span class="input-label">{{ t('admin.accounts.codexTicketProxy.strategy') }}</span>
      <select :value="modelValue.strategy ?? 'affinity'" :disabled="disabled" class="input w-full" data-testid="codex-ticket-proxy-strategy" @change="setStrategy">
        <option value="affinity">{{ t('admin.accounts.codexTicketProxy.affinity') }}</option>
        <option value="round_robin">{{ t('admin.accounts.codexTicketProxy.roundRobin') }}</option>
      </select>
      <span class="input-hint block">{{ t('admin.accounts.codexTicketProxy.strategyHint') }}</span>
    </label>
    <template v-if="modelValue.mode === 'fixed'">
      <ProxySelector
        :model-value="modelValue.proxyId"
        :proxies="availableProxies"
        :disabled="disabled"
        :allow-direct="false"
        :aria-label="t('admin.accounts.codexTicketProxy.title')"
        @update:model-value="setProxy"
      />
      <p v-if="error" role="alert" class="text-sm text-amber-600 dark:text-amber-400">
        {{ t(error) }}<span v-if="modelValue.proxyId"> (ID: {{ modelValue.proxyId }})</span>
      </p>
      <p v-if="regionMismatch" role="alert" class="text-xs text-amber-700 dark:text-amber-400">{{ t('admin.accounts.proxyRegion.fixedMismatch') }}</p>
    </template>
  </fieldset>
</template>

<script setup lang="ts">
import { computed, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Proxy } from '@/types'
import ProxySelector from '@/components/common/ProxySelector.vue'
import { codexTicketProxyValidationError, isAvailableCodexTicketProxy, type CodexTicketProxyMode, type CodexTicketProxySelection } from '@/utils/codexTicketProxy'
import { filterProxiesByRegion, normalizeProxyRegionCountry } from '@/utils/accountProxyRegion'

const props = withDefaults(defineProps<{
  modelValue: CodexTicketProxySelection
  proxies: Proxy[]
  disabled?: boolean
  regionEnabled?: boolean
  regionCountry?: string
}>(), { disabled: false })
const emit = defineEmits<{ 'update:modelValue': [value: CodexTicketProxySelection] }>()
const { t } = useI18n()
const groupName = useId()
const modes: CodexTicketProxyMode[] = ['account', 'inherit', 'random', 'fixed']
const availableProxies = computed(() => filterProxiesByRegion(props.proxies.filter(isAvailableCodexTicketProxy), props.regionEnabled ? props.regionCountry || '' : '', props.modelValue.proxyId))
const error = computed(() => codexTicketProxyValidationError(props.modelValue, props.proxies))
const regionMismatch = computed(() => props.regionEnabled && !!props.regionCountry && !!props.modelValue.proxyId &&
  normalizeProxyRegionCountry(props.proxies.find(proxy => proxy.id === props.modelValue.proxyId)?.country_code) !== props.regionCountry)

function setMode(mode: CodexTicketProxyMode) {
  if (!props.disabled) emit('update:modelValue', { ...props.modelValue, mode })
}

function setProxy(proxyId: number | null) {
  if (props.disabled || !availableProxies.value.some(proxy => proxy.id === proxyId)) return
  emit('update:modelValue', { ...props.modelValue, proxyId })
}

function setStrategy(event: Event) {
  const strategy = (event.target as HTMLSelectElement).value
  if (!props.disabled && (strategy === 'affinity' || strategy === 'round_robin')) emit('update:modelValue', { ...props.modelValue, strategy })
}
</script>
