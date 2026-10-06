<template>
  <section class="min-w-0 space-y-3" :data-testid="`${side}-scope`">
    <label class="block text-sm font-medium">
      {{ label }}
      <select class="input mt-2 w-full" :value="selection" :disabled="disabled" :data-testid="`${side}-selection`" @change="emit('update:selection', ($event.target as HTMLSelectElement).value as 'accounts' | 'groups')">
        <option value="accounts">{{ t(`${p}.selectAccounts`) }}</option>
        <option value="groups">{{ t(`${p}.selectGroups`) }}</option>
      </select>
    </label>
    <p v-if="side === 'target'" class="text-xs leading-6 text-gray-500 dark:text-gray-400">{{ t(`${p}.sourcePriorityHint`) }}</p>
    <AstraAccountPicker v-if="selection !== 'groups'" :model-value="accountIds" :label="label" :accounts="accounts" :disabled="disabled || !accountsLoaded" @update:model-value="emit('update:accountIds', $event)" />
    <div v-else class="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
      <p class="text-xs leading-6 text-gray-500 dark:text-gray-400">{{ t(`${p}.dynamicGroupsHint`) }}</p>
      <input v-model="search" class="input my-3 w-full" type="search" :aria-label="t(`${p}.searchGroups`)" :placeholder="t(`${p}.searchGroups`)" />
      <p class="mb-2 text-xs text-gray-500">{{ t(`${p}.selectedGroups`, { n: groupIds.length }) }}</p>
      <div class="max-h-52 space-y-1 overflow-y-auto" :data-testid="`${side}-groups`">
        <label v-for="group in filteredGroups" :key="group.id" class="flex items-center gap-2 rounded-lg px-2 py-2 text-sm hover:bg-gray-50 dark:hover:bg-dark-700">
          <input type="checkbox" :value="group.id" :checked="groupIds.includes(group.id)" :disabled="disabled || !groupsLoaded || (!groupIds.includes(group.id) && groupIds.length >= 100)" @change="selectGroup(group.id, ($event.target as HTMLInputElement).checked)" />
          <span class="min-w-0 break-words">#{{ group.id }} · {{ group.name }}</span>
        </label>
        <p v-if="groupsLoaded && !filteredGroups.length" class="text-sm text-gray-500">{{ t(`${p}.noGroups`) }}</p>
      </div>
      <div v-if="groupsLoaded && missingGroups.length" class="mt-3 space-y-2">
        <p class="text-xs text-amber-600">{{ t(`${p}.missingGroups`) }}</p>
        <button v-for="id in missingGroups" :key="id" type="button" class="btn btn-secondary btn-sm mr-2" :disabled="disabled" @click="selectGroup(id, false)">#{{ id }} · {{ t('common.remove') }}</button>
      </div>
      <p v-if="accountsLoaded && groupsLoaded" class="mt-3 text-sm" :class="previewCount > 64 ? 'text-amber-600' : 'text-gray-500'" :data-testid="`${side}-preview`">{{ t(`${p}.groupPreview`, { n: previewCount }) }}</p>
      <p class="mt-1 text-xs text-gray-500">{{ t(`${p}.previewHint`) }}</p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AstraAccountPicker from './AstraAccountPicker.vue'
import type { AstraSelectableAccount, AstraSelectionSide } from '@/utils/astraBorrowingSelection'
const props = defineProps<{
  side: AstraSelectionSide; label: string; selection?: 'accounts' | 'groups'; accountIds: number[]; groupIds: number[]
  accounts: AstraSelectableAccount[]; groups: { id: number; name: string }[]; accountsLoaded: boolean; groupsLoaded: boolean; disabled?: boolean
}>()
const emit = defineEmits<{
  'update:selection': [value: 'accounts' | 'groups']; 'update:accountIds': [value: number[]]; 'update:groupIds': [value: number[]]
}>()
const { t } = useI18n()
const p = 'admin.astraGateway'
const search = ref('')
const filteredGroups = computed(() => props.groups.filter(group => `${group.id} ${group.name}`.toLowerCase().includes(search.value.trim().toLowerCase())))
const missingGroups = computed(() => props.groupIds.filter(id => !props.groups.some(group => group.id === id)))
const previewCount = computed(() => props.accounts.filter(account => account.group_ids?.some(id => props.groupIds.includes(id))).length)
function selectGroup(id: number, checked: boolean) {
  emit('update:groupIds', checked ? [...new Set([...props.groupIds, id])] : props.groupIds.filter(value => value !== id))
}
</script>
