import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import AccountImportSettingsModal from '../AccountImportSettingsModal.vue'
import ProxySelector from '@/components/common/ProxySelector.vue'
import RandomProxySettings from '@/components/account/RandomProxySettings.vue'
import { defaultAccountImportSettings } from '@/api/admin/accountImportSettings'

const { getSettings, saveSettings, getProxies, listGroups, showSuccess } = vi.hoisted(() => ({
  getSettings: vi.fn(), saveSettings: vi.fn(), getProxies: vi.fn(), listGroups: vi.fn(), showSuccess: vi.fn()
}))
vi.mock('@/api/admin/accountImportSettings', async importOriginal => ({
  ...await importOriginal<typeof import('@/api/admin/accountImportSettings')>(),
  getAccountImportSettings: getSettings, saveAccountImportSettings: saveSettings
}))
vi.mock('@/api/admin', () => ({ adminAPI: { proxies: { getAll: getProxies, listGroups } } }))
vi.mock('@/api/client', () => ({ apiClient: {} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const BaseDialogStub = defineComponent({
  name: 'BaseDialog', props: ['show'], template: '<section v-if="show"><slot /><slot name="footer" /></section>'
})
const proxies = [
  { id: 7, name: 'Japan', protocol: 'http', host: 'jp.example', port: 8080, status: 'active', country_code: 'JP' },
  { id: 8, name: 'Expired', protocol: 'http', host: 'old.example', port: 8080, status: 'inactive', country_code: 'JP' }
]
const mountModal = () => mount(AccountImportSettingsModal, { props: { show: true }, global: { stubs: { BaseDialog: BaseDialogStub } } })
const enable = (wrapper: ReturnType<typeof mountModal>) => wrapper.get('[data-testid="import-settings-enabled"]').trigger('click')
const submit = (wrapper: ReturnType<typeof mountModal>) => wrapper.get('form').trigger('submit')

describe('AccountImportSettingsModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getSettings.mockReset().mockResolvedValue(defaultAccountImportSettings())
    getProxies.mockReset().mockResolvedValue(proxies)
    listGroups.mockReset().mockResolvedValue([{ id: 3, name: 'Japan pool', proxy_count: 1, active_proxy_count: 1 }])
    saveSettings.mockReset().mockImplementation(async settings => settings)
  })

  it('loads disabled defaults and preserves unspecified region and ticket proxy fields', async () => {
    const wrapper = mountModal()
    expect(wrapper.get('[role="status"]').text()).toBe('common.loading')
    expect(wrapper.get('[data-testid="import-settings-save"]').attributes('disabled')).toBeDefined()
    await flushPromises()
    expect(wrapper.text()).toContain('admin.accountImportSettings.disabledHint')
    await submit(wrapper)
    await flushPromises()
    expect(saveSettings).toHaveBeenCalledWith(defaultAccountImportSettings())
    expect(wrapper.emitted('saved')).toEqual([[]])
    expect(wrapper.emitted('close')).toEqual([[]])
    wrapper.unmount()
  })

  it('saves independent switches, a fixed account proxy, manual region and fixed ticket proxy', async () => {
    const wrapper = mountModal()
    await flushPromises()
    await enable(wrapper)
    await wrapper.get('[aria-labelledby="import-default-protection"]').trigger('click')
    await wrapper.get('[aria-labelledby="import-default-ticket"]').trigger('click')
    await wrapper.get('[aria-labelledby="import-default-excel-bps"]').trigger('click')
    await wrapper.get('[data-testid="import-settings-proxy-mode"]').setValue('fixed')
    expect(wrapper.get('[data-testid="import-settings-save"]').attributes('disabled')).toBeDefined()
    wrapper.getComponent(ProxySelector).vm.$emit('update:modelValue', 7)
    await wrapper.get('[data-testid="import-settings-region-configured"]').setValue(true)
    await wrapper.get('[data-testid="proxy-region-mode"]').setValue('manual')
    await wrapper.get('[data-testid="proxy-region-country"]').setValue('JP')
    await wrapper.get('[data-testid="import-settings-ticket-configured"]').setValue(true)
    await wrapper.get('[data-testid="codex-ticket-proxy-fixed"]').setValue(true)
    wrapper.findAllComponents(ProxySelector)[1]!.vm.$emit('update:modelValue', 7)
    await flushPromises()
    await submit(wrapper)
    await flushPromises()
    expect(saveSettings).toHaveBeenCalledWith({
      enabled: true, protection_enabled: false, codex_ticket_enabled: false, excel_bps_enabled: true, proxy_mode: 'fixed', proxy_id: 7,
      extra: { proxy_region_mode: 'manual', proxy_region_country: 'JP', codex_ticket_proxy_mode: 'fixed', codex_ticket_proxy_id: 7, codex_ticket_proxy_strategy: 'affinity' }
    })
    wrapper.unmount()
  })

  it('saves a default country for billing region fallback', async () => {
    const wrapper = mountModal()
    await flushPromises()
    await enable(wrapper)
    await wrapper.get('[data-testid="import-settings-region-configured"]').setValue(true)
    await wrapper.get('[data-testid="proxy-region-mode"]').setValue('billing')
    await wrapper.get('[data-testid="import-settings-region-fallback-country"]').setValue('JP')
    await flushPromises()
    await submit(wrapper)
    await flushPromises()
    expect(saveSettings).toHaveBeenCalledWith({ ...defaultAccountImportSettings(), enabled: true, extra: {
      proxy_region_mode: 'billing', proxy_region_country: '', proxy_region_fallback_country: 'JP'
    } })
    wrapper.unmount()
  })

  it('writes a complete random default while retaining omitted region semantics', async () => {
    const wrapper = mountModal()
    await flushPromises()
    await enable(wrapper)
    await wrapper.get('[data-testid="import-settings-proxy-mode"]').setValue('random')
    await submit(wrapper)
    await flushPromises()
    expect(saveSettings).toHaveBeenCalledWith({ ...defaultAccountImportSettings(), enabled: true, proxy_mode: 'random', extra: {
      random_proxy_pool_scope: 'all', random_proxy_pool_ids: [], random_proxy_group_id: null,
      random_proxy_empty_pool_policy: 'reject', random_proxy_max_reuse_minutes: 0, random_proxy_region_fallback: 'pool'
    } })
    wrapper.unmount()
  })

  it('retains the legacy reuse value, validates invalid input and saves emitted updates', async () => {
    getSettings.mockResolvedValue({ ...defaultAccountImportSettings(), enabled: true, proxy_mode: 'random', extra: { random_proxy_max_reuse_minutes: 15 } })
    const wrapper = mountModal()
    await flushPromises()
    const random = wrapper.getComponent(RandomProxySettings)
    expect(random.props('maxReuseMinutes')).toBe(15)
    random.vm.$emit('update:maxReuseMinutes', -1)
    await flushPromises()
    await submit(wrapper)
    expect(saveSettings).not.toHaveBeenCalled()
    random.vm.$emit('update:maxReuseMinutes', 30)
    await flushPromises()
    await submit(wrapper)
    await flushPromises()
    expect(saveSettings.mock.calls[0]?.[0].extra.random_proxy_max_reuse_minutes).toBe(30)
    wrapper.unmount()
  })

  it.each(['settings', 'proxies'])('prevents saving after %s load failure and allows retry', async failing => {
    const request = failing === 'settings' ? getSettings : getProxies
    request.mockRejectedValueOnce(new Error('unavailable'))
    const wrapper = mountModal()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain(failing === 'settings' ? 'loadFailed' : 'proxiesLoadFailed')
    expect(wrapper.get('[data-testid="import-settings-save"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('form').exists()).toBe(false)
    await wrapper.get('[data-testid="import-settings-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('form').exists()).toBe(true)
    await submit(wrapper)
    await flushPromises()
    expect(saveSettings).toHaveBeenCalledOnce()
    wrapper.unmount()
  })

  it('blocks unavailable fixed proxies and empty explicitly selected pools', async () => {
    getSettings.mockResolvedValue({ ...defaultAccountImportSettings(), enabled: true, proxy_mode: 'fixed', proxy_id: 8 })
    const wrapper = mountModal()
    await flushPromises()
    await submit(wrapper)
    expect(saveSettings).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('admin.accountImportSettings.fixedProxyRequired')
    await wrapper.get('[data-testid="import-settings-proxy-mode"]').setValue('random')
    await wrapper.get('[data-testid="random-proxy-scope"]').setValue('selected')
    await submit(wrapper)
    expect(saveSettings).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('admin.accounts.randomProxyPoolRequired')
    wrapper.unmount()
  })

  it('blocks group saving during loading and after failure, then recovers through group retry', async () => {
    getSettings.mockResolvedValue({ ...defaultAccountImportSettings(), enabled: true, proxy_mode: 'random', extra: { random_proxy_pool_scope: 'group', random_proxy_group_id: 3 } })
    let rejectGroups!: (reason: Error) => void
    listGroups.mockReturnValueOnce(new Promise((_, reject) => { rejectGroups = reject }))
    const wrapper = mountModal()
    await flushPromises()
    await submit(wrapper)
    expect(saveSettings).not.toHaveBeenCalled()
    rejectGroups(new Error('failed'))
    await flushPromises()
    await submit(wrapper)
    expect(saveSettings).not.toHaveBeenCalled()
    await wrapper.findAll('button').find(button => button.text() === 'accountProxyGroups.retry')!.trigger('click')
    await flushPromises()
    await submit(wrapper)
    await flushPromises()
    expect(saveSettings.mock.calls[0]?.[0].extra).toMatchObject({ random_proxy_pool_scope: 'group', random_proxy_group_id: 3 })
    wrapper.unmount()
  })

  it('removes only region and ticket defaults when restoring their unspecified choices', async () => {
    getSettings.mockResolvedValue({ ...defaultAccountImportSettings(), enabled: true, extra: {
      proxy_region_mode: 'manual', proxy_region_country: 'JP', proxy_region_fallback_country: 'PH', codex_ticket_proxy_mode: 'account', codex_ticket_proxy_id: 0, codex_ticket_proxy_strategy: 'affinity'
    } })
    const wrapper = mountModal()
    await flushPromises()
    await wrapper.get('[data-testid="import-settings-region-configured"]').setValue(false)
    await wrapper.get('[data-testid="import-settings-ticket-configured"]').setValue(false)
    await submit(wrapper)
    await flushPromises()
    expect(saveSettings).toHaveBeenCalledWith({ ...defaultAccountImportSettings(), enabled: true })
    wrapper.unmount()
  })

  it('locks duplicate saves and close while pending, retains changes after failure and retries', async () => {
    let rejectSave!: (reason: Error) => void
    saveSettings.mockReturnValueOnce(new Promise((_, reject) => { rejectSave = reject }))
    const wrapper = mountModal()
    await flushPromises()
    await enable(wrapper)
    await submit(wrapper)
    await submit(wrapper)
    wrapper.getComponent(BaseDialogStub).vm.$emit('close')
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(saveSettings).toHaveBeenCalledOnce()
    expect(wrapper.get('[data-testid="import-settings-fields"]').attributes('disabled')).toBeDefined()
    rejectSave(new Error('Save unavailable'))
    await flushPromises()
    expect(wrapper.text()).toContain('Save unavailable')
    expect(wrapper.get('[data-testid="import-settings-enabled"]').attributes('aria-checked')).toBe('true')
    await submit(wrapper)
    await flushPromises()
    expect(saveSettings).toHaveBeenCalledTimes(2)
    expect(wrapper.emitted('saved')).toEqual([[]])
    wrapper.unmount()
  })
})
