<template>
  <section class="card overflow-hidden">
    <div class="border-b border-gray-200 px-5 py-4 dark:border-dark-700 sm:px-6">
      <h2 class="flex items-center gap-2 font-semibold text-gray-900 dark:text-white"><Icon name="arrowsUpDown" size="sm" class="text-cyan-600 dark:text-cyan-400" />{{ t('sharedPool.subscriptionGroups') }}</h2>
      <p class="mt-1.5 text-sm leading-relaxed text-gray-500 dark:text-gray-400">{{ t('sharedPool.subscriptionGroupsHint') }}</p>
    </div>
    <div class="divide-y divide-gray-200 dark:divide-dark-700">
      <div v-for="platform in platforms" :key="platform" :data-tier-platform="platform">
        <button type="button" class="flex w-full items-center gap-3 px-5 py-4 text-left hover:bg-gray-50/60 dark:hover:bg-dark-800/50 sm:px-6" :aria-expanded="expanded[platform]" :aria-controls="`subscription-rules-${platform}`" @click="expanded[platform] = !expanded[platform]">
          <PlatformIcon :platform="platform" />
          <span class="font-medium text-gray-900 dark:text-gray-100">{{ platformNames[platform] }}</span>
          <span class="ml-auto text-xs text-gray-500 dark:text-gray-400">{{ rows[platform].length ? t('sharedPool.subscriptionRuleCount', { count: rows[platform].length }) : t('sharedPool.subscriptionUseDefault') }}</span>
          <Icon name="chevronDown" size="sm" class="shrink-0 text-gray-400 transition-transform" :class="{ 'rotate-180': expanded[platform] }" />
        </button>
        <div v-show="expanded[platform]" :id="`subscription-rules-${platform}`" class="space-y-3 px-5 pb-5 sm:px-6">
          <p v-if="platform === 'anthropic'" class="text-sm leading-relaxed text-gray-500 dark:text-gray-400">{{ t('sharedPool.subscriptionClaudeHint') }}</p>
          <p v-else-if="!rows[platform].length" class="text-sm text-gray-500 dark:text-gray-400">{{ t('sharedPool.subscriptionNoRules') }}</p>
          <div v-for="row in rows[platform]" :key="row.id" class="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-end gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)_auto]" data-tier-row>
            <div class="col-span-2 min-w-0 sm:col-span-1">
              <label :for="`subscription-tier-${row.id}`" class="input-label">{{ t('sharedPool.subscriptionTier') }}</label>
              <Select :key="`tier-${disabled}`" :id="`subscription-tier-${row.id}`" :model-value="row.tier" :options="tierSelectOptions(platform, row)" :disabled="disabled" :error="Boolean(row.tier && !knownTier(platform, row.tier))" :aria-label="`${platformNames[platform]} ${t('sharedPool.subscriptionTier')}`" class="min-w-0" data-tier-select @update:model-value="updateTier(row, $event)" />
            </div>
            <div class="min-w-0">
              <label :for="`subscription-group-${row.id}`" class="input-label">{{ t('sharedPool.subscriptionTargetGroup') }}</label>
              <Select :key="`group-${disabled}`" :id="`subscription-group-${row.id}`" :model-value="row.groupId" :options="groupSelectOptions(platform, row)" :disabled="disabled" :error="Boolean(row.groupId && !validGroup(platform, row.groupId))" :aria-label="`${platformNames[platform]} ${t('sharedPool.subscriptionTargetGroup')}`" class="min-w-0" data-group-select @update:model-value="updateGroup(row, $event)" />
            </div>
            <button type="button" class="btn btn-secondary mb-0.5 h-10 w-10 shrink-0 p-0 text-gray-500 hover:text-red-600 dark:hover:text-red-400" :aria-label="t('sharedPool.subscriptionRemoveRule')" :title="t('sharedPool.subscriptionRemoveRule')" :disabled="disabled" data-remove-rule @click="removeRule(platform, row.id)"><Icon name="trash" size="sm" /></button>
            <p v-if="row.groupId && !validGroup(platform, row.groupId)" class="col-span-2 text-xs text-amber-700 dark:text-amber-400 sm:col-span-3">{{ t('sharedPool.subscriptionUnavailableHint') }}</p>
          </div>
          <div class="flex flex-wrap items-center gap-3">
            <button v-if="platform !== 'anthropic'" type="button" class="btn btn-secondary btn-sm" :disabled="disabled || rows[platform].length >= tierOptions[platform].length" data-add-rule @click="addRule(platform)"><Icon name="plus" size="sm" class="mr-1.5" />{{ t('sharedPool.subscriptionAddRule') }}</button>
            <p class="text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ fallbackText(platform) }}</p>
          </div>
        </div>
      </div>
    </div>
    <p class="border-t border-gray-200 px-5 py-3 text-xs leading-relaxed text-gray-500 dark:border-dark-700 dark:text-gray-400 sm:px-6">{{ t('sharedPool.subscriptionExistingHint') }}</p>
  </section>
