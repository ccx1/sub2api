<template>
  <Teleport to="body">
    <div ref="overlay" class="modal-overlay" style="z-index: 80" role="dialog" aria-modal="true" :aria-labelledby="titleId" data-testid="ticket-exchange-dialog">
      <div ref="panel" class="modal-content min-w-0 max-w-7xl" tabindex="-1">
        <div class="modal-header">
          <h2 :id="titleId" class="modal-title">{{ t('admin.accounts.codexTicketHistory.exchangeTitle') }}</h2>
          <button type="button" class="rounded-lg p-2 text-gray-500 hover:bg-gray-100 focus-visible:ring-2 focus-visible:ring-primary-500 dark:hover:bg-dark-700" :aria-label="t('common.close')" @click="emit('close')">
            <Icon name="x" size="md" />
          </button>
        </div>
        <div class="modal-body min-w-0 overscroll-contain" data-testid="ticket-exchange-body">
          <CodexTicketExchangeDetails :attempt="attempt" />
        </div>
        <div class="modal-footer">
          <button type="button" class="btn btn-secondary" @click="emit('close')">{{ t('common.close') }}</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script lang="ts">
let exchangeDialogCounter = 0
</script>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CodexTicketHistoryAttempt } from '@/api/admin/accounts'
import Icon from '@/components/icons/Icon.vue'
import CodexTicketExchangeDetails from './CodexTicketExchangeDetails.vue'

const props = defineProps<{ attempt: CodexTicketHistoryAttempt; opener: HTMLElement | null }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const titleId = `ticket-exchange-title-${++exchangeDialogCounter}`
const overlay = ref<HTMLElement | null>(null)
const panel = ref<HTMLElement | null>(null)
let lowerDialog: HTMLElement | null = null
let previousAriaHidden: string | null = null
let previousInert = false
let returnFocus: HTMLElement | null = null
let active = false

function focusableElements(): HTMLElement[] {
  return Array.from(panel.value?.querySelectorAll<HTMLElement>('button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])') ?? [])
}

function focusFirst(): void {
  const target = focusableElements()[0] ?? panel.value
  target?.focus({ preventScroll: true })
}

function handleKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    event.preventDefault()
    event.stopImmediatePropagation()
    emit('close')
    return
  }
  if (event.key !== 'Tab') return
  const elements = focusableElements()
  const first = elements[0]
  const last = elements[elements.length - 1]
  const active = document.activeElement
  if (!first || !panel.value?.contains(active) || (event.shiftKey ? active === first : active === last)) {
    event.preventDefault()
    const target = event.shiftKey ? last ?? panel.value : first ?? panel.value
    target?.focus({ preventScroll: true })
  }
}

function handleFocus(event: FocusEvent): void {
  if (panel.value?.contains(event.target as Node)) return
  // 允许 Clipboard 回退通过临时 textarea 同步复制，再收回焦点。
  queueMicrotask(() => {
    if (active && !panel.value?.contains(document.activeElement)) focusFirst()
  })
}

onMounted(() => {
  active = true
  returnFocus = props.opener ?? document.activeElement as HTMLElement | null
  lowerDialog = returnFocus?.closest<HTMLElement>('[role="dialog"]') ?? null
  if (lowerDialog) {
    previousAriaHidden = lowerDialog.getAttribute('aria-hidden')
    previousInert = lowerDialog.hasAttribute('inert')
  }
  focusFirst()
  lowerDialog?.setAttribute('inert', '')
  lowerDialog?.setAttribute('aria-hidden', 'true')
  document.addEventListener('keydown', handleKeydown, true)
  document.addEventListener('focusin', handleFocus, true)
})

function deactivate(): void {
  active = false
  document.removeEventListener('keydown', handleKeydown, true)
  document.removeEventListener('focusin', handleFocus, true)
  if (lowerDialog) {
    if (!previousInert) lowerDialog.removeAttribute('inert')
    if (previousAriaHidden === null) lowerDialog.removeAttribute('aria-hidden')
    else lowerDialog.setAttribute('aria-hidden', previousAriaHidden)
  }
}

defineExpose({ deactivate })

onBeforeUnmount(() => {
  deactivate()
  // 滚动锁由仍然打开的记录弹窗持有，子窗不增删 modal-open。
  if (overlay.value?.contains(document.activeElement) && returnFocus?.isConnected) returnFocus.focus({ preventScroll: true })
})
</script>
