import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import CodexTicketSettingsView from '../CodexTicketSettingsView.vue'
import type { CodexTicketSettings } from '@/api/admin/codexTicketSettings'

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
  expect({ ...actual, tier_rules: [] }).toEqual({ ...expected, length_mode: expected.length_mode ?? 'strict', tier_rules: [] })
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
    await selectOption(wrapper, 'tier-0', 'Enterprise')
    await removeTag(wrapper, 'models', 'gpt-5.6-sol')
    mocks.save.mockRejectedValueOnce(new Error('Save rejected'))
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('Save rejected')
    expect(wrapper.get<HTMLInputElement>('[data-testid="length-0"]').element.value).toBe('342')
    expect(wrapper.get<HTMLInputElement>('[data-testid="backoff"]').element.value).toBe('90, 900')
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
