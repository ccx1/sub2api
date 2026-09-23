<template>
  <button
    type="button"
    class="btn btn-secondary px-2 md:px-3"
    :class="enabled ? 'text-amber-700 dark:text-amber-400' : ''"
    :title="`${t(`${prefix}.scopeHint`)} ${t(`${prefix}.${desktopEnabled ? 'desktopHint' : 'panelHint'}`)}`"
    :aria-label="t(`${prefix}.${enabled ? 'enabled' : 'enable'}`)"
    :aria-pressed="enabled"
    :disabled="requesting"
    data-testid="codex-ticket-alerts"
    @click="toggle"
  >
    <Icon name="bell" size="sm" />
    <span class="hidden md:inline">{{ t(`${prefix}.${enabled ? 'enabled' : 'enable'}`) }}</span>
  </button>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useCodexTicketAlerts, type CodexTicketAlertAccount } from '@/composables/useCodexTicketAlerts'

const props = defineProps<{ accounts: CodexTicketAlertAccount[] }>()
const { t } = useI18n()
const prefix = 'admin.accounts.codexTicketAlerts'
const { enabled, requesting, desktopEnabled, toggle } = useCodexTicketAlerts(() => props.accounts)
</script>
