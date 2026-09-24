<template>
  <BaseDialog
    :show="show"
    :title="t('admin.proxies.ipStatusTitle')"
    width="extra-wide"
    @close="handleClose"
  >
    <div class="space-y-4">
      <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <p class="text-sm text-gray-500 dark:text-gray-400">
          {{ t('admin.proxies.ipStatusDescription') }}
        </p>
        <div class="flex shrink-0 items-center gap-2">
          <div class="w-40">
            <Select
              v-model="statusFilter"
              :options="statusFilterOptions"
              :aria-label="t('admin.proxies.ipStatusFilter')"
              data-testid="codex-ip-status-filter"
              :disabled="loading"
              @change="handleFilterChange"
            />
          </div>
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="loading"
            :title="t('common.refresh')"
            data-testid="codex-ip-status-refresh"
            @click="load"
          >
            <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
            <span class="sr-only">{{ t('common.refresh') }}</span>
          </button>
        </div>
      </div>

      <div class="text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.proxies.ipStatusCount', { count: pagination.total }) }}
      </div>

      <div v-if="errorMessage" role="alert" class="flex items-center justify-between gap-3 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-600 dark:border-red-900/60 dark:bg-red-950/20 dark:text-red-400">
        <span>{{ errorMessage }}</span>
        <button type="button" class="btn btn-secondary shrink-0" @click="load">{{ t('common.refresh') }}</button>
      </div>

      <div v-else-if="loading" class="flex items-center justify-center py-12 text-sm text-gray-500 dark:text-gray-400">
        <Icon name="refresh" size="md" class="mr-2 animate-spin" />
        {{ t('common.loading') }}
      </div>

      <div v-else-if="items.length === 0" class="py-12 text-center text-sm text-gray-500 dark:text-gray-400">
        {{ t('admin.proxies.ipStatusEmpty') }}
      </div>

      <div v-else class="max-h-[min(60vh,32rem)] overflow-auto rounded-lg border border-gray-200 dark:border-dark-700">
        <table class="min-w-full text-left text-sm">
          <thead class="sticky top-0 z-[1] border-b border-gray-200 bg-gray-50 text-xs text-gray-500 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-400">
            <tr>
              <th class="whitespace-nowrap px-3 py-2 font-medium">{{ t('admin.proxies.ipStatusColumns.ip') }}</th>
              <th class="whitespace-nowrap px-3 py-2 font-medium">{{ t('admin.proxies.ipStatusColumns.status') }}</th>
              <th class="whitespace-nowrap px-3 py-2 font-medium">{{ t('admin.proxies.ipStatusColumns.until') }}</th>
              <th class="whitespace-nowrap px-3 py-2 font-medium">{{ t('admin.proxies.ipStatusColumns.rounds') }}</th>
              <th class="whitespace-nowrap px-3 py-2 font-medium">{{ t('admin.proxies.ipStatusColumns.failedAccounts') }}</th>
              <th class="whitespace-nowrap px-3 py-2 font-medium">{{ t('admin.proxies.ipStatusColumns.lastFailure') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
            <tr v-for="item in items" :key="`${item.ip}-${item.status}`">
              <td class="whitespace-nowrap px-3 py-2 font-mono text-gray-800 dark:text-gray-200">{{ item.ip || '-' }}</td>
              <td class="px-3 py-2">
                <span :class="['badge', item.status === 'disabled' ? 'badge-danger' : 'badge-warning']">
                  {{ item.status === 'disabled' ? t('admin.proxies.ipStatusDisabled') : t('admin.proxies.ipStatusCooling') }}
                </span>
              </td>
              <td class="whitespace-nowrap px-3 py-2 text-gray-600 dark:text-gray-300">
                {{ item.until_at ? formatDateTime(item.until_at) : '-' }}
              </td>
              <td class="px-3 py-2 text-gray-600 dark:text-gray-300">{{ item.rounds }}</td>
              <td class="px-3 py-2 text-gray-600 dark:text-gray-300">{{ item.failed_accounts }}</td>
              <td class="whitespace-nowrap px-3 py-2 text-gray-600 dark:text-gray-300">
                {{ item.last_failure_at ? formatDateTime(item.last_failure_at) : '-' }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <Pagination
        v-if="pagination.total > 0"
        :page="pagination.page"
        :total="pagination.total"
        :page-size="pagination.page_size"
        :show-page-size-selector="false"
        @update:page="handlePageChange"
      />
    </div>

    <template #footer>
      <div class="flex justify-end">
        <button type="button" class="btn btn-secondary" @click="handleClose">{{ t('common.close') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import { listCodexIPStatus, type CodexProxyIPStatus, type CodexProxyIPStatusKind } from '@/api/admin/proxies'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ (event: 'close'): void }>()
const { t } = useI18n()

const loading = ref(false)
const errorMessage = ref('')
const items = ref<CodexProxyIPStatus[]>([])
const statusFilter = ref<CodexProxyIPStatusKind | ''>('')
const pagination = reactive({ page: 1, page_size: 10, total: 0, pages: 0 })
const statusFilterOptions = computed(() => [
  { value: '', label: t('admin.proxies.ipStatusAll') },
  { value: 'cooling' as const, label: t('admin.proxies.ipStatusCooling') },
  { value: 'disabled' as const, label: t('admin.proxies.ipStatusDisabled') }
])

let currentController: AbortController | null = null

const isAbortError = (error: unknown) => {
  if (!error || typeof error !== 'object') return false
  const value = error as { name?: string; code?: string }
  return value.name === 'AbortError' || value.code === 'ERR_CANCELED'
}

async function load() {
  if (!props.show) return

  currentController?.abort()
  const controller = new AbortController()
  currentController = controller
  loading.value = true
  errorMessage.value = ''

  try {
    const result = await listCodexIPStatus({
      status: statusFilter.value || undefined,
      page: pagination.page,
      pageSize: pagination.page_size,
      signal: controller.signal
    })
    if (controller.signal.aborted || currentController !== controller) return
    items.value = result.items
    pagination.page = result.page
    pagination.page_size = result.page_size
    pagination.total = result.total
    pagination.pages = result.pages
  } catch (error: any) {
    if (controller.signal.aborted || currentController !== controller || isAbortError(error)) return
    errorMessage.value = error?.response?.data?.detail || t('admin.proxies.ipStatusLoadFailed')
    items.value = []
    pagination.total = 0
    pagination.pages = 0
  } finally {
    if (currentController === controller) {
      currentController = null
      loading.value = false
    }
  }
}

function handleFilterChange() {
  pagination.page = 1
  void load()
}

function handlePageChange(page: number) {
  pagination.page = page
  void load()
}

function resetState() {
  currentController?.abort()
  currentController = null
  loading.value = false
  errorMessage.value = ''
  items.value = []
  statusFilter.value = ''
  pagination.page = 1
  pagination.total = 0
  pagination.pages = 0
}

function handleClose() {
  resetState()
  emit('close')
}

watch(
  () => props.show,
  (show) => {
    if (!show) {
      resetState()
      return
    }
    pagination.page = 1
    void load()
  },
  { immediate: true }
)

onUnmounted(() => {
  currentController?.abort()
})
</script>
