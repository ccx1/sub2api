import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ProxyQualityView from '../ProxyQualityView.vue'
import type { ProxyQualityGuardItem, ProxyQualityGuardSettings } from '@/types'

const api = vi.hoisted(() => ({
  overview: vi.fn(), events: vi.fn(), updateSettings: vi.fn(), run: vi.fn(), reset: vi.fn()
}))
const notifications = vi.hoisted(() => ({ showSuccess: vi.fn(), showError: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { proxyQuality: api } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => notifications }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))

const settings: ProxyQualityGuardSettings = {
  enabled: true, interval_seconds: 75, check_interval_minutes: 15, check_mode: 'full',
  min_score: 73, fail_on_challenge: true, failure_threshold: 2, runtime_failure_threshold: 5,
  disable_minutes: 20, max_rounds: 4, auto_delete: false, pre_use_check: 'strict',
  include_fixed_bound: true, stable_reset_hours: 36, max_checks_per_run: 30, concurrency: 3
}
function proxy(id: number, patch: Partial<ProxyQualityGuardItem> = {}): ProxyQualityGuardItem {
  return {
    proxy_id: id, name: `Proxy ${id}`, protocol: 'http', host: `proxy${id}.example`, port: 8080,
    proxy_status: 'active', state: 'active', rounds: 0, consecutive_failures: 0,
    last_error: '', quality_grade: 'A', quality_status: 'success', fixed_bound_count: 0,
    dynamic_bound_count: 1, pre_use_ready: true, managed: true, ...patch
  }
}
function button(wrapper: VueWrapper, key: string) {
  const match = wrapper.findAll('button').find((item) => item.text() === key)
  if (!match) throw new Error(`Missing button: ${key}`)
  return match
}
async function render() {
  const wrapper = mount(ProxyQualityView, { global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' },
    BaseDialog: { props: ['show'], template: '<section v-if="show" data-testid="settings"><slot /><slot name="footer" /></section>' },
    ConfirmDialog: { props: ['show'], emits: ['confirm', 'cancel'], template: `<section v-if="show"><button @click="$emit('confirm')">confirm-reset</button><button @click="$emit('cancel')">cancel-reset</button></section>` },
    Icon: true, Select: true, Toggle: true
  } } })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks()
  api.overview.mockResolvedValue({
    settings: { ...settings }, summary: { total: 3, healthy: 1, pending: 0, failing: 1, disabled: 1 },
    items: [proxy(1), proxy(2, { state: 'disabled', rounds: 1, consecutive_failures: 1 }),
      proxy(3, { consecutive_failures: 1, group_name: 'Tokyo' })], last_run: null
  })
  api.events.mockResolvedValue({ items: [], count: 0 })
  api.updateSettings.mockImplementation(async (value) => value)
  api.reset.mockResolvedValue({ message: 'ok' })
  api.run.mockResolvedValue({ checked: 3, failed: 0, disabled: 0, restored: 0, deleted: 0 })
})

describe('ProxyQualityView integration', () => {
  it('keeps disabled and failing filters distinct and combines them with case-insensitive search', async () => {
    const wrapper = await render()
    expect(wrapper.findAll('tbody tr')).toHaveLength(3)
    const cards = wrapper.findAll('button').filter((item) => item.text().includes('admin.proxyQuality.summary.'))
    await cards.find((item) => item.text().includes('.disabled'))!.trigger('click')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.get('tbody').text()).toContain('Proxy 2')
    await cards.find((item) => item.text().includes('.failing'))!.trigger('click')
    expect(wrapper.get('tbody').text()).toContain('Proxy 3')
    await wrapper.get('input[type="search"]').setValue('TOKYO')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    await wrapper.get('input[type="search"]').setValue('proxy2.example')
    expect(wrapper.find('tbody').exists()).toBe(false)
    wrapper.unmount()
  })

  it('resets only the confirmed proxy and reloads the overview', async () => {
    const wrapper = await render()
    await button(wrapper, 'admin.proxyQuality.reset').trigger('click')
    expect(api.reset).not.toHaveBeenCalled()
    await button(wrapper, 'cancel-reset').trigger('click')
    expect(api.reset).not.toHaveBeenCalled()
    await button(wrapper, 'admin.proxyQuality.reset').trigger('click')
    await button(wrapper, 'confirm-reset').trigger('click')
    await flushPromises()
    expect(api.reset).toHaveBeenCalledOnce()
    expect(api.reset).toHaveBeenCalledWith(2)
    expect(api.overview).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('discards cancelled settings and preserves unrelated policy values when saving', async () => {
    const wrapper = await render()
    await button(wrapper, 'admin.proxyQuality.settings').trigger('click')
    await wrapper.get('[data-testid="settings"] input[type="number"]').setValue(61)
    await button(wrapper, 'common.cancel').trigger('click')
    expect(api.updateSettings).not.toHaveBeenCalled()
    await button(wrapper, 'admin.proxyQuality.settings').trigger('click')
    expect(wrapper.get<HTMLInputElement>('[data-testid="settings"] input[type="number"]').element.value).toBe('73')
    await wrapper.get('[data-testid="settings"] input[type="number"]').setValue(67)
    await button(wrapper, 'common.save').trigger('click')
    await flushPromises()
    expect(api.updateSettings).toHaveBeenCalledOnce()
    expect(api.updateSettings).toHaveBeenCalledWith({ ...settings, min_score: 67 })
    expect(wrapper.find('[data-testid="settings"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('clears the running state after failure and allows a later successful check', async () => {
    api.run.mockRejectedValueOnce(new Error('offline'))
    const wrapper = await render()
    await button(wrapper, 'admin.proxyQuality.runNow').trigger('click')
    await flushPromises()
    expect(notifications.showError).toHaveBeenCalledOnce()
    expect(button(wrapper, 'admin.proxyQuality.runNow').attributes('disabled')).toBeUndefined()
    expect(wrapper.findAll('tbody tr')).toHaveLength(3)
    await button(wrapper, 'admin.proxyQuality.runNow').trigger('click')
    await flushPromises()
    expect(api.run).toHaveBeenCalledTimes(2)
    expect(api.overview).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })
})
