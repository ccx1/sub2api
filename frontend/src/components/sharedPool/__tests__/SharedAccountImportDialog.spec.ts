import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import SharedAccountImportDialog from '../SharedAccountImportDialog.vue'
import DailyCooldownSettings from '@/components/account/DailyCooldownSettings.vue'
import type { SharedImportDefaults } from '@/api/sharedPool'

const { importAccounts, showSuccess, showWarning } = vi.hoisted(() => ({ importAccounts: vi.fn(), showSuccess: vi.fn(), showWarning: vi.fn() }))
vi.mock('@/api/sharedPool', () => ({ sharedPoolAPI: { importAccounts } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess, showWarning }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const config = { platforms: ['openai'] as const, max_concurrency: 10, platform_rate_bps: 2000, proxy_rate_bps: 100, settlement_multiplier: 1 }
const response = { total: 1, created: 1, failed: 0, items: [{ index: 1, source: 'text', name: 'My account', account_id: 10 }], warnings: [] }
function render(initialDefaults?: SharedImportDefaults) {
  return mount(SharedAccountImportDialog, {
    props: { show: true, config: { ...config, platforms: [...config.platforms] }, initialDefaults },
    global: { stubs: { BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' } } }
  })
}
function file(name: string, content: string) {
  return { name, size: content.length, type: 'application/json', text: () => Promise.resolve(content) } as File
}
async function choose(wrapper: ReturnType<typeof render>, files: File[]) {
  const input = wrapper.get('input[type="file"]')
  Object.defineProperty(input.element, 'files', { configurable: true, value: files })
  await input.trigger('change')
}

describe('shared account import', () => {
  beforeEach(() => { vi.clearAllMocks(); importAccounts.mockResolvedValue(response) })

  it.each([undefined, false, true])('automatically enables scheduling without a switch when inherited enabled is %s', async enabled => {
    const wrapper = render(enabled === undefined ? undefined : { enabled, dispatch_consent: false, concurrency: 1, protection_enabled: true })
    expect(wrapper.find('[role="switch"][aria-label="sharedPool.sharing"]').exists()).toBe(false)
    await wrapper.get('textarea').setValue('{}')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(importAccounts).toHaveBeenCalledWith(expect.objectContaining({ defaults: expect.objectContaining({ enabled: true, dispatch_consent: true }) }), expect.any(String))
    wrapper.unmount()
  })

  it('blocks import without settlement terms and accepts a zero multiplier', async () => {
    const wrapper = render()
    await wrapper.get('textarea').setValue('{}')
    await wrapper.setProps({ config: { ...config, platforms: [...config.platforms], settlement_multiplier: undefined } })
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(importAccounts).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toBe('sharedPool.settlementRequired')
    await wrapper.setProps({ config: { ...config, platforms: [...config.platforms], settlement_multiplier: 0 } })
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(importAccounts).toHaveBeenCalledWith(expect.objectContaining({ defaults: expect.objectContaining({ enabled: true, dispatch_consent: true }) }), expect.any(String))
    wrapper.unmount()
  })

  it('imports several files together and keeps user settings out of source data', async () => {
    const wrapper = render()
    await choose(wrapper, [file('export.json', '{"accounts":[]}'), file('auth.json', '{"tokens":{}}')])
    await wrapper.get('#shared-import-concurrency').setValue(4)
    await wrapper.get('#shared-import-proxy').setValue('socks5://proxy.example:1080')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(importAccounts).toHaveBeenCalledWith({
      sources: [{ name: 'export.json', content: '{"accounts":[]}' }, { name: 'auth.json', content: '{"tokens":{}}' }],
      defaults: { name: '', concurrency: 4, proxy_url: 'socks5://proxy.example:1080', enabled: true, dispatch_consent: true, protection_enabled: true, codex_ticket_enabled: true }
    }, expect.stringMatching(/^shared-import-/))
    expect(wrapper.emitted('imported')).toHaveLength(1)
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(showSuccess).toHaveBeenCalledWith('sharedPool.importResult')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('does not silently ignore text when files are selected', async () => {
    const wrapper = render()
    await choose(wrapper, [file('auth.json', '{}')])
    await wrapper.get('textarea').setValue('{}')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(importAccounts).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toBe('sharedPool.importSourceConflict')
    wrapper.unmount()
  })

  it('sends an explicit false ticket setting and uses a new retry key after changing it', async () => {
    importAccounts.mockRejectedValue(new Error('offline'))
    const wrapper = render()
    await wrapper.get('textarea').setValue('{}')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    await wrapper.get('#shared-import-codex-ticket').trigger('click')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(importAccounts.mock.calls[1][0].defaults.codex_ticket_enabled).toBe(false)
    expect(importAccounts.mock.calls[1][1]).not.toBe(importAccounts.mock.calls[0][1])
    wrapper.unmount()
  })

  it('blocks duplicate submits and closing while the request is pending', async () => {
    let resolve!: (value: unknown) => void
    importAccounts.mockImplementation(() => new Promise(done => { resolve = done }))
    const wrapper = render()
    await wrapper.get('textarea').setValue('{}')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    await wrapper.get('form').trigger('submit')
    expect(importAccounts).toHaveBeenCalledTimes(1)
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    expect(wrapper.findAll('button').find(button => button.text() === 'common.close')!.attributes('disabled')).toBeDefined()
    resolve(response)
    await flushPromises()
    wrapper.unmount()
  })

  it('reuses the operation key after network failure and changes it when content changes', async () => {
    importAccounts.mockRejectedValue(new Error('offline'))
    const wrapper = render()
    await wrapper.get('textarea').setValue('{}')
    for (let attempt = 0; attempt < 2; attempt++) {
      await wrapper.get('form').trigger('submit'); await flushPromises()
    }
    expect(importAccounts.mock.calls[0][1]).toBe(importAccounts.mock.calls[1][1])
    await wrapper.get('textarea').setValue('[]')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(importAccounts.mock.calls[2][1]).not.toBe(importAccounts.mock.calls[1][1])
    wrapper.unmount()
  })

  it('refreshes saved accounts on partial success and leaves failures visible', async () => {
    importAccounts.mockResolvedValue({ ...response, total: 2, failed: 1, items: [...response.items, { index: 2, source: 'auth.json', name: 'Expired', message: 'Expired token' }] })
    const wrapper = render()
    await wrapper.get('textarea').setValue('[]')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.emitted('imported')).toHaveLength(1)
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.get('[role="status"]').text()).toContain('Expired token')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(importAccounts.mock.calls[1][1]).not.toBe(importAccounts.mock.calls[0][1])
    wrapper.unmount()
  })

  it('keeps full failures open with their input and does not refresh accounts', async () => {
    importAccounts.mockResolvedValue({ total: 1, created: 0, failed: 1, items: [{ index: 1, source: 'text', name: 'Expired', message: 'Expired token' }], warnings: [] })
    const wrapper = render()
    await wrapper.get('textarea').setValue('[{}]')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.emitted('imported')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.get<HTMLTextAreaElement>('textarea').element.value).toBe('[{}]')
    expect(wrapper.get('[role="status"]').text()).toContain('Expired token')
    wrapper.unmount()
  })

  it('keeps successful import warnings visible after closing', async () => {
    importAccounts.mockResolvedValue({ ...response, warnings: ['Unsupported settings were ignored'] })
    const wrapper = render()
    await wrapper.get('textarea').setValue('{}')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(showWarning).toHaveBeenCalledWith('Unsupported settings were ignored')
    wrapper.unmount()
  })

  it('rejects oversized or non-JSON files before sending credentials', async () => {
    const wrapper = render()
    await choose(wrapper, [{ ...file('too-large.json', ''), size: 2 * 1024 * 1024 + 1 } as File])
    expect(wrapper.get('[role="alert"]').text()).toBe('sharedPool.importTooLarge')
    await choose(wrapper, [{ ...file('unexpected.exe', ''), type: 'application/octet-stream' } as File])
    expect(wrapper.get('[role="alert"]').text()).toBe('sharedPool.importUnsupported')
    expect(importAccounts).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('inherits the creation cooldown and includes it in import defaults without extra', async () => {
    const cooldown = { enabled: true, start: '23:00', end: '08:00', timezone: 'Asia/Shanghai' }
    const wrapper = render({ concurrency: 2, enabled: true, protection_enabled: true, daily_cooldown: cooldown })
    expect(wrapper.getComponent(DailyCooldownSettings).props('modelValue')).toEqual(cooldown)
    await wrapper.get('textarea').setValue('{}')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(importAccounts.mock.lastCall?.[0].defaults.daily_cooldown).toEqual(cooldown)
    expect(importAccounts.mock.lastCall?.[0].defaults).not.toHaveProperty('extra')
    wrapper.unmount()
  })

  it('configures a cooldown during import and sends a minimal disable when switched off', async () => {
    importAccounts.mockRejectedValue(new Error('offline'))
    const wrapper = render()
    const cooldown = { enabled: true, start: '02:00', end: '04:00', timezone: 'UTC' }
    wrapper.getComponent(DailyCooldownSettings).vm.$emit('update:modelValue', cooldown)
    await wrapper.get('textarea').setValue('{}')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(importAccounts.mock.lastCall?.[0].defaults.daily_cooldown).toEqual(cooldown)
    await wrapper.get('[data-testid="daily-cooldown-enabled"]').trigger('click')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(importAccounts.mock.lastCall?.[0].defaults.daily_cooldown).toEqual({ enabled: false })
    expect(importAccounts.mock.calls[1][1]).not.toBe(importAccounts.mock.calls[0][1])
    wrapper.unmount()
  })

  it('keeps an explicitly disabled inherited cooldown disabled', async () => {
    const wrapper = render({ concurrency: 1, enabled: false, protection_enabled: true, daily_cooldown: { enabled: false } })
    expect(wrapper.get('[data-testid="daily-cooldown-enabled"]').attributes('aria-checked')).toBe('false')
    await wrapper.get('textarea').setValue('{}')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(importAccounts.mock.lastCall?.[0].defaults.daily_cooldown).toEqual({ enabled: false })
    wrapper.unmount()
  })

  it.each([
    { start: '08:00', end: '08:00', timezone: 'UTC', error: 'equalTimes' },
    { start: '25:00', end: '08:00', timezone: 'UTC', error: 'invalidTime' },
    { start: '23:00', end: '08:00', timezone: 'Invalid/Zone', error: 'invalidTimezone' }
  ])('blocks import for $error', async ({ error, ...schedule }) => {
    const wrapper = render()
    wrapper.getComponent(DailyCooldownSettings).vm.$emit('update:modelValue', { enabled: true, ...schedule })
    await wrapper.get('textarea').setValue('{}')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(importAccounts).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain(`admin.accounts.dailyCooldown.${error}`)
    expect(wrapper.get('fieldset').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })
})
