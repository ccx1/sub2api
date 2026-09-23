import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import CodexTicketHistoryModal from '../CodexTicketHistoryModal.vue'
import accounts from '@/i18n/locales/zh/admin/accounts'
import type { CodexTicketHistory } from '@/api/admin/accounts'

const { getHistory, copyToClipboard } = vi.hoisted(() => ({ getHistory: vi.fn(), copyToClipboard: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { accounts: { getCodexTicketHistory: getHistory } } }))
vi.mock('@/api/admin/codexTicketDiagnostics', () => ({ getCodexTicketRuntimeStatus: vi.fn().mockResolvedValue({ state: 'idle', attempts_used: 0, max_attempts: 6, round: 0, max_rounds: 2, half_open: false, generation: 0 }), previewCodexTicketRequest: vi.fn() }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ locale: ref('zh'), t: translate, te: (key: string) => translate(key) !== key }) }))

function translate(key: string, params: Record<string, unknown> = {}): string {
  const messages = { admin: accounts, common: { refresh: '刷新', close: '关闭', loading: '加载中' } }
  const message = key.split('.').reduce<unknown>((value, part) => (value as Record<string, unknown> | undefined)?.[part], messages)
  return typeof message === 'string' ? message.replace(/\{(\w+)\}/g, (_, name: string) => String(params[name] ?? '')) : key
}

const Pagination = defineComponent({
  props: ['page', 'total', 'pageSize'],
  emits: ['update:page'],
  template: '<button data-testid="next-page" @click="$emit(\'update:page\', page + 1)">Next</button>'
})
const data: CodexTicketHistory = {
  summary: { total: 40, success: 20, failed: 20 }, total: 40, page: 2, page_size: 20, retained_limit: 100,
  items: [{
    id: 'attempt-2', started_at: '2026-09-21T08:00:00Z', finished_at: '2026-09-21T08:00:01Z',
    model: 'test-model', success: false, reason: 'harvest_model_mismatch',
    harvest_exchange: {
      capture_mode: 'raw', requested_model: 'test-model', reported_models: ['reported-model'],
      request: { method: 'POST', url: 'https://example.test/responses', headers: { Authorization: ['Bearer test-value'] }, body: '{"model":"test-model"}', body_bytes: 22 },
      response: { status_code: 200, body: 'raw response', body_bytes: 12 }
    }
  }]
}
const wrappers: Array<{ unmount: () => void }> = []
const originalScrollIntoView = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'scrollIntoView')
const scrollIntoView = vi.fn()

function node<T extends HTMLElement = HTMLElement>(selector: string): T {
  const element = document.querySelector<T>(selector)
  if (!element) throw new Error(`Missing element: ${selector}`)
  return element
}

async function mountHistory() {
  const entry = document.createElement('button')
  document.body.appendChild(entry)
  entry.focus()
  const wrapper = mount(CodexTicketHistoryModal, {
    attachTo: document.body,
    props: { show: true, account: { id: 1, name: 'Account' } },
    global: { stubs: { Pagination, Icon: true, Transition: true } }
  })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, entry }
}

async function openMessages() {
  const opener = node<HTMLButtonElement>('[data-testid="view-exchange"]')
  opener.focus()
  opener.click()
  await flushPromises()
  return opener
}

beforeEach(() => {
  getHistory.mockReset().mockResolvedValue(data)
  copyToClipboard.mockReset().mockResolvedValue(true)
  scrollIntoView.mockClear()
  Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', { configurable: true, value: scrollIntoView })
})

afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  document.body.innerHTML = ''
  document.body.classList.remove('modal-open')
  if (originalScrollIntoView) Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', originalScrollIntoView)
  else Reflect.deleteProperty(HTMLElement.prototype, 'scrollIntoView')
})

