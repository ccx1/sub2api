<template>
  <BaseDialog :show="true" :title="t('sharedPool.allocation')" :show-close-button="!saving" :close-on-escape="!saving" @close="close">
    <div class="mb-5 flex items-start gap-3 border-b border-gray-200 pb-4 dark:border-dark-700"><span class="rounded-lg border border-gray-200 p-2.5 text-gray-700 dark:border-dark-600 dark:text-gray-200"><PlatformIcon :platform="account.platform" size="lg" /></span><div class="min-w-0"><h3 class="break-words font-medium text-gray-900 dark:text-white">{{ account.name }}</h3><p class="mt-1 break-all text-xs text-gray-500 dark:text-gray-400">{{ account.platform }} · {{ account.owner_email || `#${account.owner_user_id}` }}</p></div></div>
    <div class="mb-5 space-y-4">
      <div class="flex items-start justify-between gap-4">
        <div><label id="pool-dispatch-enabled" class="text-sm font-medium text-gray-900 dark:text-white">{{ t('sharedPool.adminDispatch') }}</label><p class="mt-1 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t('sharedPool.adminDispatchHint') }}</p></div>
        <Toggle :model-value="dispatchEnabled" :disabled="saving" aria-labelledby="pool-dispatch-enabled" data-test="admin-dispatch" @update:model-value="setDispatch" />
      </div>
      <p v-if="!account.dispatch_consent" class="rounded-lg bg-amber-50 p-3 text-xs leading-relaxed text-amber-700 dark:bg-amber-950/30 dark:text-amber-400" data-test="legacy-dispatch-notice">{{ t('sharedPool.adminLegacyDispatchHint') }}</p>
      <div v-if="account.type === 'oauth'">
        <label for="pool-subscription-tier" class="input-label">{{ t('sharedPool.subscriptionTier') }}</label>
        <p class="mb-2 text-xs text-gray-500 dark:text-gray-400" data-test="admin-effective-tier">{{ t('sharedPool.currentSubscriptionTier') }} <span class="font-medium text-gray-700 dark:text-gray-200">{{ effectiveTierLabel }}</span> · {{ t(account.subscription_tier_override ? 'sharedPool.subscriptionTierManual' : 'sharedPool.subscriptionTierAutomatic') }}</p>
        <Select v-if="tierOptions.length" id="pool-subscription-tier" :model-value="subscriptionTier" :options="tierSelectOptions" :disabled="saving" :aria-label="t('sharedPool.subscriptionTier')" data-test="admin-subscription-tier" @update:model-value="setTier" />
        <p class="mt-2 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t(tierOptions.length ? 'sharedPool.adminTierHint' : 'sharedPool.adminTierUnsupported') }}</p>
      </div>
      <div>
        <label for="pool-account-priority" class="input-label">{{ t('admin.accounts.priority') }}</label>
        <input id="pool-account-priority" v-model.number="priority" required type="number" min="0" max="100" step="1" :disabled="saving" class="input w-full" />
        <p class="mt-2 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t('admin.accounts.priorityHint') }}</p>
      </div>
    </div>
    <div class="flex items-center justify-between gap-2"><h3 class="text-sm font-medium text-gray-900 dark:text-white">{{ t('sharedPool.groups') }}</h3><span class="text-xs text-gray-500 dark:text-gray-400">{{ t('sharedPool.selectedGroups', { count: groupIDs.length }) }}</span></div>
    <p class="mt-2 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t('sharedPool.allocationHint') }}</p>
    <p class="mt-1 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t('sharedPool.dispatchBillingHint') }}</p>
    <div class="my-4 max-h-72 space-y-2 overflow-y-auto">
      <label v-for="group in displayedGroups" :key="group.id" class="flex cursor-pointer items-center gap-3 rounded-lg border p-3 text-sm transition-colors" :class="groupIDs.includes(group.id) ? 'border-cyan-300 bg-cyan-50/50 dark:border-cyan-700 dark:bg-cyan-950/20' : 'border-gray-200 hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-700/30'">
        <input v-model="groupIDs" type="checkbox" :value="group.id" :disabled="saving || (!group.available && !groupIDs.includes(group.id))" class="h-4 w-4 shrink-0 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500 dark:bg-dark-800" />
        <span class="min-w-0 flex-1 break-words text-gray-900 dark:text-white">{{ group.available ? group.name : t('sharedPool.subscriptionUnavailableGroup', { name: group.name }) }}</span>
        <span class="shrink-0 text-xs tabular-nums text-gray-500 dark:text-gray-400">{{ group.rate == null ? '—' : `${group.rate}x` }}</span>
      </label>
      <p v-if="!compatibleGroups.length" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-700 dark:bg-amber-950/30 dark:text-amber-400">{{ t('sharedPool.allocationNoCompatibleGroups') }}</p>
    </div>
    <p v-if="error" class="mt-3 text-sm text-red-600 dark:text-red-400" role="alert">{{ error }}</p>
    <template #footer><div class="flex justify-end gap-3"><button type="button" class="btn btn-secondary" :disabled="saving" @click="close">{{ t('common.cancel') }}</button><button type="button" class="btn btn-primary" :disabled="saving || !hasChanges" @click="save">{{ saving ? t('common.saving') : t('common.save') }}</button></div></template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Toggle from '@/components/common/Toggle.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import type { AdminGroup } from '@/types'
