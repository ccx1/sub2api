<template>
  <AppLayout>
    <TablePageLayout>
      <template #actions>
        <div class="flex flex-col gap-3 rounded-lg border border-primary-200 bg-primary-50/80 p-4 dark:border-primary-800 dark:bg-primary-900/20 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">快速开始教程</h2>
            <p class="mt-1 text-sm text-gray-600 dark:text-gray-300">小白用户请直接打开金山文档，按照里面的步骤做。</p>
          </div>
          <a
            :href="quickStartGuideUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="btn btn-primary w-full justify-center sm:w-auto"
          >
            <Icon name="externalLink" size="sm" />
            <span>打开金山文档</span>
          </a>
        </div>
      </template>

      <template #filters>
        <div class="flex flex-col justify-between gap-4 lg:flex-row lg:items-start">
          <div class="flex flex-1 flex-wrap items-center gap-3">
            <div class="relative w-full sm:w-80">
              <Icon
                name="search"
                size="md"
                class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 dark:text-gray-500"
              />
              <input
                v-model="searchQuery"
                type="text"
                :placeholder="t('availableChannels.searchPlaceholder')"
                class="input pl-10"
              />
            </div>
          </div>

          <div class="flex w-full flex-shrink-0 flex-wrap items-center justify-end gap-3 lg:w-auto">
            <button
              @click="loadChannels"
              :disabled="loading"
              class="btn btn-secondary"
              :title="t('common.refresh', 'Refresh')"
            >
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <div class="h-full overflow-hidden bg-white dark:bg-dark-800">
          <div v-if="loading" class="flex h-full items-center justify-center py-16">
            <Icon name="refresh" size="lg" class="animate-spin text-gray-400" />
          </div>

          <div v-else-if="filteredChannels.length === 0" class="flex h-full items-center justify-center px-6 py-16 text-center">
            <div>
              <Icon name="inbox" size="xl" class="mx-auto mb-3 h-12 w-12 text-gray-400" />
              <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('availableChannels.empty') }}</p>
            </div>
          </div>

          <div
            v-else
            class="grid h-full min-h-0 grid-cols-1 transition-[grid-template-columns] duration-200"
            :class="channelPanelCollapsed ? 'lg:grid-cols-[72px_minmax(0,1fr)]' : 'lg:grid-cols-[280px_minmax(0,1fr)]'"
          >
            <aside class="min-h-0 border-b border-gray-200 bg-gray-50/70 dark:border-dark-700 dark:bg-dark-900/40 lg:border-b-0 lg:border-r">
              <div class="flex items-center justify-between gap-2 border-b border-gray-200 px-4 py-3 dark:border-dark-700">
                <h2
                  class="min-w-0 truncate text-sm font-semibold text-gray-900 dark:text-white"
                  :class="channelPanelCollapsed ? 'lg:sr-only' : ''"
                >
                  {{ t('availableChannels.title') }}
                </h2>
                <button
                  type="button"
                  class="inline-flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-lg border border-gray-200 bg-white text-gray-500 transition hover:border-primary-200 hover:text-primary-600 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-400 dark:hover:border-primary-800 dark:hover:text-primary-300"
                  :title="channelPanelCollapsed ? t('availableChannels.expandChannels', '展开渠道列表') : t('availableChannels.collapseChannels', '收起渠道列表')"
                  :aria-expanded="!channelPanelCollapsed"
                  @click="channelPanelCollapsed = !channelPanelCollapsed"
                >
                  <Icon :name="channelPanelCollapsed ? 'chevronRight' : 'chevronLeft'" size="sm" />
                </button>
              </div>

              <div
                class="max-h-full overflow-y-auto p-3"
                :class="channelPanelCollapsed ? 'lg:hidden' : ''"
              >
                <button
                  v-for="channel in filteredChannels"
                  :key="channel.name"
                  type="button"
                  class="mb-2 w-full rounded-xl border px-4 py-3 text-left transition last:mb-0"
                  :class="selectedChannelName === channel.name
                    ? 'border-primary-300 bg-primary-50 text-primary-700 shadow-sm dark:border-primary-700 dark:bg-primary-900/20 dark:text-primary-300'
                    : 'border-gray-200 bg-white text-gray-700 hover:border-primary-200 hover:bg-gray-50 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-300 dark:hover:border-primary-800 dark:hover:bg-dark-700'"
                  @click="selectedChannelName = channel.name"
                >
                  <div class="flex items-start justify-between gap-3">
                    <div class="min-w-0">
                      <div class="truncate text-sm font-medium">{{ channel.name }}</div>
                    </div>
                    <span
                      class="mt-0.5 inline-flex h-6 min-w-6 items-center justify-center rounded-full bg-gray-100 px-2 text-[11px] font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300"
                    >
                      {{ getChannelModelCount(channel) }}
                    </span>
                  </div>
                </button>
              </div>

              <div v-if="channelPanelCollapsed" class="hidden max-h-full flex-col items-center gap-2 overflow-y-auto py-3 lg:flex">
                <button
                  v-for="channel in filteredChannels"
                  :key="channel.name"
                  type="button"
                  class="relative inline-flex h-11 w-11 items-center justify-center rounded-lg border text-sm font-semibold transition"
                  :class="selectedChannelName === channel.name
                    ? 'border-primary-300 bg-primary-50 text-primary-700 shadow-sm dark:border-primary-700 dark:bg-primary-900/20 dark:text-primary-300'
                    : 'border-gray-200 bg-white text-gray-600 hover:border-primary-200 hover:bg-gray-50 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-300 dark:hover:border-primary-800 dark:hover:bg-dark-700'"
                  :title="channel.name"
                  @click="selectedChannelName = channel.name"
                >
                  <span class="max-w-7 truncate">{{ getChannelInitial(channel.name) }}</span>
                  <span class="absolute -right-1 -top-1 inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-gray-100 px-1 text-[10px] font-medium text-gray-600 ring-2 ring-white dark:bg-dark-700 dark:text-gray-300 dark:ring-dark-900">
                    {{ getChannelModelCount(channel) }}
                  </span>
                </button>
              </div>
            </aside>

            <section class="flex min-h-0 flex-col">
              <div class="border-b border-gray-200 px-5 py-4 dark:border-dark-700">
                <h2 class="truncate text-lg font-semibold text-gray-900 dark:text-white">
                  {{ selectedChannel?.name }}
                </h2>
              </div>

              <div class="min-h-0 flex-1 overflow-auto bg-gray-50/40 p-4 dark:bg-dark-900/20 sm:p-5">
                <div v-if="selectedChannelModels.length > 0" class="grid grid-cols-1 gap-4 xl:grid-cols-2 2xl:grid-cols-3">
                  <article
                    v-for="model in selectedChannelModels"
                    :key="`${model.platform}-${model.name}`"
                    class="group flex min-h-[190px] flex-col rounded-lg border border-gray-200 bg-white p-4 shadow-sm transition hover:border-primary-200 hover:shadow-md dark:border-dark-700 dark:bg-dark-800/95 dark:hover:border-primary-800"
                  >
                    <div class="flex items-start gap-3">
                      <div class="flex h-12 w-12 flex-shrink-0 items-center justify-center rounded-lg border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-900">
                        <ModelIcon :model="model.name" size="28px" />
                      </div>
                      <div class="min-w-0 flex-1">
                        <h3 class="break-words text-base font-semibold leading-tight text-gray-900 dark:text-white">
                          {{ model.name }}
                        </h3>
                        <p v-if="model.platform" class="mt-1 truncate text-xs uppercase tracking-wide text-gray-400 dark:text-gray-500">
                          {{ model.platform }}
                        </p>
                      </div>
                      <button
                        type="button"
                        class="inline-flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-lg border border-gray-200 bg-white text-gray-500 opacity-100 transition hover:border-primary-200 hover:text-primary-600 dark:border-dark-700 dark:bg-dark-900 dark:text-gray-400 dark:hover:border-primary-800 dark:hover:text-primary-300 sm:opacity-0 sm:group-hover:opacity-100 sm:group-focus-within:opacity-100"
                        :title="t('common.copy', '复制')"
                        @click="copyModelName(model.name)"
                      >
                        <Icon name="copy" size="sm" />
                      </button>
                    </div>

                    <div class="mt-4 space-y-1.5 text-sm">
                      <template v-if="getModelPricingItems(model).length > 0">
                        <div
                          v-for="item in getModelPricingItems(model)"
                          :key="item.key"
                          class="flex flex-wrap items-baseline gap-x-1.5 gap-y-0.5 text-gray-600 dark:text-gray-300"
                        >
                          <span class="text-gray-500 dark:text-gray-400">{{ item.label }}</span>
                          <span class="font-medium text-gray-800 dark:text-gray-100">{{ item.value }}</span>
                          <span v-if="item.unit" class="text-gray-500 dark:text-gray-400">{{ item.unit }}</span>
                        </div>
                      </template>
                      <p v-else class="text-sm text-gray-500 dark:text-gray-400">
                        {{ t('availableChannels.noPricing', '暂无定价') }}
                      </p>
                    </div>

                    <div class="mt-auto flex flex-wrap items-center gap-2 pt-4">
                        <span
                          class="inline-flex rounded-full px-2.5 py-1 text-xs font-medium"
                          :class="getBillingModeBadgeClass(model.pricing?.billing_mode)"
                        >
                          {{ getBillingModeText(model.pricing?.billing_mode) }}
                        </span>
                        <span
                          v-if="model.platform"
                          class="inline-flex rounded-full bg-gray-100 px-2.5 py-1 text-xs font-medium uppercase text-gray-600 dark:bg-dark-700 dark:text-gray-300"
                        >
                          {{ model.platform }}
                        </span>
                    </div>
                  </article>
                </div>

                <div v-else class="flex h-full min-h-[260px] items-center justify-center px-6 py-12 text-center">
                  <div>
                    <Icon name="inbox" size="xl" class="mx-auto mb-3 h-12 w-12 text-gray-400" />
                    <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('availableChannels.noModels') }}</p>
                  </div>
                </div>
              </div>
            </section>
          </div>
        </div>
      </template>
    </TablePageLayout>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import ModelIcon from '@/components/common/ModelIcon.vue'
