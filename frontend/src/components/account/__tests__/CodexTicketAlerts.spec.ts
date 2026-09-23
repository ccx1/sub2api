import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import CodexTicketAlerts from '../CodexTicketAlerts.vue'
import type { CodexTicketAlertAccount } from '@/composables/useCodexTicketAlerts'

const { showWarning } = vi.hoisted(() => ({ showWarning: vi.fn() }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showWarning }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({
  t: (key: string, values?: Record<string, unknown>) => `${key}${values ? ` ${JSON.stringify(values)}` : ''}`
}) }))

type State = NonNullable<CodexTicketAlertAccount['codex_turn_tickets']>[number]['credential_state']
const account = (state: State = 'available', id = 1, model = 'gpt-6-astra'): CodexTicketAlertAccount => ({
  id, codex_turn_tickets: [{ model, credential_state: state, ready: state === 'available' || state === 'revalidation_required',
    remaining_seconds: 60, blocked: false }]
})
const preferenceKey = 'codex-ticket-desktop-alerts'
let wrappers: VueWrapper[] = []
let notification: ReturnType<typeof vi.fn> & { permission: NotificationPermission; requestPermission: ReturnType<typeof vi.fn> }
const render = (accounts = [account()]) => {
  const wrapper = mount(CodexTicketAlerts, { props: { accounts } })
  wrappers.push(wrapper)
  return wrapper
}
const click = async (wrapper: VueWrapper) => {
  await wrapper.get('button').trigger('click')
  await flushPromises()
}

beforeEach(() => {
  localStorage.clear()
  showWarning.mockReset()
  notification = Object.assign(vi.fn(), { permission: 'default' as NotificationPermission, requestPermission: vi.fn() })
  notification.requestPermission.mockImplementation(async () => {
    notification.permission = 'granted'
    return 'granted'
  })
  vi.stubGlobal('Notification', notification)
  vi.stubGlobal('isSecureContext', true)
})

