import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import SharedAutoTransferSettings from '../SharedAutoTransferSettings.vue'
import type { SharedAutoTransferSettings as Settings } from '@/api/sharedPool'

const { autoTransferSettings, saveAutoTransferSettings } = vi.hoisted(() => ({ autoTransferSettings: vi.fn(), saveAutoTransferSettings: vi.fn() }))
vi.mock('@/api/sharedPool', () => ({ sharedPoolAPI: { autoTransferSettings, saveAutoTransferSettings } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, values?: { timezone: string }) => values ? `${key}: ${values.timezone}` : key }) }))

const defaults: Settings = { enabled: false, threshold: 1, daily_time: '00:00', timezone: 'Asia/Shanghai' }
let wrapper: VueWrapper
async function render() {
  wrapper = mount(SharedAutoTransferSettings)
  await flushPromises()
  return wrapper
}
beforeEach(() => {
  vi.resetAllMocks()
  autoTransferSettings.mockResolvedValue({ ...defaults })
  saveAutoTransferSettings.mockImplementation(async input => ({ ...defaults, ...input }))
})
afterEach(() => wrapper?.unmount())

describe('shared pool automatic balance transfer settings', () => {
  it('loads independently before exposing the saved state and editable defaults', async () => {
    let resolve!: (value: Settings) => void
    autoTransferSettings.mockImplementationOnce(() => new Promise<Settings>(done => { resolve = done }))
    await render()
    expect(wrapper.get('[role="status"]').text()).toBe('common.loading')
    expect(wrapper.find('form').exists()).toBe(false)
    expect(wrapper.find('[data-test="saved-status"]').exists()).toBe(false)
    resolve({ ...defaults }); await flushPromises()
    expect(wrapper.get('[data-test="saved-status"]').text()).toBe('sharedPool.autoTransferDisabled')
    expect(wrapper.get<HTMLInputElement>('#shared-auto-transfer-threshold').element.value).toBe('1')
    expect(wrapper.get<HTMLInputElement>('#shared-auto-transfer-time').element.value).toBe('00:00')
    expect(wrapper.get('#shared-auto-transfer-timezone').text()).toContain('Asia/Shanghai')
    expect(wrapper.find('select').exists()).toBe(false)
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
  })

  it('keeps edits local until saved and reflects the returned server state', async () => {
    await render()
    await wrapper.get('[role="switch"]').trigger('click')
    await wrapper.get('#shared-auto-transfer-threshold').setValue('12.12345678')
    await wrapper.get('#shared-auto-transfer-time').setValue('23:59')
    expect(saveAutoTransferSettings).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="saved-status"]').text()).toBe('sharedPool.autoTransferDisabled')
    expect(wrapper.get('#shared-auto-transfer-status').text()).toBe('sharedPool.autoTransferUnsaved')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(saveAutoTransferSettings).toHaveBeenCalledWith({ enabled: true, threshold: 12.12345678, daily_time: '23:59' })
    expect(wrapper.get('[data-test="saved-status"]').text()).toBe('sharedPool.autoTransferEnabled')
    expect(wrapper.get('#shared-auto-transfer-status').text()).toBe('sharedPool.saved')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
  })

  it('saves an explicit disable without losing the configured threshold and time', async () => {
    autoTransferSettings.mockResolvedValue({ ...defaults, enabled: true, threshold: 5, daily_time: '09:30' })
    await render()
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.get('[data-test="saved-status"]').text()).toBe('sharedPool.autoTransferEnabled')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(saveAutoTransferSettings).toHaveBeenCalledWith({ enabled: false, threshold: 5, daily_time: '09:30' })
    expect(wrapper.get('[data-test="saved-status"]').text()).toBe('sharedPool.autoTransferDisabled')
  })

  it('locks controls during saving and prevents duplicate requests', async () => {
    let resolve!: (value: Settings) => void
    saveAutoTransferSettings.mockImplementationOnce(() => new Promise<Settings>(done => { resolve = done }))
    await render()
    await wrapper.get('[role="switch"]').trigger('click')
    await wrapper.get('form').trigger('submit')
    await wrapper.get('form').trigger('submit')
    expect(saveAutoTransferSettings).toHaveBeenCalledTimes(1)
    expect(wrapper.get('button[type="submit"]').text()).toBe('common.saving')
    for (const control of wrapper.findAll('form button, form input')) expect(control.attributes('disabled')).toBeDefined()
    resolve({ ...defaults, enabled: true }); await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeUndefined()
  })

  it('offers a local retry when loading fails', async () => {
    autoTransferSettings.mockRejectedValueOnce(new Error('offline'))
    await render()
    expect(wrapper.get('[role="alert"]').text()).toBe('sharedPool.autoTransferLoadFailed')
    expect(wrapper.find('form').exists()).toBe(false)
    await wrapper.get('button').trigger('click'); await flushPromises()
    expect(autoTransferSettings).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.find('form').exists()).toBe(true)
  })

  it('preserves edits and the last saved state after a save failure, then retries', async () => {
    saveAutoTransferSettings.mockRejectedValueOnce(new Error('offline'))
    await render()
    await wrapper.get('[role="switch"]').trigger('click')
    await wrapper.get('#shared-auto-transfer-threshold').setValue('2')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('sharedPool.autoTransferSaveFailed')
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[data-test="saved-status"]').text()).toBe('sharedPool.autoTransferDisabled')
    expect(wrapper.get<HTMLInputElement>('#shared-auto-transfer-threshold').element.value).toBe('2')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(saveAutoTransferSettings).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="saved-status"]').text()).toBe('sharedPool.autoTransferEnabled')
  })

  it.each(['', '0', '-1', 'NaN', 'Infinity', '1e-8', '0.000000001', '1.123456789', '1000000001', '1000000000.00000001'])('rejects invalid threshold %j without sending an update', async value => {
    await render()
    await wrapper.get('#shared-auto-transfer-threshold').setValue(value)
    expect(wrapper.get('#shared-auto-transfer-threshold').attributes('aria-invalid')).toBe('true')
    expect(wrapper.get('[role="alert"]').text()).toBe('sharedPool.autoTransferInvalidThreshold')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(saveAutoTransferSettings).not.toHaveBeenCalled()
  })

  it.each(['0.00000001', '1000000000', '1000000000.00000000'])('accepts threshold boundary %s and preserves the saved precision', async value => {
    await render()
    await wrapper.get('#shared-auto-transfer-threshold').setValue(value)
    expect(wrapper.get('#shared-auto-transfer-threshold').attributes('aria-invalid')).toBe('false')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(saveAutoTransferSettings).toHaveBeenCalledWith({ enabled: false, threshold: Number(value), daily_time: '00:00' })
    expect(wrapper.get<HTMLInputElement>('#shared-auto-transfer-threshold').element.value).toBe(value === '1000000000.00000000' ? '1000000000' : value)
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
  })

  it('rejects an empty daily time and associates the validation message with its input', async () => {
    await render()
    await wrapper.get('#shared-auto-transfer-time').setValue('')
    expect(wrapper.get('#shared-auto-transfer-time').attributes('aria-invalid')).toBe('true')
    expect(wrapper.get('#shared-auto-transfer-time').attributes('aria-describedby')).toContain('shared-auto-transfer-time-error')
    expect(wrapper.get('#shared-auto-transfer-time-error').text()).toBe('sharedPool.autoTransferInvalidTime')
    await wrapper.get('form').trigger('submit')
    expect(saveAutoTransferSettings).not.toHaveBeenCalled()
  })
})