import userChannelsAPI, { type UserAvailableChannel, type UserSupportedModel } from '@/api/channels'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatScaled } from '@/utils/pricing'
import { getBillingModeBadgeClass, getBillingModeLabel } from '@/utils/billingMode'
import { BILLING_MODE_IMAGE, BILLING_MODE_PER_REQUEST, BILLING_MODE_TOKEN } from '@/constants/channel'

const { t } = useI18n()
const appStore = useAppStore()

const channels = ref<UserAvailableChannel[]>([])
const loading = ref(false)
const searchQuery = ref('')
const selectedChannelName = ref('')
const channelPanelCollapsed = ref(false)
const quickStartGuideUrl = 'https://www.kdocs.cn/l/cmuLD3zCWFWq'

const filteredChannels = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  if (!q) return channels.value

  return channels.value.filter((channel) => {
    if (channel.name.toLowerCase().includes(q)) return true
    if ((channel.description || '').toLowerCase().includes(q)) return true

    return channel.platforms.some((section) =>
      section.platform.toLowerCase().includes(q) ||
      section.supported_models.some((model) => model.name.toLowerCase().includes(q)),
    )
  })
})

const selectedChannel = computed(() => {
  const list = filteredChannels.value
  if (list.length === 0) return null
  return list.find((channel) => channel.name === selectedChannelName.value) ?? list[0]
})

