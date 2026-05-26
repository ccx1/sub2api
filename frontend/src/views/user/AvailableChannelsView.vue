<template>
  <AppLayout>
    <TablePageLayout>
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
        <div class="h-full overflow-hidden rounded-2xl border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800">
          <div v-if="loading" class="flex h-full items-center justify-center py-16">
            <Icon name="refresh" size="lg" class="animate-spin text-gray-400" />
          </div>

          <div v-else-if="filteredChannels.length === 0" class="flex h-full items-center justify-center px-6 py-16 text-center">
            <div>
              <Icon name="inbox" size="xl" class="mx-auto mb-3 h-12 w-12 text-gray-400" />
              <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('availableChannels.empty') }}</p>
            </div>
          </div>

          <div v-else class="grid h-full min-h-0 grid-cols-1 lg:grid-cols-[280px_minmax(0,1fr)]">
            <aside class="border-b border-gray-200 bg-gray-50/70 dark:border-dark-700 dark:bg-dark-900/40 lg:border-b-0 lg:border-r">
              <div class="border-b border-gray-200 px-4 py-3 dark:border-dark-700">
                <h2 class="text-sm font-semibold text-gray-900 dark:text-white">
                  {{ t('availableChannels.title') }}
                </h2>
              </div>

              <div class="max-h-full overflow-y-auto p-3">
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
            </aside>

            <section class="flex min-h-0 flex-col">
              <div class="border-b border-gray-200 px-5 py-4 dark:border-dark-700">
                <h2 class="truncate text-lg font-semibold text-gray-900 dark:text-white">
                  {{ selectedChannel?.name }}
                </h2>
              </div>

              <div class="min-h-0 flex-1 overflow-auto">
                <table class="w-full min-w-[880px] border-collapse text-sm">
                  <thead class="sticky top-0 z-10 bg-gray-50/95 text-xs font-medium uppercase tracking-wide text-gray-500 backdrop-blur dark:bg-dark-800/95 dark:text-gray-400">
                    <tr>
                      <th class="px-5 py-3 text-left">模型名称</th>
                      <th class="px-5 py-3 text-left">{{ t('availableChannels.pricing.billingMode') }}</th>
                      <th class="px-5 py-3 text-left">{{ t('availableChannels.pricing.inputPrice') }}</th>
                      <th class="px-5 py-3 text-left">{{ t('availableChannels.pricing.outputPrice') }}</th>
                      <th class="px-5 py-3 text-left">{{ t('availableChannels.pricing.cacheWritePrice') }}</th>
                      <th class="px-5 py-3 text-left">{{ t('availableChannels.pricing.cacheReadPrice') }}</th>
                      <th class="px-5 py-3 text-left">{{ t('availableChannels.pricing.perRequestPrice') }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr
                      v-for="model in selectedChannelModels"
                      :key="`${model.platform}-${model.name}`"
                      class="border-t border-gray-100 dark:border-dark-700"
                    >
                      <td class="px-5 py-4 align-top">
                        <div class="font-medium text-gray-900 dark:text-white">{{ model.name }}</div>
                      </td>
                      <td class="px-5 py-4 align-top">
                        <span
                          class="inline-flex rounded-full px-2.5 py-1 text-xs font-medium"
                          :class="getBillingModeBadgeClass(model.pricing?.billing_mode)"
                        >
                          {{ getBillingModeText(model.pricing?.billing_mode) }}
                        </span>
                      </td>
                      <td class="px-5 py-4 align-top text-gray-700 dark:text-gray-300">{{ getModelPricingValue(model, 'input_price') }}</td>
                      <td class="px-5 py-4 align-top text-gray-700 dark:text-gray-300">{{ getModelPricingValue(model, 'output_price') }}</td>
                      <td class="px-5 py-4 align-top text-gray-700 dark:text-gray-300">{{ getModelPricingValue(model, 'cache_write_price') }}</td>
                      <td class="px-5 py-4 align-top text-gray-700 dark:text-gray-300">{{ getModelPricingValue(model, 'cache_read_price') }}</td>
                      <td class="px-5 py-4 align-top text-gray-700 dark:text-gray-300">
                        <div v-if="getPerRequestPricingItems(model).length > 0" class="space-y-1">
                          <div
                            v-for="item in getPerRequestPricingItems(model)"
                            :key="item.key"
                            class="whitespace-nowrap"
                          >
                            <span v-if="item.label" class="text-gray-500 dark:text-gray-400">{{ item.label }}: </span>
                            <span>{{ item.value }}</span>
                            <span class="ml-1 text-gray-400 dark:text-gray-500">{{ t('availableChannels.pricing.unitPerRequest') }}</span>
                          </div>
                        </div>
                        <span v-else>-</span>
                      </td>
                    </tr>
                    <tr v-if="selectedChannelModels.length === 0">
                      <td colspan="7" class="px-5 py-12 text-center text-sm text-gray-500 dark:text-gray-400">
                        {{ t('availableChannels.noModels') }}
                      </td>
                    </tr>
                  </tbody>
                </table>
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

function formatIntervalRange(min: number, max: number | null): string {
  if (max == null) return `${min}+`
  return `${min}-${max}`
}

onMounted(loadChannels)
</script>
