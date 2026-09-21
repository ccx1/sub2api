<template>
  <form class="min-w-0 space-y-5" @submit.prevent="save">
    <section class="card overflow-hidden">
      <div class="border-b border-gray-200 px-5 py-4 dark:border-dark-700 sm:px-6">
        <h2 class="flex items-center gap-2 font-semibold text-gray-900 dark:text-white"><Icon name="chart" size="sm" class="text-cyan-600 dark:text-cyan-400" />{{ t('sharedPool.globalRates') }}</h2>
        <p class="mt-1.5 text-sm text-gray-500 dark:text-gray-400">{{ t('sharedPool.globalRatesHint') }}</p>
      </div>
      <div class="grid gap-5 p-5 sm:grid-cols-2 sm:p-6 xl:grid-cols-4">
        <div><label for="pool-platform-rate" class="input-label">{{ t('sharedPool.platformShare') }}</label><div class="relative"><input id="pool-platform-rate" v-model.number="platformRate" required type="number" min="0" max="100" step="0.01" class="input w-full pr-10" /><span class="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-sm text-gray-400">%</span></div><p class="mt-2 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t('sharedPool.platformShareHint') }}</p></div>
        <div><label for="pool-proxy-rate" class="input-label">{{ t('sharedPool.proxyShare') }}</label><div class="relative"><input id="pool-proxy-rate" v-model.number="proxyRate" required type="number" min="0" max="100" step="0.01" class="input w-full pr-10" /><span class="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-sm text-gray-400">%</span></div><p class="mt-2 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t('sharedPool.proxyShareHint') }}</p></div>
        <div><label for="pool-max-concurrency" class="input-label">{{ t('sharedPool.maxConcurrency') }}</label><input id="pool-max-concurrency" v-model.number="maxConcurrency" required type="number" min="1" step="1" class="input w-full" /><p class="mt-2 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t('sharedPool.maxConcurrencyHint') }}</p></div>
        <div><label for="pool-default-priority" class="input-label">{{ t('sharedPool.defaultPriority') }}</label><input id="pool-default-priority" v-model.number="defaultPriority" required type="number" min="0" max="100" step="1" :disabled="saving" class="input w-full" /><p class="mt-2 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t('sharedPool.defaultPriorityHint') }}</p></div>
      </div>
    </section>
    <section class="card overflow-hidden">
      <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 px-5 py-4 dark:border-dark-700 sm:px-6">
        <div><h2 class="flex items-center gap-2 font-semibold text-gray-900 dark:text-white"><Icon name="link" size="sm" class="text-cyan-600 dark:text-cyan-400" />{{ t('sharedPool.defaultGroup') }}</h2><p class="mt-1.5 text-sm text-gray-500 dark:text-gray-400">{{ t('sharedPool.defaultGroupHint') }}</p></div>
        <RouterLink class="btn btn-secondary btn-sm" to="/admin/groups"><Icon name="externalLink" size="sm" class="mr-1.5" />{{ t('nav.groups') }}</RouterLink>
      </div>
      <div class="space-y-4 p-5 sm:p-6">
        <p v-if="!sharedGroups.length" class="flex items-start gap-2 rounded-lg bg-amber-50/80 p-3 text-sm text-amber-700 dark:bg-amber-950/30 dark:text-amber-400"><Icon name="infoCircle" size="sm" class="mt-0.5 shrink-0" />{{ t('sharedPool.defaultGroupMissing') }}</p>
        <div class="grid gap-5 sm:grid-cols-2">
          <div v-for="platform in platforms" :key="platform">
            <label :for="`default-group-${platform}`" class="input-label flex items-center gap-2"><PlatformIcon :platform="platform" />{{ platformNames[platform] }}</label>
            <Select :key="`${platform}-${saving}`" :id="`default-group-${platform}`" :model-value="defaults[platform]" :options="defaultGroupOptions(platform)" :disabled="saving" :error="Boolean(defaults[platform] && !validDefault(platform))" :aria-label="`${platformNames[platform]} ${t('sharedPool.defaultGroup')}`" class="min-w-0" @update:model-value="updateDefault(platform, $event)" />
          </div>
        </div>
      </div>
    </section>
    <SharedPoolSubscriptionGroups ref="subscriptionGroups" :rules="settings.subscription_group_ids" :defaults="defaults" :groups="groups" :disabled="saving" />
    <SharedPoolSubscriptionRates ref="subscriptionRates" :rates="settings.subscription_settlement_multipliers" :disabled="saving" />
    <SharedPoolSettlementSettings ref="settlementSettings" :settings="settings" :disabled="saving" />
    <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
    <div class="flex justify-end"><button class="btn btn-primary" :disabled="saving"><Icon :name="saving ? 'refresh' : 'check'" size="sm" class="mr-2" :class="{ 'animate-spin': saving }" />{{ saving ? t('common.saving') : t('common.save') }}</button></div>
  </form>
