import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import CodexTicketSettingsView from '../CodexTicketSettingsView.vue'
import type { CodexTicketSettings } from '@/api/admin/codexTicketSettings'
import { defaultTicketProtection } from '@/components/admin/codexTicketSettingsForm'

const mocks = vi.hoisted(() => ({ load: vi.fn(), save: vi.fn() }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string, values?: { value?: string }) => values?.value ? `${key}:${values.value}` : key })
}))
vi.mock('@/api/admin/codexTicketSettings', () => ({ getCodexTicketSettings: mocks.load, saveCodexTicketSettings: mocks.save }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))

const settings = (): CodexTicketSettings => ({
  enabled: true, target_length: 292, ttl_seconds: 3600, refresh_before_seconds: 600,
  harvest_probe_interval_seconds: 6, harvest_attempt_timeout_seconds: 25, fail_closed: true,
  models: ['gpt-6-astra', 'gpt-5.6-sol'],
  tier_rules: [
    { tier: 'team', aliases: ['business', 'chatgptteam', 'chatgptbusiness', 'business_standard'], target_length: 332 },
    { tier: 'pro', aliases: ['chatgptpro'], target_length: 292 }
  ],
  rejected_lengths: [312], harvest_concurrency: 8, retry_backoff_seconds: [30, 60, 300, 600, 1200, 1800],
  retry_max_attempts: 6, retry_exhausted_cooldown_seconds: 1800, auth_cooldown_seconds: 300,
  rate_limit_cooldown_seconds: 300, respect_retry_after: true
})
type Page = ReturnType<typeof mount<typeof CodexTicketSettingsView>>
let wrapper: Page | undefined
const render = () => {
  wrapper = mount(CodexTicketSettingsView, {
    attachTo: document.body,
    global: { stubs: { RouterLink: { template: '<a><slot /></a>' }, transition: true } }
  })
  return wrapper
}
const tags = (page: Page, field: string) => page.get(`[data-testid="${field}"]`)
  .findAll('[data-testid="selected-tag"]').map(tag => tag.text())
const menu = () => document.body.querySelector<HTMLElement>('[role="listbox"]')!
const option = (label: string) => [...menu().querySelectorAll<HTMLElement>('[role="option"]')]
  .find(item => item.textContent?.trim() === label)!
async function openOptions(page: Page, field: string) {
  await page.get(`[data-testid="${field}"] .select-trigger`).trigger('click')
  expect(menu()).not.toBeNull()
}
async function selectOption(page: Page, field: string, label: string) {
  await openOptions(page, field)
  const choice = option(label)
  expect(choice.getAttribute('aria-disabled')).toBe('false')
  choice.click()
  await flushPromises()
}
async function removeTag(page: Page, field: string, value: string) {
  await page.get(`[data-testid="${field}"] [data-testid="tag-remove-${value}"]`).trigger('click')
}
function expectSaved(expected: CodexTicketSettings) {
  const actual = mocks.save.mock.calls.at(-1)![0] as CodexTicketSettings
  const entries = (value: CodexTicketSettings) => value.tier_rules.flatMap(rule =>
    [rule.tier, ...rule.aliases].map(name => ({ name, length: rule.target_length }))
  ).sort((a, b) => a.name.localeCompare(b.name))
  expect({ ...actual, tier_rules: [] }).toEqual({ ...expected, credential_mode: expected.credential_mode ?? 'state', cookie_ttl_seconds: expected.cookie_ttl_seconds ?? 20, cookie_refresh_before_seconds: expected.cookie_refresh_before_seconds ?? 5, pool_capacity: expected.pool_capacity ?? 5, verify_business: expected.verify_business ?? true, business_verification_rounds: expected.business_verification_rounds ?? 1, proxy_failure_threshold: expected.proxy_failure_threshold ?? 3, session_mode: expected.session_mode ?? 'random', refresh_strategy: expected.refresh_strategy ?? 'revalidate', protection: expected.protection ?? defaultTicketProtection(), length_mode: expected.length_mode ?? 'strict', tier_rules: [] })
  expect(entries(actual)).toEqual(entries(expected))
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.load.mockResolvedValue(settings())
  mocks.save.mockImplementation(async value => value)
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
})

