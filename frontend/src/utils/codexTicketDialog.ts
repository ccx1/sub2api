import { onBeforeUnmount, onMounted, type Ref } from 'vue'

export function useCodexTicketDialog(panel: Ref<HTMLElement | null>, opener: HTMLElement | null, close: () => void) {
  let lower: HTMLElement | null = null
  let previousHidden: string | null = null
  let previousInert = false
  let returnFocus: HTMLElement | null = null
  const focusables = () => Array.from(panel.value?.querySelectorAll<HTMLElement>('button:not([disabled]), input:not([disabled]), textarea:not([disabled]), select:not([disabled]), [href]') ?? [])
  function keydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopImmediatePropagation()
      close()
    }
    if (event.key !== 'Tab') return
    const elements = focusables()
    const first = elements[0] ?? panel.value
    const last = elements.at(-1) ?? panel.value
    if (!panel.value?.contains(document.activeElement) || (event.shiftKey ? document.activeElement === first : document.activeElement === last)) {
      event.preventDefault()
      ;(event.shiftKey ? last : first)?.focus()
    }
  }
  onMounted(() => {
    returnFocus = opener ?? document.activeElement as HTMLElement | null
    lower = returnFocus?.closest<HTMLElement>('[role="dialog"]') ?? null
    previousHidden = lower?.getAttribute('aria-hidden') ?? null
    previousInert = lower?.hasAttribute('inert') ?? false
    ;(focusables()[0] ?? panel.value)?.focus()
    lower?.setAttribute('inert', '')
    lower?.setAttribute('aria-hidden', 'true')
    document.addEventListener('keydown', keydown, true)
  })
  onBeforeUnmount(() => {
    document.removeEventListener('keydown', keydown, true)
    if (!previousInert) lower?.removeAttribute('inert')
    if (previousHidden === null) lower?.removeAttribute('aria-hidden')
    else lower?.setAttribute('aria-hidden', previousHidden)
    if (returnFocus?.isConnected) returnFocus.focus({ preventScroll: true })
  })
}
