<template>
  <span v-if="config.enabled" :title="t('admin.accounts.dailyCooldown.schedule', { ...config })" :class="active ? 'text-amber-700 dark:text-amber-400' : 'text-gray-500 dark:text-gray-400'" class="block text-xs" data-testid="daily-cooldown-badge">
    {{ t(active ? 'admin.accounts.dailyCooldown.active' : 'admin.accounts.dailyCooldown.scheduled') }}
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { isDailyCooldownActive, normalizeDailyCooldown } from '@/utils/dailyCooldown'

const props = defineProps<{ value: unknown; now: number }>()
const { t } = useI18n()
const config = computed(() => normalizeDailyCooldown(props.value))
const active = computed(() => isDailyCooldownActive(props.value, props.now))
</script>