const selectedChannelModels = computed<UserSupportedModel[]>(() => {
  const channel = selectedChannel.value
  if (!channel) return []

  return channel.platforms.flatMap((section) =>
    section.supported_models.map((model) => ({
      ...model,
      platform: model.platform || section.platform,
    })),
  )
})

watch(filteredChannels, (list) => {
  if (list.length === 0) {
    selectedChannelName.value = ''
    return
  }

  if (!list.some((channel) => channel.name === selectedChannelName.value)) {
    selectedChannelName.value = list[0].name
  }
}, { immediate: true })

async function loadChannels() {
  loading.value = true
  try {
    const list = await userChannelsAPI.getAvailable()
    channels.value = list
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  } finally {
    loading.value = false
  }
}

function getChannelModelCount(channel: UserAvailableChannel): number {
  return channel.platforms.reduce((count, section) => count + section.supported_models.length, 0)
}

function getChannelInitial(name: string): string {
  return name.trim().slice(0, 1).toUpperCase() || '#'
}

function getBillingModeText(mode: string | null | undefined): string {
  return getBillingModeLabel(mode, t)
}

function getModelPricingValue(
  model: UserSupportedModel,
  field: 'input_price' | 'output_price' | 'cache_write_price' | 'cache_read_price',
): string {
  const pricing = model.pricing
  if (!pricing) return '-'

  if (pricing.billing_mode !== BILLING_MODE_TOKEN) return '-'
  return formatScaled(pricing[field], 1_000_000)
}