afterEach(() => {
  wrappers.forEach(wrapper => wrapper.unmount())
  wrappers = []
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('CodexTicketAlerts', () => {
  it('does not request permission or alert on first load, including saved opt-in', () => {
    localStorage.setItem(preferenceKey, 'true')
    render([account('expired')])
    expect(notification.requestPermission).not.toHaveBeenCalled()
    expect(notification).not.toHaveBeenCalled()
    expect(showWarning).not.toHaveBeenCalled()
  })

  it('requests permission only on a click and saves the enabled preference', async () => {
    const wrapper = render()
    expect(wrapper.get('button').attributes('aria-pressed')).toBe('false')
    await click(wrapper)
    expect(notification.requestPermission).toHaveBeenCalledOnce()
    expect(wrapper.get('button').attributes('aria-pressed')).toBe('true')
    expect(localStorage.getItem(preferenceKey)).toBe('true')
  })

  it.each(['expired', 'revoked', 'missing'] as const)('notifies once when credentials become %s independently of blocked', async state => {
    const wrapper = render()
    await click(wrapper)
    await wrapper.setProps({ accounts: [account(state)] })
    expect(showWarning).toHaveBeenCalledOnce()
    expect(notification).toHaveBeenCalledOnce()
    await wrapper.setProps({ accounts: [account(state)] })
    expect(showWarning).toHaveBeenCalledOnce()
    expect(notification).toHaveBeenCalledOnce()
  })

  it('does not alert while disabled or when revalidation is due but credentials remain usable', async () => {
    const wrapper = render()
    await wrapper.setProps({ accounts: [account('expired')] })
    expect(showWarning).not.toHaveBeenCalled()
    await click(wrapper)
    await wrapper.setProps({ accounts: [account('revalidation_required')] })
    await wrapper.setProps({ accounts: [account('available')] })
    expect(showWarning).not.toHaveBeenCalled()
    expect(notification).not.toHaveBeenCalled()
  })

  it('coalesces one unavailable incident until actual recovery permits a new alert', async () => {
    const wrapper = render()
    await click(wrapper)
    for (const state of ['expired', 'revoked', 'missing'] as const) {
      await wrapper.setProps({ accounts: [account(state)] })
    }
    expect(showWarning).toHaveBeenCalledOnce()
    expect(notification).toHaveBeenCalledOnce()
    const notReady = account('available')
    notReady.codex_turn_tickets![0].ready = false
    await wrapper.setProps({ accounts: [notReady] })
    await wrapper.setProps({ accounts: [account('missing')] })
    expect(showWarning).toHaveBeenCalledOnce()
    await wrapper.setProps({ accounts: [account('revalidation_required')] })
    await wrapper.setProps({ accounts: [account('expired')] })
    expect(showWarning).toHaveBeenCalledTimes(2)
    expect(notification).toHaveBeenCalledTimes(2)
  })

  it('baselines pagination, filtering and newly appearing models without false transitions', async () => {
    const wrapper = render()
    await click(wrapper)
    await wrapper.setProps({ accounts: [account('expired', 2)] })
    await wrapper.setProps({ accounts: [] })
    await wrapper.setProps({ accounts: [account('expired', 1)] })
    await wrapper.setProps({ accounts: [account('expired', 1, 'gpt-5.6-sol')] })
    expect(showWarning).not.toHaveBeenCalled()
    expect(notification).not.toHaveBeenCalled()
  })

  it('keeps panel alerts after permission denial without repeatedly requesting permission', async () => {
    notification.requestPermission.mockImplementation(async () => {
      notification.permission = 'denied'
      return 'denied'
    })
    const wrapper = render()
    await click(wrapper)
    await click(wrapper)
    await click(wrapper)
    await wrapper.setProps({ accounts: [account('expired')] })
    expect(notification.requestPermission).toHaveBeenCalledOnce()
    expect(showWarning).toHaveBeenCalledOnce()
    expect(notification).not.toHaveBeenCalled()
  })

  it('retains panel alerts when notifications are unsupported or throw', async () => {
    vi.stubGlobal('Notification', undefined)
    const unsupported = render()
    await click(unsupported)
    await unsupported.setProps({ accounts: [account('expired')] })
    expect(showWarning).toHaveBeenCalledOnce()
    vi.stubGlobal('Notification', notification)
    notification.permission = 'granted'
    notification.mockImplementation(() => { throw new Error('Desktop unavailable') })
    const throwing = render()
    await throwing.setProps({ accounts: [account('expired')] })
    expect(showWarning).toHaveBeenCalledTimes(2)
  })

  it('coalesces failures without disclosing account names, identifiers or credentials', async () => {
    const initial = [account('available', 101), account('available', 102, 'gpt-5.6-sol')]
    const wrapper = render(initial)
    await click(wrapper)
    await wrapper.setProps({ accounts: initial.map(row => ({ ...row,
      name: 'private-account@example.invalid', credentials: { access_token: 'fixture-private-token' },
      codex_turn_tickets: row.codex_turn_tickets!.map(ticket => ({ ...ticket, ready: false, credential_state: 'expired' }))
    })) })
    expect(notification).toHaveBeenCalledOnce()
    const content = JSON.stringify(notification.mock.calls)
    expect(notification.mock.calls[0][1].body).toContain('"count":2')
    expect(content).toContain('gpt-6-astra')
    expect(content).not.toContain('private-account')
    expect(content).not.toContain('fixture-private-token')
    expect(content).not.toContain('101')
    expect(showWarning).toHaveBeenCalledOnce()
  })

  it('suppresses duplicate clicks while permission is pending and stops alerts after opt-out', async () => {
    let resolve!: (permission: NotificationPermission) => void
    notification.requestPermission.mockImplementation(() => new Promise<NotificationPermission>(done => { resolve = done }))
    const wrapper = render()
    await wrapper.get('button').trigger('click')
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
    await wrapper.get('button').trigger('click')
    expect(notification.requestPermission).toHaveBeenCalledOnce()
    notification.permission = 'granted'
    resolve('granted')
    await flushPromises()
    await click(wrapper)
    await wrapper.setProps({ accounts: [account('expired')] })
    expect(showWarning).not.toHaveBeenCalled()
    expect(notification).not.toHaveBeenCalled()
  })
})
