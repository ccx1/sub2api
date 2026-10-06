<template>
  <AppLayout>
    <div class="mx-auto min-w-0 max-w-6xl space-y-6">
      <header class="flex flex-wrap items-start justify-between gap-3">
        <div class="min-w-0">
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('admin.astraGateway.policyTitle') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.astraGateway.policyDescription') }}</p>
        </div>
        <RouterLink to="/admin/codex-ticket-settings" class="btn btn-secondary">{{ t('codexTicketSettings.title') }}</RouterLink>
      </header>
      <HarvestGatewayBorrowPanel />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import HarvestGatewayBorrowPanel from '@/components/admin/HarvestGatewayBorrowPanel.vue'
const { t } = useI18n()
</script>
