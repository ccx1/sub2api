<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-6">
      <header class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('codexModelQuality.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('codexModelQuality.pageDescription') }}</p>
        </div>
        <RouterLink to="/admin/codex-ticket-settings" class="btn btn-secondary">{{ t('codexModelQuality.ticketSettings') }}</RouterLink>
      </header>
      <p v-if="loading" role="status" class="py-12 text-center text-sm text-gray-500">{{ t('codexModelQuality.loading') }}</p>
      <div v-else-if="loadError" role="alert" class="card flex flex-wrap items-center justify-between gap-3 p-5">
        <p class="text-sm text-red-600 dark:text-red-400">{{ loadError }}</p>
        <button type="button" class="btn btn-secondary" data-testid="load-retry" @click="load">{{ t('codexModelQuality.reload') }}</button>
      </div>
      <CodexModelQuality v-else :models="models" />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import CodexModelQuality from '@/components/admin/CodexModelQuality.vue'
import { getCodexTicketSettings } from '@/api/admin/codexTicketSettings'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const loading = ref(true)
const loadError = ref('')
const models = ref<string[]>([])

// 检测目标模型沿用已保存的打票配置，页面只读取、不修改打票设置。
async function load() {
  loading.value = true
  loadError.value = ''
  try { models.value = [...(await getCodexTicketSettings()).models] }
  catch (error) { loadError.value = extractApiErrorMessage(error, t('codexTicketSettings.loadFailed')) }
  finally { loading.value = false }
}
onMounted(load)
</script>
