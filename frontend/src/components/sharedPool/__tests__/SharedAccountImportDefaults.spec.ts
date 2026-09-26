import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import SharedAccountDialog from '../SharedAccountDialog.vue'
import SharedAccountImportDialog from '../SharedAccountImportDialog.vue'
import SharedCredentialsForm from '../SharedCredentialsForm.vue'
import ExcelBPSOptionsFields from '@/components/account/ExcelBPSOptionsFields.vue'
import type { SharedAccount, SharedConfig, SharedImportDefaults } from '@/api/sharedPool'
import { defaultExcelBPSOptions, type ExcelBPSOptions } from '@/utils/excelBPSOptions'

const { create, update, importAccounts } = vi.hoisted(() => ({ create: vi.fn(), update: vi.fn(), importAccounts: vi.fn() }))
vi.mock('@/api/sharedPool', () => ({ sharedPoolAPI: { create, update, importAccounts } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showWarning: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin', () => ({ adminAPI: { grok: { getCapabilities: vi.fn() } } }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copied: false, copyToClipboard: vi.fn() }) }))
vi.mock('@/components/account/ModelWhitelistSelector.vue', () => ({ default: { props: ['modelValue'], emits: ['update:modelValue'], template: '<div />' } }))

type GlobalDefaults = { protection_enabled: boolean; codex_ticket_enabled: boolean; excel_bps_enabled: boolean; excel_bps_options: ExcelBPSOptions }
const configured: GlobalDefaults = {
  protection_enabled: false, codex_ticket_enabled: false, excel_bps_enabled: true,
  excel_bps_options: { models: ['gpt-6-astra'], auto_disable_on_403: true, cache_creation_as_input: true }
}
const editableConfigured = {
  protection_enabled: configured.protection_enabled, excel_bps_enabled: configured.excel_bps_enabled,
  excel_bps_options: configured.excel_bps_options
}
function config(importDefaults?: GlobalDefaults): SharedConfig & { import_defaults?: GlobalDefaults } {
  return { platforms: ['openai', 'anthropic'], max_concurrency: 9, platform_rate_bps: 2000, proxy_rate_bps: 100, settlement_multiplier: 1, import_defaults: importDefaults }
}
const stubs = { BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }, SharedCredentialsForm: true, SharedRevenueSplit: true }
const wrappers: Array<{ unmount: () => void }> = []
function creation(importDefaults?: GlobalDefaults, account?: SharedAccount) {
  const wrapper = mount(SharedAccountDialog, { props: { show: true, config: config(importDefaults), account }, global: { stubs } })
  wrappers.push(wrapper)
  return wrapper
}
function importing(importDefaults?: GlobalDefaults, initialDefaults?: SharedImportDefaults) {
  const wrapper = mount(SharedAccountImportDialog, { props: { show: true, config: config(importDefaults), initialDefaults }, global: { stubs } })
  wrappers.push(wrapper)
  return wrapper
}
async function saveCreation(wrapper: ReturnType<typeof creation>, planType = 'plus') {
  wrapper.getComponent(SharedCredentialsForm).vm.$emit('change', { access_token: 'fixture-token', plan_type: planType })
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  return create.mock.lastCall?.[0]
}
async function saveImport(wrapper: ReturnType<typeof importing>) {
  await wrapper.get('textarea').setValue('[{"platform":"openai","type":"oauth"}]')
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  return importAccounts.mock.lastCall?.[0].defaults
}

describe('shared account global import defaults', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    create.mockResolvedValue({})
    update.mockResolvedValue({})
    importAccounts.mockResolvedValue({ total: 1, created: 1, failed: 0, items: [], warnings: [] })
  })
  afterEach(() => { for (const wrapper of wrappers.splice(0)) wrapper.unmount() })

  it('seeds creation with only the approved global fields and retains shared rules', async () => {
    const wrapper = creation(configured)
    expect(wrapper.find('#shared-codex-ticket').exists()).toBe(false)
    expect(wrapper.getComponent(ExcelBPSOptionsFields).props('modelValue')).toEqual(configured.excel_bps_options)
    const payload = await saveCreation(wrapper)
    expect(payload).toMatchObject({ ...editableConfigured, enabled: true, dispatch_consent: true, concurrency: 1, proxy_url: '' })
    for (const key of ['codex_ticket_enabled', 'group_ids', 'priority', 'extra', 'daily_cooldown', 'proxy_id', 'proxy_mode']) expect(payload).not.toHaveProperty(key)
  })

  it('preserves global false values for every selectable flag', async () => {
    const defaults = { ...configured, excel_bps_enabled: false }
    const wrapper = creation(defaults)
    expect(await saveCreation(wrapper)).toMatchObject({ protection_enabled: false, excel_bps_enabled: false })
    expect(create.mock.lastCall?.[0]).not.toHaveProperty('codex_ticket_enabled')
    expect(create.mock.lastCall?.[0]).not.toHaveProperty('excel_bps_options')
  })

  it('keeps legacy creation and import defaults when global configuration is absent', async () => {
    const expected = { protection_enabled: true, excel_bps_enabled: false }
    expect(await saveCreation(creation())).toMatchObject(expected)
    expect(await saveImport(importing())).toMatchObject(expected)
  })

  it.each(['pro', 'chatgpt_pro', 'prolite'])('leaves globally configured tickets to server policy for %s credentials', async planType => {
    const wrapper = creation({ ...configured, excel_bps_enabled: false })
    expect(await saveCreation(wrapper, planType)).not.toHaveProperty('codex_ticket_enabled')
    expect(wrapper.find('#shared-codex-ticket').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('sharedPool.codexTicketRequiredHint')
  })

  it('omits global BPS and ticket flags for non-OpenAI and API-key creation', async () => {
    const wrapper = creation(configured)
    for (const selector of ['[data-account-type="apikey"]', '[data-platform="anthropic"]']) {
      await wrapper.get(selector).trigger('click')
      expect(wrapper.find('#shared-excel-bps').exists()).toBe(false)
      const payload = await saveCreation(wrapper)
      expect(payload).not.toHaveProperty('excel_bps_enabled')
      expect(payload).not.toHaveProperty('excel_bps_options')
      expect(payload).not.toHaveProperty('codex_ticket_enabled')
    }
  })

  it.each([false, true])('does not apply global defaults when editing existing BPS %s', async enabled => {
    const savedOptions = { models: null, auto_disable_on_403: false, cache_creation_as_input: false }
    const account = { id: 7, name: 'Existing', platform: 'openai', type: 'oauth', concurrency: 4, enabled: false, protection_enabled: true, excel_bps_enabled: enabled, excel_bps_options: savedOptions } as SharedAccount
    const wrapper = creation({ ...configured, excel_bps_enabled: !enabled }, account)
    expect(wrapper.get('#shared-excel-bps').attributes('aria-checked')).toBe(String(enabled))
    if (enabled) expect(wrapper.getComponent(ExcelBPSOptionsFields).props('modelValue')).toEqual(savedOptions)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(update).toHaveBeenCalledWith(7, { name: 'Existing', platform: 'openai', type: 'oauth', concurrency: 4, enabled: false, protection_enabled: true })
  })

  it('carries the current creation form into import ahead of changed global defaults', async () => {
    const wrapper = creation({ ...configured, excel_bps_enabled: false })
    wrapper.getComponent(SharedCredentialsForm).vm.$emit('change', { plan_type: 'pro' })
    await wrapper.get('#shared-concurrency').setValue(3)
    await wrapper.findAll('button').find(button => button.text() === 'sharedPool.importAccounts')!.trigger('click')
    const current = wrapper.emitted('import')?.[0]?.[0] as SharedImportDefaults
    expect(current).toMatchObject({ protection_enabled: false, excel_bps_enabled: false, concurrency: 3 })
    expect(current).not.toHaveProperty('codex_ticket_enabled')
    const imported = await saveImport(importing({ ...configured, protection_enabled: true, codex_ticket_enabled: true }, current))
    expect(imported).toMatchObject(current)
    expect(imported).not.toHaveProperty('excel_bps_options')
  })

  it('seeds direct import with global false values and BPS sub-options', async () => {
    const wrapper = importing(configured)
    expect(wrapper.find('#shared-import-codex-ticket').exists()).toBe(false)
    expect(wrapper.getComponent(ExcelBPSOptionsFields).props('modelValue')).toEqual(configured.excel_bps_options)
    expect(await saveImport(wrapper)).toMatchObject({ ...editableConfigured, concurrency: 1, proxy_url: '', enabled: true, dispatch_consent: true })
    expect(importAccounts.mock.lastCall?.[0].defaults).not.toHaveProperty('codex_ticket_enabled')
    expect(wrapper.text()).not.toContain('sharedPool.codexTicketRequiredHint')
  })

  it('keeps explicit import false values ahead of global true values', async () => {
    const initial = { concurrency: 2, enabled: true, protection_enabled: false, codex_ticket_enabled: false, excel_bps_enabled: false }
    const wrapper = importing({ ...configured, protection_enabled: true, codex_ticket_enabled: true }, initial)
    expect(await saveImport(wrapper)).toMatchObject({ concurrency: 2, enabled: true, protection_enabled: false, excel_bps_enabled: false })
    expect(importAccounts.mock.lastCall?.[0].defaults).not.toHaveProperty('codex_ticket_enabled')
    expect(importAccounts.mock.lastCall?.[0].defaults).not.toHaveProperty('excel_bps_options')
  })

  it.each([false, true])('does not mix global BPS options into an explicit %s import toggle', async enabled => {
    const wrapper = importing(configured, { concurrency: 1, enabled: true, protection_enabled: true, excel_bps_enabled: enabled })
    if (!enabled) await wrapper.get('#shared-import-excel-bps').trigger('click')
    expect(wrapper.getComponent(ExcelBPSOptionsFields).props('modelValue')).toEqual(defaultExcelBPSOptions())
    expect(wrapper.find('#shared-import-codex-ticket').exists()).toBe(false)
    expect(await saveImport(wrapper)).toMatchObject({ excel_bps_enabled: true, excel_bps_options: defaultExcelBPSOptions() })
    expect(importAccounts.mock.lastCall?.[0].defaults).not.toHaveProperty('codex_ticket_enabled')
  })

  it('keeps explicit import BPS options as one configuration family', async () => {
    const options = { models: [], auto_disable_on_403: false, cache_creation_as_input: false }
    const wrapper = importing(configured, { concurrency: 1, enabled: true, protection_enabled: true, excel_bps_enabled: true, excel_bps_options: options })
    expect(await saveImport(wrapper)).toMatchObject({ excel_bps_enabled: true, excel_bps_options: options })
    expect(importAccounts.mock.lastCall?.[0].defaults).not.toHaveProperty('codex_ticket_enabled')
  })
})
