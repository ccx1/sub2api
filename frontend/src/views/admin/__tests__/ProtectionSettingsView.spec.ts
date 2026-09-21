import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import ProtectionSettingsView from '../ProtectionSettingsView.vue'
import type { ProtectionPreview, ProtectionSettingsUpdateResult, ProtectionStrategy } from '@/api/admin/accountProtection'

const mocks = vi.hoisted(() => ({
  getSettings: vi.fn(), saveSettings: vi.fn(), strategies: vi.fn(), getAccount: vi.fn(),
  preview: vi.fn(), apply: vi.fn(), showSuccess: vi.fn()
}))
const route = reactive<{ query: { account_id?: string } }>({ query: {} })
vi.mock('vue-router', () => ({ useRoute: () => route }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, te: () => false }) }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: mocks.showSuccess }) }))
vi.mock('@/api/admin/accounts', () => ({ getById: mocks.getAccount }))
vi.mock('@/api/admin/accountProtection', () => ({
  getProtectionSettings: mocks.getSettings, saveProtectionSettings: mocks.saveSettings,
  listProtectionStrategies: mocks.strategies, previewProtection: mocks.preview, applyProtection: mocks.apply
}))

const strategy = (id: string, supported = true): ProtectionStrategy => ({
  id, name: id, description: `${id} description`, category: 'common', identity_mode: 'full',
  tls_profile: 'nodejs22', max_concurrency: 8, risk: 'high', apply_supported: supported,
  requires_openai_oauth: true, diagnostic_only: false
})
const preview = (overrides: Partial<ProtectionPreview> = {}): ProtectionPreview => ({
  account_id: 42, enabled: true, active_mode: 'legacy', eligible: true,
  changes: [{ key: 'strategy', from: 'legacy', to: 'mode2' }], ...overrides
})
const makeWrapper = () => mount(ProtectionSettingsView, {
  global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } }
})

beforeEach(() => {
  vi.clearAllMocks()
  route.query = {}
  mocks.getSettings.mockResolvedValue({ default_mode: 'mode2' })
  mocks.saveSettings.mockImplementation(async settings => ({ ...settings, sync: { updated: 2, unchanged: 1, failed: [] } }))
  mocks.strategies.mockResolvedValue([strategy('mode2'), strategy('legacy'), strategy('native_baseline', false)])
  mocks.getAccount.mockResolvedValue({ id: 42, name: 'Codex account', platform: 'openai', type: 'oauth' })
  mocks.preview.mockResolvedValue(preview())
  mocks.apply.mockResolvedValue({ id: 42, name: 'Codex account', platform: 'openai', type: 'oauth' })
})

