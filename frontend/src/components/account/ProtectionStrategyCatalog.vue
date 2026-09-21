<template>
  <section aria-labelledby="protection-catalog-title" class="space-y-4">
    <div>
      <h2 id="protection-catalog-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('accountProtection.catalogTitle') }}</h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('accountProtection.catalogHint') }}</p>
    </div>
    <div class="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
      <article v-for="strategy in strategies" :key="strategy.id" class="card min-w-0 space-y-4 p-4" :class="{ 'ring-1 ring-primary-500/50': strategy.id === defaultMode }">
        <div>
          <div class="flex flex-wrap items-center gap-2">
            <h3 class="font-semibold text-gray-900 dark:text-white">{{ strategy.name }}</h3>
            <span v-if="strategy.id === defaultMode" class="rounded px-2 py-0.5 text-xs text-primary-600 bg-primary-50 dark:bg-primary-900/30 dark:text-primary-300">{{ t('accountProtection.defaultBadge') }}</span>
            <span v-if="!strategy.apply_supported" class="rounded bg-gray-100 px-2 py-0.5 text-xs text-gray-600 dark:bg-dark-700 dark:text-gray-300">{{ t('accountProtection.previewOnly') }}</span>
          </div>
          <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">{{ strategy.description }}</p>
        </div>
        <dl class="space-y-2 text-sm">
          <div class="flex justify-between gap-3"><dt class="text-gray-500 dark:text-gray-400">{{ t('accountProtection.identity') }}</dt><dd class="text-right text-gray-900 dark:text-gray-200">{{ label('identityModes', strategy.identity_mode) }}</dd></div>
          <div class="flex justify-between gap-3"><dt class="text-gray-500 dark:text-gray-400">{{ t('accountProtection.tls') }}</dt><dd class="text-right text-gray-900 dark:text-gray-200">{{ label('tlsModes', strategy.tls_profile) }}</dd></div>
          <div class="flex justify-between gap-3"><dt class="text-gray-500 dark:text-gray-400">{{ t('accountProtection.concurrency') }}</dt><dd class="text-right text-gray-900 dark:text-gray-200">{{ strategy.max_concurrency || t('accountProtection.unchanged') }}</dd></div>
          <div class="border-t border-gray-100 pt-2 dark:border-dark-700"><dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('accountProtection.applicability') }}</dt><dd class="mt-1 text-gray-700 dark:text-gray-300">{{ t(strategy.requires_openai_oauth ? 'accountProtection.openaiOnly' : 'accountProtection.compatibleAccounts') }}</dd></div>
        </dl>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { ProtectionStrategy } from '@/api/admin/accountProtection'

defineProps<{ strategies: ProtectionStrategy[]; defaultMode: string }>()
const { t, te } = useI18n()
function label(section: string, value: string) {
  const key = `accountProtection.${section}.${value}`
  return te(key) ? t(key) : value
}
</script>