interface PricingDisplayItem {
  key: string
  label: string
  value: string
  unit?: string
}

function getModelPricingItems(model: UserSupportedModel): PricingDisplayItem[] {
  const pricing = model.pricing
  if (!pricing) return []

  if (pricing.billing_mode === BILLING_MODE_TOKEN) {
    return [
      {
        key: 'input',
        label: t('availableChannels.pricing.inputPrice'),
        value: getModelPricingValue(model, 'input_price'),
        unit: t('availableChannels.pricing.unitPerMillion'),
      },
      {
        key: 'output',
        label: t('availableChannels.pricing.outputPrice'),
        value: getModelPricingValue(model, 'output_price'),
        unit: t('availableChannels.pricing.unitPerMillion'),
      },
      {
        key: 'cache-write',
        label: t('availableChannels.pricing.cacheWritePrice'),
        value: getModelPricingValue(model, 'cache_write_price'),
        unit: t('availableChannels.pricing.unitPerMillion'),
      },
      {
        key: 'cache-read',
        label: t('availableChannels.pricing.cacheReadPrice'),
        value: getModelPricingValue(model, 'cache_read_price'),
        unit: t('availableChannels.pricing.unitPerMillion'),
      },
    ].filter((item) => item.value !== '-')
  }

  return getPerRequestPricingItems(model).map((item) => ({
    ...item,
    label: item.label || getRequestPricingLabel(pricing.billing_mode),
    unit: t('availableChannels.pricing.unitPerRequest'),
  }))
}

function getPerRequestPricingItems(model: UserSupportedModel): PricingDisplayItem[] {
  const pricing = model.pricing
  if (!pricing) return []
  if (pricing.billing_mode !== BILLING_MODE_PER_REQUEST && pricing.billing_mode !== BILLING_MODE_IMAGE) {
    return []
  }

  const items: PricingDisplayItem[] = []
  if (pricing.per_request_price != null) {
    items.push({
      key: 'default',
      label: pricing.intervals.length > 0 ? t('common.default', '默认') : '',
      value: formatScaled(pricing.per_request_price, 1),
    })
  } else if (pricing.billing_mode === BILLING_MODE_IMAGE && pricing.image_output_price != null) {
    items.push({
      key: 'legacy-image',
      label: pricing.intervals.length > 0 ? t('availableChannels.pricing.imageOutputPrice') : '',
      value: formatScaled(pricing.image_output_price, 1),
    })
  }

  pricing.intervals.forEach((interval, index) => {
    if (interval.per_request_price == null) return
    items.push({
      key: `tier-${index}`,
      label: interval.tier_label || formatIntervalRange(interval.min_tokens, interval.max_tokens),
      value: formatScaled(interval.per_request_price, 1),
    })
  })

  return items
}

function getRequestPricingLabel(mode: string | null | undefined): string {
  if (mode === BILLING_MODE_IMAGE) return t('availableChannels.pricing.imageOutputPrice')
  return t('availableChannels.pricing.perRequestPrice')
}

function formatIntervalRange(min: number, max: number | null): string {
  if (max == null) return `${min}+`
  return `${min}-${max}`
}

async function copyModelName(name: string) {
  try {
    if (!navigator.clipboard) throw new Error('Clipboard unavailable')
    await navigator.clipboard.writeText(name)
    appStore.showSuccess(t('common.copiedToClipboard', '已复制到剪贴板'))
  } catch {
    appStore.showError(t('common.copyFailed', '复制失败'))
  }
}

onMounted(loadChannels)
</script>