import { adminSharedPoolAPI, type SharedAccount, type SharedAccountAllocationInput } from '@/api/sharedPool'
import { isDispatchGroup, isSharedManualAssignmentGroup, subscriptionTierOptions, validSharedPriority } from './settlementPolicy'
const props = defineProps<{ account: SharedAccount; groups: AdminGroup[] }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const groupIDs = ref([...props.account.group_ids])
const dispatchEnabled = ref(props.account.enabled && !props.account.admin_disabled)
const dispatchChanged = ref(false)
const subscriptionTier = ref(props.account.subscription_tier_override || '')
const priority = ref<number | ''>(props.account.priority ?? 50)
const tierOptions = computed(() => props.account.type === 'oauth' ? subscriptionTierOptions[props.account.platform] : [])
const tierSelectOptions = computed<SelectOption[]>(() => [{ value: '', label: t('sharedPool.subscriptionTierAutomatic') }, ...tierOptions.value])
const effectiveTierLabel = computed(() => tierOptions.value.find(tier => tier.value === props.account.subscription_tier)?.label || t('sharedPool.unknownTier'))
const groupsChanged = computed(() => groupIDs.value.length !== props.account.group_ids.length || groupIDs.value.some(id => !props.account.group_ids.includes(id)))
const tierChanged = computed(() => tierOptions.value.length > 0 && subscriptionTier.value !== (props.account.subscription_tier_override || ''))
const priorityChanged = computed(() => priority.value !== (props.account.priority ?? 50))
const hasChanges = computed(() => groupsChanged.value || dispatchChanged.value || tierChanged.value || priorityChanged.value)
const saving = ref(false)
const error = ref('')
const compatibleGroups = computed(() => props.groups.filter(group => group.platform === props.account.platform
  && (!group.require_oauth_only || props.account.type === 'oauth')
  && (props.account.dispatch_consent ? isSharedManualAssignmentGroup(group) : group.is_shared_pool && isDispatchGroup(group))))
const displayedGroups = computed(() => {
  const available = new Set(compatibleGroups.value.map(group => group.id))
  const groups = props.groups.filter(group => available.has(group.id) || props.account.group_ids.includes(group.id))
    .map(group => ({ id: group.id, name: group.name, rate: group.rate_multiplier as number | null, available: available.has(group.id) }))
  for (const id of props.account.group_ids) if (!groups.some(group => group.id === id)) groups.push({ id, name: `#${id}`, rate: null, available: false })
  return groups
})
function close() { if (!saving.value) emit('close') }
function setDispatch(value: boolean) {
  if (!saving.value) { dispatchEnabled.value = value; dispatchChanged.value = true }
}
function setTier(value: SelectOption['value']) {
  if (!saving.value && typeof value === 'string' && (!value || tierOptions.value.some(tier => tier.value === value))) subscriptionTier.value = value
}
async function save() {
  if (saving.value || !hasChanges.value) return
  if (groupsChanged.value && groupIDs.value.some(id => !compatibleGroups.value.some(group => group.id === id))) { error.value = t('sharedPool.allocationInvalid'); return }
  if (priorityChanged.value && !validSharedPriority(priority.value)) { error.value = t('sharedPool.invalidPriority'); return }
  saving.value = true; error.value = ''
  const input: SharedAccountAllocationInput = {}
  if (groupsChanged.value) input.group_ids = groupIDs.value
  if (dispatchChanged.value) { input.enabled = dispatchEnabled.value; input.admin_disabled = !dispatchEnabled.value }
  if (tierChanged.value) input.subscription_tier = subscriptionTier.value
  if (priorityChanged.value && validSharedPriority(priority.value)) input.priority = priority.value
  try { await adminSharedPoolAPI.allocate(props.account.id, input); emit('saved') }
  catch (e: unknown) { error.value = (e as Error).message || t('sharedPool.actionFailed') }
  finally { saving.value = false }
}
</script>