describe('Codex ticket messages nested dialog', () => {
  it('opens above the real history dialog without scrolling it and Escape closes only the child', async () => {
    const { wrapper } = await mountHistory()
    const parent = node('.modal-overlay')
    const parentBody = parent.querySelector<HTMLElement>('.modal-body')!
    parentBody.scrollTop = 420
    const opener = await openMessages()
    const child = node('[data-testid="ticket-exchange-dialog"]')
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(2)
    expect(child.style.zIndex).toBe('80')
    expect(parent.hasAttribute('inert')).toBe(true)
    expect(parent.getAttribute('aria-hidden')).toBe('true')
    expect(parentBody.scrollTop).toBe(420)
    expect(scrollIntoView).not.toHaveBeenCalled()
    expect(child.contains(document.activeElement)).toBe(true)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(document.querySelector('[data-testid="ticket-exchange-dialog"]')).toBeNull()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1)
    expect(parent.hasAttribute('inert')).toBe(false)
    expect(parent.hasAttribute('aria-hidden')).toBe(false)
    expect(parentBody.scrollTop).toBe(420)
    expect(document.activeElement).toBe(opener)
    expect(document.body.classList.contains('modal-open')).toBe(true)
    expect(wrapper.findComponent(Pagination).props('page')).toBe(2)
    expect(getHistory).toHaveBeenCalledTimes(1)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('traps keyboard focus inside the upper dialog and copies raw message content there', async () => {
    await mountHistory()
    const opener = await openMessages()
    const child = node('[data-testid="ticket-exchange-dialog"]')
    const buttons = child.querySelectorAll<HTMLButtonElement>('button')
    expect(document.activeElement).toBe(buttons[0])
    document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true }))
    expect(document.activeElement).toBe(buttons[buttons.length - 1])
    document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }))
    expect(document.activeElement).toBe(buttons[0])
    opener.focus()
    await flushPromises()
    expect(child.contains(document.activeElement)).toBe(true)
    node<HTMLButtonElement>('[data-testid="harvest-request"] button').click()
    await flushPromises()
    expect(copyToClipboard.mock.calls[0]![0]).toBe('POST https://example.test/responses\nAuthorization: Bearer test-value\n\n{"model":"test-model"}')
    expect(node('[data-testid="ticket-exchange-body"]').classList.contains('overscroll-contain')).toBe(true)
  })

  it('allows temporary textarea focus for HTTP clipboard fallback and returns focus to the child', async () => {
    await mountHistory()
    await openMessages()
    copyToClipboard.mockImplementation(() => {
      const textarea = document.createElement('textarea')
      textarea.readOnly = true
      document.body.appendChild(textarea)
      textarea.focus()
      expect(document.activeElement).toBe(textarea)
      textarea.remove()
      return Promise.resolve(true)
    })
    node<HTMLButtonElement>('[data-testid="harvest-request"] button').click()
    await flushPromises()
    expect(copyToClipboard).toHaveBeenCalledTimes(1)
    expect(node('[data-testid="ticket-exchange-dialog"]').contains(document.activeElement)).toBe(true)
  })

  it('closing the parent closes the child, clears the lock, and restores the outside focus', async () => {
    const { wrapper, entry } = await mountHistory()
    await openMessages()
    await wrapper.setProps({ show: false })
    await flushPromises()
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
    expect(document.body.classList.contains('modal-open')).toBe(false)
    expect(document.activeElement).toBe(entry)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it.each(['account', 'refresh'])('closes messages on %s changes while keeping the parent open', async action => {
    const { wrapper } = await mountHistory()
    await openMessages()
    if (action === 'account') await wrapper.setProps({ account: { id: 2, name: 'Second account' } })
    else node<HTMLButtonElement>('.modal-overlay .modal-footer button').click()
    await flushPromises()
    expect(document.querySelector('[data-testid="ticket-exchange-dialog"]')).toBeNull()
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1)
    expect(node('.modal-overlay').hasAttribute('inert')).toBe(false)
    expect(document.body.classList.contains('modal-open')).toBe(true)
    expect(getHistory).toHaveBeenCalledTimes(2)
  })

  it('unmounting clears listeners, queued focus recovery, both overlays and the body lock', async () => {
    const { wrapper, entry } = await mountHistory()
    await openMessages()
    entry.focus()
    wrapper.unmount()
    wrappers.pop()
    await flushPromises()
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
    expect(document.body.classList.contains('modal-open')).toBe(false)
    expect(document.activeElement).toBe(entry)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(wrapper.emitted('close')).toBeUndefined()
  })
})