describe('CodexTicketSettingsView', () => {
  it.each([undefined, 'revalidate', 'replace'] as const)('loads refresh strategy %s and preserves it through save and reload', async refresh_strategy => {
    mocks.load.mockResolvedValueOnce({ ...settings(), refresh_strategy })
    const page = render()
    await flushPromises()
    const expected = refresh_strategy ?? 'revalidate'
    const selector = page.get<HTMLSelectElement>('[data-testid="refresh-strategy"]')
    expect(selector.element.value).toBe(expected)
    expect(selector.attributes('aria-describedby')).toBe('ticket-refresh-strategy-hint')
    expect(page.get('[data-testid="refresh-strategy-hint"]').text()).toBe(`codexTicketSettings.refreshStrategy${expected === 'replace' ? 'Replace' : 'Revalidate'}Hint`)
    await page.get('[data-testid="auth_cooldown_seconds"]').setValue('900')
    await page.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...settings(), refresh_strategy: expected, auth_cooldown_seconds: 900 })
    page.unmount()
    mocks.load.mockResolvedValueOnce(mocks.save.mock.calls.at(-1)![0])
    const reloaded = render()
    await flushPromises()
    expect(reloaded.get<HTMLSelectElement>('[data-testid="refresh-strategy"]').element.value).toBe(expected)
  })

  it('switches refresh strategy timing labels and defaults an omitted save response', async () => {
    const page = render()
    await flushPromises()
    const timingFields = ['ttl_seconds', 'refresh_before_seconds', 'cookie_ttl_seconds', 'cookie_refresh_before_seconds']
    for (const refresh_strategy of ['replace', 'revalidate'] as const) {
      await page.get('[data-testid="refresh-strategy"]').setValue(refresh_strategy)
      for (const field of timingFields) {
        expect(page.get(`[data-testid="${field}"]`).element.closest('label')?.textContent)
          .toContain(`codexTicketSettings.${refresh_strategy === 'replace' ? 'updateFields' : 'fields'}.${field}`)
      }
      await page.get('form').trigger('submit')
      await flushPromises()
      expectSaved({ ...settings(), refresh_strategy })
    }
    mocks.save.mockResolvedValueOnce(settings())
    await page.get('[data-testid="refresh-strategy"]').setValue('replace')
    await page.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...settings(), refresh_strategy: 'replace' })
    expect(page.get<HTMLSelectElement>('[data-testid="refresh-strategy"]').element.value).toBe('revalidate')
  })

  it.each(['unsupported', '', null])('blocks invalid refresh strategy %s until explicitly corrected', async refresh_strategy => {
    mocks.load.mockResolvedValueOnce({ ...settings(), refresh_strategy })
    const page = render()
    await flushPromises()
    expect(page.get('[data-testid="validation-error"]').text()).toBe('codexTicketSettings.errors.refreshStrategy')
    expect(page.get<HTMLButtonElement>('[data-testid="save-settings"]').element.disabled).toBe(true)
    await page.get('form').trigger('submit')
    expect(mocks.save).not.toHaveBeenCalled()
    await page.get('[data-testid="refresh-strategy"]').setValue('replace')
    await page.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...settings(), refresh_strategy: 'replace' })
  })

  it.each([{ lengths: [] }, { lengths: null }])('keeps cleared protection lengths after save and reload with response $lengths', async ({ lengths }) => {
    const stored = { ...settings(), protection: { ...defaultTicketProtection(), reject_and_silence_lengths: lengths } }
    mocks.save.mockResolvedValueOnce(stored)
    const page = render()
    await flushPromises()
    await page.get('[data-testid="protection-lengths"]').setValue('')
    await page.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...settings(), protection: { ...defaultTicketProtection(), reject_and_silence_lengths: [] } })
    expect(page.text()).toContain('codexTicketSettings.saved')
    expect(page.get<HTMLInputElement>('[data-testid="protection-lengths"]').element.value).toBe('')
    page.unmount()
    mocks.load.mockResolvedValueOnce(stored)
    const reloaded = render()
    await flushPromises()
    expect(reloaded.find('form').exists()).toBe(true)
    expect(reloaded.get<HTMLInputElement>('[data-testid="protection-lengths"]').element.value).toBe('')
    await reloaded.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...settings(), protection: { ...defaultTicketProtection(), reject_and_silence_lengths: [] } })
  })

  it('defaults legacy credentials to STATE and saves experimental Cookie settings', async () => {
    const page = render()
    await flushPromises()
    expect(page.get<HTMLSelectElement>('[data-testid="credential-mode"]').element.value).toBe('state')
    expect(page.get<HTMLInputElement>('[data-testid="cookie_ttl_seconds"]').element.value).toBe('20')
    await page.get('[data-testid="credential-mode"]').setValue('cookie_state')
    expect(page.get<HTMLInputElement>('[data-testid="fail-closed"]').element.disabled).toBe(false)
    expect(page.find('[data-testid="cookie-fail-open-hint"]').exists()).toBe(false)
    expect(page.get<HTMLSelectElement>('[data-testid="length-mode"]').element.disabled).toBe(true)
    expect(page.get<HTMLFieldSetElement>('[data-testid="length-rules"]').element.disabled).toBe(true)
    expect(page.get<HTMLInputElement>('[data-testid="protection-lengths"]').element.disabled).toBe(true)
    expect(page.get('[data-testid="length-mode-hint"]').text()).toBe('codexTicketSettings.cookieLengthHint')
    await page.get('[data-testid="cookie_ttl_seconds"]').setValue('21')
    await page.get('[data-testid="cookie_refresh_before_seconds"]').setValue('0')
    await page.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...settings(), credential_mode: 'cookie_state', cookie_ttl_seconds: 21, cookie_refresh_before_seconds: 0 })
    await page.get('[data-testid="credential-mode"]').setValue('state')
    expect(page.get<HTMLInputElement>('[data-testid="fail-closed"]').element.disabled).toBe(false)
    expect(page.get<HTMLInputElement>('[data-testid="fail-closed"]').element.checked).toBe(true)
  })

  it.each(['cookie', 'cookie_state'] as const)('keeps the pause choice editable and persisted in %s mode', async credential_mode => {
    mocks.load.mockResolvedValueOnce({ ...settings(), credential_mode })
    const page = render()
    await flushPromises()
    const pause = page.get<HTMLInputElement>('[data-testid="fail-closed"]')
    for (const fail_closed of [true, false]) {
      expect(pause.element.matches(':disabled')).toBe(false)
      await pause.setValue(fail_closed)
      for (const mode of ['state', 'cookie', 'cookie_state', credential_mode]) {
        await page.get('[data-testid="credential-mode"]').setValue(mode)
        expect(pause.element.matches(':disabled')).toBe(false)
        expect(pause.element.checked).toBe(fail_closed)
      }
      await page.get('form').trigger('submit')
      await flushPromises()
      expectSaved({ ...settings(), credential_mode, fail_closed })
      expect(pause.element.checked).toBe(fail_closed)
    }
    page.unmount()
    mocks.load.mockResolvedValueOnce(mocks.save.mock.calls.at(-1)![0])
    const reloaded = render()
    await flushPromises()
    expect(reloaded.get<HTMLInputElement>('[data-testid="fail-closed"]').element.checked).toBe(false)
    expect(reloaded.get<HTMLInputElement>('[data-testid="fail-closed"]').element.matches(':disabled')).toBe(false)
  })

  it('loads Cookie mode and rejects expired-or-later refresh before saving', async () => {
    mocks.load.mockResolvedValueOnce({ ...settings(), credential_mode: 'cookie', cookie_ttl_seconds: 30, cookie_refresh_before_seconds: 3 })
    const page = render()
    await flushPromises()
    expect(page.get<HTMLSelectElement>('[data-testid="credential-mode"]').element.value).toBe('cookie')
    await page.get('[data-testid="cookie_refresh_before_seconds"]').setValue('30')
    expect(page.get('[data-testid="validation-error"]').text()).toBe('codexTicketSettings.errors.cookieRefresh')
    await page.get('form').trigger('submit')
    expect(mocks.save).not.toHaveBeenCalled()
  })

  it.each([undefined, null, 1, 20])('loads pool capacity %s and saves a usable default for old responses', async pool_capacity => {
    mocks.load.mockResolvedValueOnce({ ...settings(), pool_capacity })
    const page = render()
    await flushPromises()
    const control = page.get<HTMLInputElement>('[data-testid="pool_capacity"]')
    expect(control.element.value).toBe(String(pool_capacity ?? 5))
    expect(control.attributes()).toMatchObject({ min: '1', max: '20', step: '1', 'aria-describedby': 'ticket-pool-capacity-hint' })
    await page.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...settings(), pool_capacity: pool_capacity ?? 5 })
  })

  it('keeps edited pool capacity after a failed save and restores the server value after success', async () => {
    const page = render()
    await flushPromises()
    await page.get('[data-testid="pool_capacity"]').setValue('8')
    mocks.save.mockRejectedValueOnce(new Error('offline'))
    await page.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...settings(), pool_capacity: 8 })
    expect(page.get<HTMLInputElement>('[data-testid="pool_capacity"]').element.value).toBe('8')
    mocks.save.mockResolvedValueOnce({ ...settings(), pool_capacity: 6 })
    await page.get('form').trigger('submit')
    await flushPromises()
    expect(page.get<HTMLInputElement>('[data-testid="pool_capacity"]').element.value).toBe('6')
  })

  it.each(['0', '21', '1.5', ''])('blocks invalid pool capacity input %s', async value => {
    const page = render()
    await flushPromises()
    await page.get('[data-testid="pool_capacity"]').setValue(value)
    expect(page.get<HTMLButtonElement>('[data-testid="save-settings"]').element.disabled).toBe(true)
    await page.get('form').trigger('submit')
    expect(mocks.save).not.toHaveBeenCalled()
  })

  it.each([undefined, false, true])('loads business verification %s and submits an explicit boolean', async verify_business => {
    mocks.load.mockResolvedValueOnce({ ...settings(), verify_business })
    const wrapper = render()
    await flushPromises()
    const control = wrapper.get<HTMLInputElement>('[data-testid="verify-business"]')
    expect(control.element.checked).toBe(verify_business ?? true)
    expect(control.attributes('aria-describedby')).toBe('ticket-verify-business-hint')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.save.mock.calls.at(-1)![0].verify_business).toBe(verify_business ?? true)
  })

  it('preserves disabled verification after a failed save and restores the returned value', async () => {
    const wrapper = render()
    await flushPromises()
    const control = wrapper.get<HTMLInputElement>('[data-testid="verify-business"]')
    await control.setValue(false)
    mocks.save.mockRejectedValueOnce(new Error('offline'))
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(control.element.checked).toBe(false)
    expect(mocks.save.mock.calls.at(-1)![0].verify_business).toBe(false)
    mocks.save.mockResolvedValueOnce(settings())
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(control.element.checked).toBe(true)
    expect(wrapper.text()).toContain('codexTicketSettings.verifyBusinessHint')
  })

  it.each([undefined, 1, 5, 10])('loads quality rounds %s and saves the effective value', async business_verification_rounds => {
    mocks.load.mockResolvedValueOnce({ ...settings(), business_verification_rounds, models: ['gpt-5.6-sol'] })
    const page = render()
    await flushPromises()
    const control = page.get<HTMLInputElement>('[data-testid="business-verification-rounds"]')
    expect(control.element.value).toBe(String(business_verification_rounds ?? 1))
    expect(control.attributes()).toMatchObject({ min: '1', max: '10', step: '1', 'aria-describedby': 'ticket-business-verification-rounds-hint' })
    await page.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...settings(), business_verification_rounds: business_verification_rounds ?? 1, models: ['gpt-5.6-sol'] })
  })

  it('keeps quality rounds when verification is disabled and when saving fails', async () => {
    const page = render()
    await flushPromises()
    const rounds = page.get<HTMLInputElement>('[data-testid="business-verification-rounds"]')
    await rounds.setValue('5')
    await page.get('[data-testid="verify-business"]').setValue(false)
    expect(rounds.element.disabled).toBe(true)
    await page.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...settings(), verify_business: false, business_verification_rounds: 5 })
    await page.get('[data-testid="verify-business"]').setValue(true)
    expect(rounds.element.disabled).toBe(false)
    mocks.save.mockRejectedValueOnce(new Error('offline'))
    await page.get('form').trigger('submit')
    await flushPromises()
    expect(rounds.element.value).toBe('5')
    expectSaved({ ...settings(), business_verification_rounds: 5 })
    mocks.save.mockResolvedValueOnce({ ...settings(), business_verification_rounds: 3 })
    await page.get('form').trigger('submit')
    await flushPromises()
    expect(rounds.element.value).toBe('3')
  })

  it.each(['0', '11', '1.5', ''])('blocks invalid quality rounds input %s', async value => {
    const page = render()
    await flushPromises()
    await page.get('[data-testid="business-verification-rounds"]').setValue(value)
    expect(page.get('[data-testid="validation-error"]').text()).toBe('codexTicketSettings.errors.range')
    expect(page.get<HTMLButtonElement>('[data-testid="save-settings"]').element.disabled).toBe(true)
    await page.get('form').trigger('submit')
    expect(mocks.save).not.toHaveBeenCalled()
  })

  it('keeps rejection retry settings editable and saves them while shared protection is disabled', async () => {
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get<HTMLInputElement>('[data-testid="protection-enabled"]').element.checked).toBe(false)
    const section = wrapper.get('[data-testid="rejection-retry-settings"]')
    for (const [key, value] of Object.entries({ rejection_retry_interval_seconds: 45, rejection_retry_max_attempts: 8, rejection_retry_cooldown_seconds: 600 })) {
      const field = section.get<HTMLInputElement>(`[data-testid="${key}"]`)
      expect(field.element.matches(':disabled')).toBe(false)
      await field.setValue(String(value))
    }
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...settings(), protection: { ...defaultTicketProtection(), enabled: false,
      rejection_retry_interval_seconds: 45, rejection_retry_max_attempts: 8, rejection_retry_cooldown_seconds: 600 } })
  })

  it.each([undefined, 0])('loads legacy rejection retry defaults %s and saves edits independently of ordinary retries', async value => {
    mocks.load.mockResolvedValueOnce({ ...settings(), protection: { ...defaultTicketProtection(),
      rejection_retry_interval_seconds: value, rejection_retry_max_attempts: value, rejection_retry_cooldown_seconds: value } })
    const wrapper = render()
    await flushPromises()
    for (const [key, expected] of Object.entries({ rejection_retry_interval_seconds: 30, rejection_retry_max_attempts: 6, rejection_retry_cooldown_seconds: 300 })) {
      const field = wrapper.get<HTMLInputElement>(`[data-testid="${key}"]`)
      expect(field.element.value).toBe(String(expected))
      expect(field.attributes('aria-describedby')).toBe('ticket-rejection-retry-hint')
    }
    expect(wrapper.get('#ticket-rejection-retry-hint').text()).toBe('codexTicketSettings.rejectionRetryHint')
    await wrapper.get('[data-testid="rejection_retry_interval_seconds"]').setValue('45')
    await wrapper.get('[data-testid="rejection_retry_max_attempts"]').setValue('8')
    await wrapper.get('[data-testid="rejection_retry_cooldown_seconds"]').setValue('600')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...settings(), protection: { ...defaultTicketProtection(),
      rejection_retry_interval_seconds: 45, rejection_retry_max_attempts: 8, rejection_retry_cooldown_seconds: 600 } })
    expect(wrapper.get<HTMLInputElement>('[data-testid="rejection_retry_interval_seconds"]').element.value).toBe('45')
  })

  it.each([
    ['rejection_retry_interval_seconds', '-1'], ['rejection_retry_interval_seconds', '86401'],
    ['rejection_retry_max_attempts', '1.5'], ['rejection_retry_max_attempts', '1001'],
    ['rejection_retry_cooldown_seconds', '-1'], ['rejection_retry_cooldown_seconds', '86401']
  ])('blocks invalid policy rejection retry input %s=%s', async (field, value) => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get(`[data-testid="${field}"]`).setValue(value)
    expect(wrapper.get<HTMLButtonElement>('[data-testid="save-settings"]').element.disabled).toBe(true)
    await wrapper.get('form').trigger('submit')
    expect(mocks.save).not.toHaveBeenCalled()
  })

  it.each([undefined, 0, 1, 1000])('loads proxy failure threshold %s and saves its effective value', async proxy_failure_threshold => {
    mocks.load.mockResolvedValueOnce({ ...settings(), proxy_failure_threshold })
    const wrapper = render()
    await flushPromises()
    const expected = proxy_failure_threshold === undefined || proxy_failure_threshold === 0 ? 3 : proxy_failure_threshold
    const control = wrapper.get<HTMLInputElement>('[data-testid="proxy_failure_threshold"]')
    expect(control.element.value).toBe(String(expected))
    expect(control.attributes()).toMatchObject({ type: 'number', min: '1', max: '1000', step: '1' })
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...settings(), proxy_failure_threshold: expected })
  })

  it('sends an edited proxy threshold and fills the server value without losing Session mode', async () => {
    mocks.load.mockResolvedValueOnce({ ...settings(), session_mode: 'account_model' })
    mocks.save.mockResolvedValueOnce({ ...settings(), session_mode: 'account_model', proxy_failure_threshold: 4 })
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="proxy_failure_threshold"]').setValue('5')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...settings(), session_mode: 'account_model', proxy_failure_threshold: 5 })
    expect(wrapper.get<HTMLInputElement>('[data-testid="proxy_failure_threshold"]').element.value).toBe('4')
    expect(wrapper.get<HTMLSelectElement>('#ticket-session-mode').element.value).toBe('account_model')
    mocks.save.mockResolvedValueOnce(settings())
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get<HTMLInputElement>('[data-testid="proxy_failure_threshold"]').element.value).toBe('3')
  })

  it.each(['0', '-1', '1.5', '1001'])('rejects user-entered proxy failure threshold %s', async value => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="proxy_failure_threshold"]').setValue(value)
    expect(wrapper.get('[data-testid="validation-error"]').text()).toBe('codexTicketSettings.errors.range')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="save-settings"]').element.disabled).toBe(true)
    await wrapper.get('form').trigger('submit')
    expect(mocks.save).not.toHaveBeenCalled()
  })

  it.each([undefined, 'random', 'account', 'account_model'] as const)('loads Session mode %s and preserves it when saving other settings', async session_mode => {
    mocks.load.mockResolvedValueOnce({ ...settings(), session_mode })
    const wrapper = render()
    await flushPromises()
    const control = wrapper.get<HTMLSelectElement>('#ticket-session-mode')
    expect(control.element.value).toBe(session_mode ?? 'random')
    expect(control.attributes('aria-describedby')).toBe('ticket-session-mode-hint')
    await wrapper.get('[data-testid="auth_cooldown_seconds"]').setValue('900')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...settings(), session_mode: session_mode ?? 'random', auth_cooldown_seconds: 900 })
  })

  it('submits both fixed Session modes and restores random from an omitted save response field', async () => {
    const wrapper = render()
    await flushPromises()
    for (const session_mode of ['account', 'account_model'] as const) {
      await wrapper.get('#ticket-session-mode').setValue(session_mode)
      await wrapper.get('form').trigger('submit')
      await flushPromises()
      expectSaved({ ...settings(), session_mode })
      expect(wrapper.get<HTMLSelectElement>('#ticket-session-mode').element.value).toBe(session_mode)
    }
    mocks.save.mockResolvedValueOnce(settings())
    await wrapper.get('#ticket-session-mode').setValue('random')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...settings(), session_mode: 'random' })
    expect(wrapper.get<HTMLSelectElement>('#ticket-session-mode').element.value).toBe('random')
    expect(wrapper.text()).toContain('codexTicketSettings.saved')
  })

  it('blocks an unknown Session mode from the API until a supported choice is selected', async () => {
    mocks.load.mockResolvedValueOnce({ ...settings(), session_mode: 'unsupported' })
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get('[data-testid="validation-error"]').text()).toBe('codexTicketSettings.errors.sessionMode')
    await wrapper.get('form').trigger('submit')
    expect(mocks.save).not.toHaveBeenCalled()
    await wrapper.get('#ticket-session-mode').setValue('random')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...settings(), session_mode: 'random' })
  })

  it('loads legacy protection defaults and serializes enabled auto overrides and account limits', async () => {
    mocks.load.mockResolvedValueOnce({ ...settings(), length_mode: 'auto' })
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get<HTMLInputElement>('[data-testid="protection-enabled"]').element.checked).toBe(false)
    expect(wrapper.get<HTMLInputElement>('[data-testid="protection-lengths"]').element.value).toBe('312')
    await wrapper.get('[data-testid="protection-enabled"]').setValue(true)
    expect(wrapper.get('[data-testid="protection-auto-warning"]').text()).toBe('codexTicketSettings.protectionAutoHint')
    await wrapper.get('[data-testid="protection-lengths"]').setValue('312, 356')
    await wrapper.get('[data-testid="max_account_attempts"]').setValue('4')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...settings(), length_mode: 'auto', protection: { ...defaultTicketProtection(), enabled: true, reject_and_silence_lengths: [312, 356], max_account_attempts: 4 } })
  })
  it('loads legacy rules as strict, preserves disabled fields in auto and restores them when switching back', async () => {
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get<HTMLSelectElement>('[data-testid="length-mode"]').element.value).toBe('strict')
    await openOptions(wrapper, 'tier-0')
    await wrapper.get('[data-testid="length-mode"]').setValue('auto')
    expect(document.body.querySelector('[role="listbox"]')).toBeNull()
    expect(wrapper.get<HTMLFieldSetElement>('[data-testid="length-rules"]').element.disabled).toBe(true)
    expect(wrapper.get('[data-testid="length-mode-hint"]').text()).toBe('codexTicketSettings.lengthModeAutoHint')
    expect(wrapper.get<HTMLInputElement>('[data-testid="length-0"]').element.matches(':disabled')).toBe(true)
    expect(wrapper.get<HTMLInputElement>('[data-testid="target_length"]').element.matches(':disabled')).toBe(true)
    expect(wrapper.get<HTMLInputElement>('[data-testid="rejected-lengths"]').element.matches(':disabled')).toBe(true)
    expect(wrapper.get<HTMLButtonElement>('[data-testid="tier-0"] .select-trigger').element.disabled).toBe(true)
    expect(wrapper.get<HTMLButtonElement>('[data-testid="models"] .select-trigger').element.disabled).toBe(false)
    expect(wrapper.get<HTMLInputElement>('[data-testid="retry_max_attempts"]').element.matches(':disabled')).toBe(false)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...settings(), length_mode: 'auto' })
    expect(wrapper.get<HTMLSelectElement>('[data-testid="length-mode"]').element.value).toBe('auto')
    await wrapper.get('[data-testid="length-mode"]').setValue('strict')
    expect(wrapper.get<HTMLFieldSetElement>('[data-testid="length-rules"]').element.disabled).toBe(false)
    expect(tags(wrapper, 'tier-0')).toEqual(['Business Standard', 'Business'])
    expect(wrapper.get<HTMLInputElement>('[data-testid="length-0"]').element.value).toBe('332')
    expect(wrapper.get<HTMLInputElement>('[data-testid="rejected-lengths"]').element.value).toBe('312')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...settings(), length_mode: 'strict' })
  })

  it('allows 312 overlaps in auto without dropping the preserved strict rules', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="length-0"]').setValue('312')
    expect(wrapper.get('[data-testid="validation-error"]').text()).toBe('codexTicketSettings.errors.overlap')
    await wrapper.get('[data-testid="length-mode"]').setValue('auto')
    expect(wrapper.find('[data-testid="validation-error"]').exists()).toBe(false)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...settings(), length_mode: 'auto', tier_rules: [
      { ...settings().tier_rules[0], target_length: 312 }, settings().tier_rules[1]
    ] })
    await wrapper.get('[data-testid="length-mode"]').setValue('strict')
    expect(wrapper.get('[data-testid="validation-error"]').text()).toBe('codexTicketSettings.errors.overlap')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="save-settings"]').element.disabled).toBe(true)
  })

  it('rejects an unrecognized saved length mode until a supported mode is selected', async () => {
    mocks.load.mockResolvedValueOnce({ ...settings(), length_mode: 'unknown' })
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get('[data-testid="validation-error"]').text()).toBe('codexTicketSettings.errors.lengthMode')
    await wrapper.get('form').trigger('submit')
    expect(mocks.save).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="length-mode"]').setValue('auto')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...settings(), length_mode: 'auto' })
  })

  it('selects multiple tiers for one length and saves model selections and retry controls together', async () => {
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get<HTMLInputElement>('[data-testid="length-0"]').element.value).toBe('332')
    expect(wrapper.get<HTMLInputElement>('[data-testid="length-1"]').element.value).toBe('292')
    expect(tags(wrapper, 'tier-0')).toEqual(['Business Standard', 'Business'])
    expect(tags(wrapper, 'tier-1')).toEqual(['Pro 20x'])
    await wrapper.get('[data-testid="remove-tier-1"]').trigger('click')
    await wrapper.get('[data-testid="add-tier"]').trigger('click')
    await selectOption(wrapper, 'tier-1', 'Enterprise')
    await selectOption(wrapper, 'tier-1', 'Plus')
    expect(tags(wrapper, 'tier-1')).toEqual(['Enterprise', 'Plus'])
    await wrapper.get('[data-testid="length-1"]').setValue('352')
    await removeTag(wrapper, 'models', 'gpt-5.6-sol')
    await selectOption(wrapper, 'models', 'gpt-5.5')
    await wrapper.get('[data-testid="backoff"]').setValue('60, 120, 600')
    await wrapper.get('[data-testid="retry_max_attempts"]').setValue('0')
    await wrapper.get('[data-testid="auth_cooldown_seconds"]').setValue('900')
    await wrapper.get('[data-testid="enabled"]').setValue(false)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expectSaved({
      ...settings(), enabled: false, auth_cooldown_seconds: 900, retry_max_attempts: 0,
      models: ['gpt-6-astra', 'gpt-5.5'],
      retry_backoff_seconds: [60, 120, 600],
      tier_rules: [settings().tier_rules[0], { tier: 'enterprise', aliases: ['plus'], target_length: 352 }]
    })
    expect(wrapper.text()).toContain('codexTicketSettings.saved')
  })

  it('locks duplicate submissions and fills normalized values from the server after success', async () => {
    let complete!: (value: CodexTicketSettings) => void
    mocks.save.mockImplementation(() => new Promise(resolve => { complete = resolve }))
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="length-0"]').setValue('342')
    await openOptions(wrapper, 'models')
    await wrapper.get('form').trigger('submit')
    await wrapper.get('form').trigger('submit')
    expect(mocks.save).toHaveBeenCalledTimes(1)
    expect(wrapper.get<HTMLButtonElement>('[data-testid="save-settings"]').element.disabled).toBe(true)
    expect(wrapper.get<HTMLFieldSetElement>('fieldset').element.disabled).toBe(true)
    expect(wrapper.get<HTMLSelectElement>('[data-testid="length-mode"]').element.disabled).toBe(true)
    expect(wrapper.get<HTMLSelectElement>('[data-testid="refresh-strategy"]').element.disabled).toBe(true)
    expect(wrapper.get<HTMLInputElement>('[data-testid="fail-closed"]').element.matches(':disabled')).toBe(true)
    expect(wrapper.get<HTMLSelectElement>('#ticket-session-mode').element.matches(':disabled')).toBe(true)
    expect(document.body.querySelector('[role="listbox"]')).toBeNull()
    expect(wrapper.findAll<HTMLButtonElement>('.select-trigger').every(button => button.element.disabled)).toBe(true)
    complete({ ...settings(), models: ['gpt-5.5'], harvest_concurrency: 12 })
    await flushPromises()
    expect(wrapper.get<HTMLInputElement>('[data-testid="length-0"]').element.value).toBe('332')
    expect(tags(wrapper, 'models')).toEqual(['gpt-5.5'])
    expect(wrapper.get<HTMLInputElement>('[data-testid="harvest_concurrency"]').element.value).toBe('12')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="save-settings"]').element.disabled).toBe(false)
  })

  it('supports retry after loading failure and keeps every draft field when saving fails', async () => {
    mocks.load.mockRejectedValueOnce(new Error('Load unavailable'))
    const wrapper = render()
    await flushPromises()
    expect(wrapper.text()).toContain('Load unavailable')
    expect(wrapper.find('form').exists()).toBe(false)
    await wrapper.get('[data-testid="load-retry"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="length-0"]').setValue('342')
    await wrapper.get('[data-testid="backoff"]').setValue('90, 900')
    await wrapper.get('#ticket-session-mode').setValue('account_model')
    await wrapper.get('[data-testid="proxy_failure_threshold"]').setValue('7')
    await selectOption(wrapper, 'tier-0', 'Enterprise')
    await removeTag(wrapper, 'models', 'gpt-5.6-sol')
    mocks.save.mockRejectedValueOnce(new Error('Save rejected'))
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('Save rejected')
    expect(wrapper.get<HTMLInputElement>('[data-testid="length-0"]').element.value).toBe('342')
    expect(wrapper.get<HTMLInputElement>('[data-testid="backoff"]').element.value).toBe('90, 900')
    expect(wrapper.get<HTMLSelectElement>('#ticket-session-mode').element.value).toBe('account_model')
    expect(wrapper.get<HTMLInputElement>('[data-testid="proxy_failure_threshold"]').element.value).toBe('7')
    expect(tags(wrapper, 'tier-0')).toEqual(['Business Standard', 'Business', 'Enterprise'])
    expect(tags(wrapper, 'models')).toEqual(['gpt-6-astra'])
    const draft = mocks.save.mock.calls[0][0]
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.save).toHaveBeenCalledTimes(2)
    expect(mocks.save.mock.calls[1][0]).toEqual(draft)
  })

  it('searches existing tiers and models without allowing unknown text to create values', async () => {
    const wrapper = render()
    await flushPromises()
    for (const field of ['tier-0', 'models']) {
      const selected = tags(wrapper, field)
      await openOptions(wrapper, field)
      const search = menu().querySelector<HTMLInputElement>('input')!
      search.value = 'unrecognized-typo-value'
      search.dispatchEvent(new Event('input'))
      await nextTick()
      search.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
      await nextTick()
      expect(menu().querySelectorAll('[role="option"]')).toHaveLength(0)
      expect(tags(wrapper, field)).toEqual(selected)
      document.body.click()
      await nextTick()
    }
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expectSaved(settings())
  })

  it('disables tiers already assigned to another row and allows moving a removed tag', async () => {
    const wrapper = render()
    await flushPromises()
    await openOptions(wrapper, 'tier-1')
    const occupied = option('Business Standard')
    expect(occupied.getAttribute('aria-disabled')).toBe('true')
    occupied.click()
    await nextTick()
    expect(tags(wrapper, 'tier-1')).toEqual(['Pro 20x'])
    document.body.click()
    await nextTick()
    await removeTag(wrapper, 'tier-0', 'team')
    await selectOption(wrapper, 'tier-1', 'Business Standard')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...settings(), tier_rules: [
      { tier: 'business', aliases: ['chatgptbusiness'], target_length: 332 },
      { tier: 'pro', aliases: ['chatgptpro', 'team', 'chatgptteam', 'business_standard'], target_length: 292 }
    ] })
  })

  it('preserves saved custom tiers, exact aliases and models when editing other settings', async () => {
    const stored = { ...settings(), models: ['gpt-6-astra', 'saved-private-model'], tier_rules: [
      { tier: 'TEAM', aliases: ['old_enterprise', 'business'], target_length: 332 },
      { tier: 'pro', aliases: ['chatgpt_pro'], target_length: 292 }
    ] }
    mocks.load.mockResolvedValueOnce(stored)
    const wrapper = render()
    await flushPromises()
    expect(tags(wrapper, 'tier-0')).toContain('codexTicketSettings.existingSelection:old_enterprise')
    expect(tags(wrapper, 'models')).toContain('codexTicketSettings.existingSelection:saved-private-model')
    await wrapper.get('[data-testid="auth_cooldown_seconds"]').setValue('900')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...stored, auth_cooldown_seconds: 900 })
  })

  it('requires a subscription selection for every row and at least one model', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="add-tier"]').trigger('click')
    expect(wrapper.get('[data-testid="validation-error"]').text()).toBe('codexTicketSettings.errors.selectTier')
    await wrapper.get('form').trigger('submit')
    expect(mocks.save).not.toHaveBeenCalled()
    await selectOption(wrapper, 'tier-2', 'Free')
    expect(wrapper.find('[data-testid="validation-error"]').exists()).toBe(false)
    await removeTag(wrapper, 'models', 'gpt-6-astra')
    await removeTag(wrapper, 'models', 'gpt-5.6-sol')
    expect(wrapper.get('[data-testid="validation-error"]').text()).toBe('codexTicketSettings.errors.models')
    await wrapper.get('form').trigger('submit')
    expect(mocks.save).not.toHaveBeenCalled()
  })

  it.each([
    ['length-0', '312', 'overlap'],
    ['length-0', '15', 'tierLength'],
    ['backoff', '60, 30', 'backoff'],
    ['backoff', '', 'backoff'],
    ['refresh_before_seconds', '3600', 'refresh'],
    ['harvest_concurrency', '0', 'range'],
    ['retry_max_attempts', '-1', 'range'],
    ['rejected-lengths', '312, bad', 'rejected']
  ])('prevents submission for invalid %s', async (field, value, error) => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get(`[data-testid="${field}"]`).setValue(value)
    expect(wrapper.get('[data-testid="validation-error"]').text()).toBe(`codexTicketSettings.errors.${error}`)
    expect(wrapper.get<HTMLButtonElement>('[data-testid="save-settings"]').element.disabled).toBe(true)
    await wrapper.get('form').trigger('submit')
    expect(mocks.save).not.toHaveBeenCalled()
  })

  it('allows empty optional tier and rejected-length lists while preserving the fallback', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="remove-tier-0"]').trigger('click')
    await wrapper.get('[data-testid="remove-tier-0"]').trigger('click')
    await wrapper.get('[data-testid="rejected-lengths"]').setValue('')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expectSaved({ ...settings(), tier_rules: [], rejected_lengths: [] })
  })
})
