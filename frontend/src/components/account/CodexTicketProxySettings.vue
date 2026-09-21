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
    </template>
  </fieldset>
</template>

<script setup lang="ts">
import { computed, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Proxy } from '@/types'
import ProxySelector from '@/components/common/ProxySelector.vue'
import { codexTicketProxyValidationError, isAvailableCodexTicketProxy, type CodexTicketProxyMode, type CodexTicketProxySelection } from '@/utils/codexTicketProxy'

const props = withDefaults(defineProps<{
  modelValue: CodexTicketProxySelection
  proxies: Proxy[]
  disabled?: boolean
}>(), { disabled: false })
const emit = defineEmits<{ 'update:modelValue': [value: CodexTicketProxySelection] }>()
const { t } = useI18n()
const groupName = useId()
const modes: CodexTicketProxyMode[] = ['account', 'inherit', 'random', 'fixed']
const availableProxies = computed(() => props.proxies.filter(isAvailableCodexTicketProxy))
const error = computed(() => codexTicketProxyValidationError(props.modelValue, props.proxies))

function setMode(mode: CodexTicketProxyMode) {
  if (!props.disabled) emit('update:modelValue', { ...props.modelValue, mode })
}

function setProxy(proxyId: number | null) {
  if (props.disabled || !availableProxies.value.some(proxy => proxy.id === proxyId)) return
  emit('update:modelValue', { ...props.modelValue, proxyId })
}
</script>
