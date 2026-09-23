import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import type { Account } from '@/types'

export type CodexTicketAlertAccount = Pick<Account, 'id' | 'codex_turn_tickets'>
type TicketState = NonNullable<Account['codex_turn_tickets']>[number]['credential_state']
type Snapshot = { key: string; model: string; state: TicketState; ready: boolean }
const preferenceKey = 'codex-ticket-desktop-alerts'
const unavailableStates = new Set<TicketState>(['expired', 'revoked', 'missing'])
const usableStates = new Set<TicketState>(['available', 'revalidation_required'])

function savedPreference() {
  try { return localStorage.getItem(preferenceKey) === 'true' } catch { return false }
}

export function useCodexTicketAlerts(accounts: () => CodexTicketAlertAccount[]) {
  const { t } = useI18n()
  const appStore = useAppStore()
  const enabled = ref(savedPreference())
  const requesting = ref(false)
  const supported = typeof window !== 'undefined' && typeof window.Notification === 'function'
    && window.isSecureContext !== false
  const permission = ref<NotificationPermission>(supported ? window.Notification.permission : 'denied')
  const desktopEnabled = computed(() => enabled.value && supported && permission.value === 'granted')
  let previous = new Map<string, Snapshot>()

  function observe(snapshot: Snapshot[]) {
    const changed = snapshot.filter(ticket => previous.get(ticket.key)?.ready
      && usableStates.has(previous.get(ticket.key)?.state)
      && unavailableStates.has(ticket.state) && !ticket.ready)
    // 翻页、筛选和新出现的模型仅建立基线，避免把未观察到的变化当成新故障。
    previous = new Map(snapshot.map(ticket => [ticket.key, ticket]))
    if (!enabled.value || changed.length === 0) return
    const models = [...new Set(changed.map(ticket => ticket.model))].slice(0, 3).join(', ')
    const message = t('admin.accounts.codexTicketAlerts.message', { count: changed.length, models })
    appStore.showWarning(message, 8000)
    notifyDesktop(message)
  }

  function notifyDesktop(message: string) {
    if (!supported) return
    permission.value = window.Notification.permission
    if (!desktopEnabled.value) return
    try {
      new window.Notification(t('admin.accounts.codexTicketAlerts.title'), {
        body: message, tag: 'codex-ticket-credentials'
      })
    } catch { /* 浏览器不支持构造通知时保留面板提醒。 */ }
  }

  async function toggle() {
    if (requesting.value) return
    if (enabled.value) enabled.value = false
    else {
      requesting.value = true
      try {
        if (supported && permission.value === 'default') {
          permission.value = await window.Notification.requestPermission()
        }
      } catch { /* 权限不可用时仍允许面板提醒。 */ }
      finally { requesting.value = false }
      enabled.value = true
    }
    try { localStorage.setItem(preferenceKey, String(enabled.value)) } catch { /* 本次页面仍生效。 */ }
  }

  watch(() => accounts().flatMap(account => (account.codex_turn_tickets ?? []).map(ticket => ({
    key: `${account.id}:${ticket.model}`, model: ticket.model, state: ticket.credential_state, ready: ticket.ready
  }))), observe, { immediate: true })

  return { enabled, requesting, desktopEnabled, toggle }
}
