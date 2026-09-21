<template>
  <div class="mt-3 rounded-lg border border-cyan-500/20 bg-cyan-500/5 px-3 py-2 text-sm" data-testid="protection-managed-notice">
    <p class="font-medium text-gray-900 dark:text-gray-100">{{ t('accountProtection.managedTitle') }}</p>
    <p class="mt-1 text-xs text-gray-600 dark:text-gray-400">{{ t('accountProtection.managedDescription') }}</p>
    <a :href="`/admin/account-protection?account_id=${accountId}`" class="mt-2 inline-block font-medium text-primary-600 hover:underline dark:text-primary-400">
      {{ t('accountProtection.manageAction') }}
    </a>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{ accountId: number }>()
const { t } = useI18n()
</script>
