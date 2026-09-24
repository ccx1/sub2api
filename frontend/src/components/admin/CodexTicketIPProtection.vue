<template>
  <section class="card min-w-0 space-y-4 p-5" data-testid="ip-protection-settings" aria-labelledby="ticket-ip-protection-title">
    <h2 id="ticket-ip-protection-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('codexTicketSettings.ipProtectionTitle') }}</h2>
    <label class="flex items-start gap-3">
      <input v-model="settings.proxy_ip_protection_enabled" type="checkbox" data-testid="proxy_ip_protection_enabled" :disabled="disabled" aria-describedby="ticket-ip-protection-hint" class="mt-1 h-4 w-4" />
      <span class="text-sm text-gray-900 dark:text-white">{{ t('codexTicketSettings.ipProtectionEnabled') }}</span>
    </label>
    <p id="ticket-ip-protection-hint" class="text-sm text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.ipProtectionHint') }}</p>
    <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.ipProtectionIdentityHint') }}</p>
    <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.ipProtectionPersistenceHint') }}</p>
    <fieldset :disabled="disabled || !settings.proxy_ip_protection_enabled" class="grid min-w-0 gap-4 sm:grid-cols-2">
      <label v-for="field in ticketIPProtectionFields" :key="field.key" class="min-w-0 space-y-1">
        <span class="input-label">{{ t(`codexTicketSettings.fields.${field.key}`) }}</span>
        <input v-model.number="settings[field.key]" :data-testid="field.key" type="number" :min="field.min" :max="field.max" step="1" class="input w-full" />
        <span class="block text-xs text-gray-500 dark:text-gray-400">{{ t('codexTicketSettings.range', { min: field.min, max: field.max }) }}</span>
      </label>
    </fieldset>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { CodexTicketProtectionSettings } from '@/api/admin/codexTicketSettings'
import { ticketIPProtectionFields } from './codexTicketSettingsForm'

defineProps<{ disabled?: boolean }>()
const settings = defineModel<Required<CodexTicketProtectionSettings>>({ required: true })
const { t } = useI18n()
</script>