</template>

<script setup lang="ts">
import { reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AdminGroup } from '@/types'
import type { SharedPlatform, SharedSettings } from '@/api/sharedPool'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import { sharedPlatforms as platforms, sharedPlatformNames as platformNames, subscriptionTierOptions as tierOptions, isDispatchGroup } from './settlementPolicy'

const props = defineProps<{
  rules?: SharedSettings['subscription_group_ids']
  defaults: Partial<Record<SharedPlatform, number>>
  groups: AdminGroup[]
  disabled?: boolean
}>()
const { t } = useI18n()
type RuleRow = { id: number; tier: string; groupId: number }
const rows = reactive<Record<SharedPlatform, RuleRow[]>>({ openai: [], anthropic: [], gemini: [], antigravity: [] })
const expanded = reactive<Record<SharedPlatform, boolean>>({ openai: false, anthropic: false, gemini: false, antigravity: false })
let nextRowId = 0
watch(() => props.rules, value => {
  for (const platform of platforms) {
    rows[platform] = Object.entries(value?.[platform] || {}).map(([tier, groupId]) => ({ id: nextRowId++, tier, groupId }))
    if (rows[platform].length) expanded[platform] = true
  }
}, { immediate: true, deep: true })

function groupsFor(platform: SharedPlatform) {
  return props.groups.filter(group => group.platform === platform && isDispatchGroup(group))
}
function validGroup(platform: SharedPlatform, id: number) { return groupsFor(platform).some(group => group.id === id) }
function knownTier(platform: SharedPlatform, tier: string) { return tierOptions[platform].some(option => option.value === tier) }
function unavailableGroup(id: number) {
  const group = props.groups.find(item => item.id === id)
  return t('sharedPool.subscriptionUnavailableGroup', { name: group?.name || `#${id}` })
}
function tierSelectOptions(platform: SharedPlatform, row: RuleRow): SelectOption[] {
  const options: SelectOption[] = [{ value: '', label: t('sharedPool.subscriptionSelectTier') }]
  if (row.tier && !knownTier(platform, row.tier)) {
    options.push({ value: row.tier, label: t('sharedPool.subscriptionUnknownTier', { tier: row.tier }), disabled: true })
  }
  return options.concat(tierOptions[platform])
}
function groupSelectOptions(platform: SharedPlatform, row: RuleRow): SelectOption[] {
  const options: SelectOption[] = [{ value: 0, label: t('sharedPool.subscriptionSelectGroup') }]
  if (row.groupId && !validGroup(platform, row.groupId)) {
    options.push({ value: row.groupId, label: unavailableGroup(row.groupId), disabled: true })
  }
  return options.concat(groupsFor(platform).map(group => ({ value: group.id, label: `${group.name} · ${group.rate_multiplier}x` })))
}
function updateTier(row: RuleRow, value: SelectOption['value']) {
  if (!props.disabled && typeof value === 'string') row.tier = value
}
function updateGroup(row: RuleRow, value: SelectOption['value']) {
  if (!props.disabled && typeof value === 'number') row.groupId = value
}
function fallbackText(platform: SharedPlatform) {
  const group = groupsFor(platform).find(item => item.id === props.defaults[platform])
  return group ? t('sharedPool.subscriptionFallback', { name: group.name }) : t('sharedPool.subscriptionDefaultRequiredHint')
}
function addRule(platform: SharedPlatform) { rows[platform].push({ id: nextRowId++, tier: '', groupId: 0 }) }
function removeRule(platform: SharedPlatform, id: number) { rows[platform] = rows[platform].filter(row => row.id !== id) }

function serialize(): NonNullable<SharedSettings['subscription_group_ids']> {
  const result: NonNullable<SharedSettings['subscription_group_ids']> = {}
  for (const platform of platforms) {
    if (!rows[platform].length) continue
    const ruleError = validatePlatform(platform)
    if (ruleError) { expanded[platform] = true; throw new Error(ruleError) }
    result[platform] = Object.fromEntries(rows[platform].map(row => [row.tier, row.groupId]))
  }
  return result
}
function validatePlatform(platform: SharedPlatform) {
  const context = { platform: platformNames[platform] }
  const seen = new Set<string>()
  for (const row of rows[platform]) {
    if (!knownTier(platform, row.tier)) return t('sharedPool.subscriptionInvalidTier', context)
    if (seen.has(row.tier)) return t('sharedPool.subscriptionDuplicateTier', context)
    if (!validGroup(platform, row.groupId)) return t('sharedPool.subscriptionInvalidGroup', context)
    seen.add(row.tier)
  }
  return ''
}
defineExpose({ serialize })
</script>
