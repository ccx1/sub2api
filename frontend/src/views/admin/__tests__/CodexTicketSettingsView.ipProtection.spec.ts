import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CodexTicketSettingsView from '../CodexTicketSettingsView.vue'
import type { CodexTicketSettings } from '@/api/admin/codexTicketSettings'
import { defaultTicketProtection } from '@/components/admin/codexTicketSettingsForm'

const mocks = vi.hoisted(() => ({ load: vi.fn(), save: vi.fn() }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))
vi.mock('@/api/admin/codexTicketSettings', () => ({ getCodexTicketSettings: mocks.load, saveCodexTicketSettings: mocks.save }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))

const settings = (): CodexTicketSettings => ({
  enabled: true, target_length: 292, ttl_seconds: 3600, refresh_before_seconds: 600,
  harvest_probe_interval_seconds: 6, harvest_attempt_timeout_seconds: 25, fail_closed: true,
  models: ['gpt-6-astra'], tier_rules: [], rejected_lengths: [312], harvest_concurrency: 8,
  retry_backoff_seconds: [30, 60, 300], retry_max_attempts: 6, retry_exhausted_cooldown_seconds: 1800,
  auth_cooldown_seconds: 300, rate_limit_cooldown_seconds: 300, respect_retry_after: true
})
let page: ReturnType<typeof mount<typeof CodexTicketSettingsView>> | undefined
function render() {
  page = mount(CodexTicketSettingsView, {
    global: { stubs: { RouterLink: true, CodexTicketTagSelect: true, CodexModelQuality: true } }
  })
  return page
}
beforeEach(() => {
  vi.resetAllMocks()
  mocks.load.mockResolvedValue(settings())
  mocks.save.mockImplementation(async value => value)
})
afterEach(() => page?.unmount())

describe('ticket IP protection settings', () => {
  it('saves independently of shared protection and keeps values when switched off and reloaded', async () => {
    const view = render()
    await flushPromises()
    const section = view.get('[data-testid="ip-protection-settings"]')
    expect(section.get('fieldset').attributes()).toHaveProperty('disabled')
    expect(section.text()).toContain('codexTicketSettings.ipProtectionPersistenceHint')
    await section.get('[data-testid="proxy_ip_protection_enabled"]').setValue(true)
    expect(section.get('fieldset').attributes('disabled')).toBeUndefined()
    for (const [key, value] of Object.entries({ proxy_ip_failure_account_threshold: 7, proxy_ip_failure_window_seconds: 120, proxy_ip_cooldown_seconds: 900, proxy_ip_max_rounds: 4 })) {
      await section.get(`[data-testid="${key}"]`).setValue(value)
    }
    await view.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.save.mock.lastCall?.[0].protection).toEqual({ ...defaultTicketProtection(),
      proxy_ip_protection_enabled: true, proxy_ip_failure_account_threshold: 7,
      proxy_ip_failure_window_seconds: 120, proxy_ip_cooldown_seconds: 900, proxy_ip_max_rounds: 4 })
    await section.get('[data-testid="proxy_ip_protection_enabled"]').setValue(false)
    await view.get('form').trigger('submit')
    await flushPromises()
    const saved = mocks.save.mock.lastCall?.[0] as CodexTicketSettings
    expect(saved.protection).toMatchObject({ enabled: false, proxy_ip_protection_enabled: false, proxy_ip_failure_window_seconds: 120 })
    view.unmount()
    mocks.load.mockResolvedValueOnce(saved)
    const reloaded = render()
    await flushPromises()
    expect(reloaded.get<HTMLInputElement>('[data-testid="proxy_ip_failure_window_seconds"]').element.value).toBe('120')
    expect(reloaded.get<HTMLInputElement>('[data-testid="proxy_ip_protection_enabled"]').element.checked).toBe(false)
  })

  it('keeps unsaved settings after a failed save and blocks malformed values', async () => {
    const view = render()
    await flushPromises()
    await view.get('[data-testid="proxy_ip_protection_enabled"]').setValue(true)
    await view.get('[data-testid="proxy_ip_failure_window_seconds"]').setValue(86401)
    await view.get('form').trigger('submit')
    expect(mocks.save).not.toHaveBeenCalled()
    expect(view.get('[data-testid="validation-error"]').text()).toBe('codexTicketSettings.errors.range')
    await view.get('[data-testid="proxy_ip_failure_window_seconds"]').setValue(120)
    mocks.save.mockRejectedValueOnce(new Error('temporary save failure'))
    await view.get('form').trigger('submit')
    await flushPromises()
    expect(view.text()).toContain('temporary save failure')
    expect(view.get<HTMLInputElement>('[data-testid="proxy_ip_protection_enabled"]').element.checked).toBe(true)
    expect(view.get<HTMLInputElement>('[data-testid="proxy_ip_failure_window_seconds"]').element.value).toBe('120')
  })
})