</template>

<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AdminGroup } from '@/types'
import { adminSharedPoolAPI, type SharedSettings, type SharedPlatform } from '@/api/sharedPool'
import { useAppStore } from '@/stores/app'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import SharedPoolSubscriptionGroups from './SharedPoolSubscriptionGroups.vue'
import SharedPoolSubscriptionRates from './SharedPoolSubscriptionRates.vue'
import SharedPoolSettlementSettings from './SharedPoolSettlementSettings.vue'
import { isDispatchGroup, validSharedPriority } from './settlementPolicy'
const props = defineProps<{ settings: SharedSettings; groups: AdminGroup[] }>()
const emit = defineEmits<{ saved: [settings: SharedSettings] }>()
const { t } = useI18n()
const app = useAppStore()
const platforms: SharedPlatform[] = ['openai', 'anthropic', 'gemini', 'antigravity']
const platformNames: Record<SharedPlatform, string> = { openai: 'OpenAI', anthropic: 'Claude', gemini: 'Gemini', antigravity: 'Antigravity' }
const platformRate = ref(props.settings.platform_rate_bps / 100)
const proxyRate = ref(props.settings.proxy_rate_bps / 100)
const maxConcurrency = ref(props.settings.max_concurrency)
const defaultPriority = ref<number | ''>(props.settings.default_priority ?? 50)
const defaults = reactive<Partial<Record<SharedPlatform, number>>>({ openai: 0, anthropic: 0, gemini: 0, antigravity: 0, ...props.settings.default_group_ids })
const sharedGroups = computed(() => props.groups.filter(isDispatchGroup))
const saving = ref(false)
const error = ref('')
const subscriptionGroups = ref<InstanceType<typeof SharedPoolSubscriptionGroups>>()
const subscriptionRates = ref<InstanceType<typeof SharedPoolSubscriptionRates>>()
const settlementSettings = ref<InstanceType<typeof SharedPoolSettlementSettings>>()
function validDefault(platform: SharedPlatform) { return sharedGroups.value.some(group => group.id === defaults[platform] && group.platform === platform) }
function defaultGroupOptions(platform: SharedPlatform): SelectOption[] {
  const options: SelectOption[] = [{ value: 0, label: t('sharedPool.noDefault') }]
  const selected = defaults[platform]
  if (selected && !validDefault(platform)) {
    options.push({ value: selected, label: t('sharedPool.subscriptionUnavailableGroup', { name: props.groups.find(group => group.id === selected)?.name || `#${selected}` }), disabled: true })
  }
  return options.concat(sharedGroups.value.filter(group => group.platform === platform).map(group => ({ value: group.id, label: `${group.name} · ${group.rate_multiplier}x` })))
}
function updateDefault(platform: SharedPlatform, value: SelectOption['value']) {
  if (!saving.value && typeof value === 'number') defaults[platform] = value
}
async function save() {
  if (saving.value) return
  error.value = ''
  if (Number(platformRate.value) + Number(proxyRate.value) > 100) { error.value = t('sharedPool.invalidRates'); return }
  if (!validSharedPriority(defaultPriority.value)) { error.value = t('sharedPool.invalidPriority'); return }
  saving.value = true
  try {
    const defaultGroupIDs: Partial<Record<SharedPlatform, number>> = {}
    for (const platform of platforms) {
      if (!defaults[platform]) continue
      if (!validDefault(platform)) throw new Error(t('sharedPool.subscriptionInvalidDefault', { platform: platformNames[platform] }))
      defaultGroupIDs[platform] = defaults[platform]
    }
    const subscriptionGroupIDs = subscriptionGroups.value?.serialize() || {}
    const subscriptionSettlementMultipliers = subscriptionRates.value?.serialize() || {}
    const policy = settlementSettings.value?.serialize() || { settlement_multiplier: 1 }
    const payload: SharedSettings = { platform_rate_bps: Math.round(platformRate.value * 100), proxy_rate_bps: Math.round(proxyRate.value * 100), max_concurrency: maxConcurrency.value, default_group_ids: defaultGroupIDs, subscription_group_ids: subscriptionGroupIDs, ...policy }
    if (props.settings.default_priority !== undefined || defaultPriority.value !== 50) payload.default_priority = defaultPriority.value
    // Older servers omit this field. Keep the request compatible while sending an
    // explicit empty object when a server has returned the tier-rate setting.
    if (props.settings.subscription_settlement_multipliers !== undefined || Object.keys(subscriptionSettlementMultipliers).length) {
      payload.subscription_settlement_multipliers = subscriptionSettlementMultipliers
    }
    const result = await adminSharedPoolAPI.saveSettings(payload)
    emit('saved', result); app.showSuccess(t('sharedPool.saved'))
  } catch (e: unknown) { error.value = (e as Error).message || t('sharedPool.actionFailed') }
  finally { saving.value = false }
}
</script>