describe('ProtectionSettingsView', () => {
  it('loads the persisted default and saves a changed default only once while pending', async () => {
    let completeSave!: (value: ProtectionSettingsUpdateResult) => void
    mocks.saveSettings.mockImplementation(() => new Promise(resolve => { completeSave = resolve }))
    const wrapper = makeWrapper()
    await flushPromises()
    const select = wrapper.get<HTMLSelectElement>('[data-testid="default-mode"]')
    expect(select.element.value).toBe('mode2')
    expect(select.findAll('option')).toHaveLength(2)
    await select.setValue('legacy')
    const button = wrapper.get<HTMLButtonElement>('[data-testid="save-default"]')
    await button.trigger('submit')
    expect(mocks.saveSettings).toHaveBeenCalledWith({ default_mode: 'legacy' })
    expect(button.element.disabled).toBe(true)
    await button.trigger('submit')
    expect(mocks.saveSettings).toHaveBeenCalledTimes(1)
    completeSave({ default_mode: 'legacy', sync: { updated: 2, unchanged: 1, failed: [] } })
    await flushPromises()
    expect(button.element.disabled).toBe(false)
    expect(mocks.showSuccess).toHaveBeenCalledWith('accountProtection.saveSuccess')
    wrapper.unmount()
  })

  it('shows load errors with retry and preserves an unsaved choice after save failure', async () => {
    mocks.getSettings.mockRejectedValueOnce(new Error('Settings unavailable'))
    const wrapper = makeWrapper()
    await flushPromises()
    expect(wrapper.text()).toContain('Settings unavailable')
    await wrapper.get('[data-testid="load-retry"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="default-mode"]').setValue('legacy')
    mocks.saveSettings.mockRejectedValueOnce(new Error('Save rejected'))
    await wrapper.get('[data-testid="save-default"]').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('Save rejected')
    expect(wrapper.get<HTMLSelectElement>('[data-testid="default-mode"]').element.value).toBe('legacy')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="save-default"]').element.disabled).toBe(false)
    wrapper.unmount()
  })

  it('loads an account from the URL and requires a fresh preview after changing strategy', async () => {
    route.query = { account_id: '42' }
    const wrapper = makeWrapper()
    await flushPromises()
    expect(mocks.getAccount).toHaveBeenCalledWith(42)
    expect(mocks.preview).toHaveBeenCalledWith(42, 'mode2')
    expect(wrapper.get('[data-testid="current-strategy"]').text()).toContain('legacy')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="apply-strategy"]').element.disabled).toBe(false)
    await wrapper.get('[data-testid="target-mode"]').setValue('legacy')
    expect(wrapper.find('[data-testid="apply-strategy"]').exists()).toBe(false)
    await wrapper.get('[data-testid="inspect-account"]').trigger('submit')
    await flushPromises()
    expect(mocks.preview).toHaveBeenLastCalledWith(42, 'legacy')
    await wrapper.get('[data-testid="apply-strategy"]').trigger('click')
    await flushPromises()
    expect(mocks.apply).toHaveBeenCalledWith(42, 'legacy')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="apply-strategy"]').element.disabled).toBe(true)
    wrapper.unmount()
  })

  it('prevents applying an ineligible strategy and never offers native baseline as an application', async () => {
    route.query = { account_id: '42' }
    mocks.preview.mockResolvedValue(preview({ eligible: false, reason: 'Unsupported account' }))
    const wrapper = makeWrapper()
    await flushPromises()
    expect(wrapper.text()).toContain('Unsupported account')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="apply-strategy"]').element.disabled).toBe(true)
    expect(wrapper.get('[data-testid="target-mode"]').findAll('option').map(option => option.text())).toEqual(['mode2', 'legacy'])
    expect(mocks.apply).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('rejects invalid IDs and discards a preview for a previous account selection', async () => {
    const wrapper = makeWrapper()
    await flushPromises()
    await wrapper.get('[data-testid="account-id"]').setValue('-1')
    await wrapper.get('[data-testid="inspect-account"]').trigger('submit')
    expect(wrapper.text()).toContain('accountProtection.invalidId')
    expect(mocks.getAccount).not.toHaveBeenCalled()
    let completePreview!: (value: ProtectionPreview) => void
    mocks.preview.mockImplementationOnce(() => new Promise(resolve => { completePreview = resolve }))
    await wrapper.get('[data-testid="account-id"]').setValue('42')
    await wrapper.get('[data-testid="inspect-account"]').trigger('submit')
    await wrapper.get('[data-testid="account-id"]').setValue('43')
    completePreview(preview())
    await flushPromises()
    expect(wrapper.find('[data-testid="apply-strategy"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Codex account')
    wrapper.unmount()
  })

  it('locks applying while pending and invalidates the preview after a failed application', async () => {
    route.query = { account_id: '42' }
    let rejectApply!: (reason: Error) => void
    mocks.apply.mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectApply = reject }))
    const wrapper = makeWrapper()
    await flushPromises()
    const applyButton = wrapper.get<HTMLButtonElement>('[data-testid="apply-strategy"]')
    await applyButton.trigger('click')
    await applyButton.trigger('click')
    expect(mocks.apply).toHaveBeenCalledTimes(1)
    expect(wrapper.get<HTMLInputElement>('[data-testid="account-id"]').element.disabled).toBe(true)
    expect(wrapper.get<HTMLButtonElement>('[data-testid="save-default"]').element.disabled).toBe(true)
    rejectApply(new Error('Apply failed'))
    await flushPromises()
    expect(wrapper.text()).toContain('Apply failed')
    expect(wrapper.find('[data-testid="apply-strategy"]').exists()).toBe(false)
    expect(wrapper.get<HTMLButtonElement>('[data-testid="inspect-account"]').element.disabled).toBe(false)
    expect(wrapper.get<HTMLButtonElement>('[data-testid="save-default"]').element.disabled).toBe(false)
    wrapper.unmount()
  })

  it('re-inspects the latest route account when navigation changes during a pending preview', async () => {
    route.query = { account_id: '42' }
    let completePreview!: (value: ProtectionPreview) => void
    mocks.preview.mockImplementationOnce(() => new Promise(resolve => { completePreview = resolve }))
    const wrapper = makeWrapper()
    await flushPromises()
    expect(wrapper.get<HTMLButtonElement>('[data-testid="save-default"]').element.disabled).toBe(true)
    route.query = { account_id: '43' }
    await flushPromises()
    mocks.getAccount.mockResolvedValueOnce({ id: 43, name: 'Next account', platform: 'openai', type: 'oauth' })
    mocks.preview.mockResolvedValueOnce(preview({ account_id: 43 }))
    completePreview(preview())
    await flushPromises()
    expect(mocks.getAccount).toHaveBeenLastCalledWith(43)
    expect(mocks.preview).toHaveBeenLastCalledWith(43, 'mode2')
    expect(wrapper.text()).toContain('Next account')
    expect(wrapper.text()).not.toContain('Codex account')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="save-default"]').element.disabled).toBe(false)
    wrapper.unmount()
  })

  it('shows partial sync failures and allows saving the same default again', async () => {
    mocks.saveSettings.mockResolvedValueOnce({
      default_mode: 'mode2', sync: { updated: 1, unchanged: 2, failed: [{ account_id: 87, error: 'Account changed' }], error: 'Scan interrupted' }
    })
    const wrapper = makeWrapper()
    await flushPromises()
    await wrapper.get('[data-testid="save-default"]').trigger('submit')
    await flushPromises()
    expect(mocks.saveSettings).toHaveBeenCalledWith({ default_mode: 'mode2' })
    expect(wrapper.get('[data-testid="sync-result"]').text()).toContain('#87: Account changed')
    expect(wrapper.text()).toContain('accountProtection.syncIncomplete')
    expect(mocks.showSuccess).not.toHaveBeenCalled()
    expect(wrapper.get<HTMLButtonElement>('[data-testid="save-default"]').element.disabled).toBe(false)
    await wrapper.get('[data-testid="save-default"]').trigger('submit')
    await flushPromises()
    expect(mocks.saveSettings).toHaveBeenCalledTimes(2)
    expect(mocks.showSuccess).toHaveBeenCalledWith('accountProtection.saveSuccess')
    wrapper.unmount()
  })

  it('locks account application during global sync and refreshes the manually selected account afterward', async () => {
    let completeSave!: (value: ProtectionSettingsUpdateResult) => void
    mocks.saveSettings.mockImplementationOnce(() => new Promise(resolve => { completeSave = resolve }))
    const wrapper = makeWrapper()
    await flushPromises()
    await wrapper.get('[data-testid="account-id"]').setValue('42')
    await wrapper.get('[data-testid="inspect-account"]').trigger('submit')
    await flushPromises()
    await wrapper.get('[data-testid="default-mode"]').setValue('legacy')
    await wrapper.get('[data-testid="save-default"]').trigger('submit')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="apply-strategy"]').element.disabled).toBe(true)
    expect(wrapper.get<HTMLInputElement>('[data-testid="account-id"]').element.disabled).toBe(true)
    completeSave({ default_mode: 'legacy', sync: { updated: 2, unchanged: 1, failed: [] } })
    await flushPromises()
    expect(wrapper.get<HTMLInputElement>('[data-testid="account-id"]').element.value).toBe('42')
    expect(mocks.preview).toHaveBeenLastCalledWith(42, 'legacy')
    expect(mocks.preview).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })
})
