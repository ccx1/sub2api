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
  wrapper = mount(SharedAutoTransferSettings, { global: { stubs: {
    BaseDialog: { name: 'BaseDialog', props: ['show', 'width', 'closeOnEscape', 'showCloseButton'], template: '<div v-if="show" role="dialog"><slot /><slot name="footer" /></div>' }
  } } })
  await flushPromises()
  return wrapper
}
async function openSettings() {
  await wrapper.get('[data-test="auto-transfer-settings"]').trigger('click')
  await flushPromises()
}
function cancelButton() { return wrapper.findAll('button').find(button => button.text() === 'common.cancel')! }
beforeEach(() => {
  vi.resetAllMocks()
  autoTransferSettings.mockResolvedValue({ ...defaults })
  saveAutoTransferSettings.mockImplementation(async input => ({ ...defaults, ...input }))
})
afterEach(() => wrapper?.unmount())

describe('shared pool automatic balance transfer settings', () => {
  it('shows a compact saved status and opens editable defaults only from the settings button', async () => {
    await render()
    const button = wrapper.get('[data-test="auto-transfer-settings"]')
    expect(button.attributes('aria-haspopup')).toBe('dialog')
    expect(button.attributes('aria-expanded')).toBe('false')
    expect(wrapper.get('[data-test="saved-status"]').text()).toBe('sharedPool.autoTransferDisabled')
    expect(wrapper.find('form').exists()).toBe(false)
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('sharedPool.autoTransferHint')
    await openSettings()
    expect(button.attributes('aria-expanded')).toBe('true')
    expect(wrapper.getComponent({ name: 'BaseDialog' }).props('width')).toBe('normal')
    expect(wrapper.get('form').attributes('id')).toBe('shared-auto-transfer-form')
    expect(wrapper.get('button[type="submit"]').attributes('form')).toBe('shared-auto-transfer-form')
    expect(wrapper.get<HTMLInputElement>('#shared-auto-transfer-threshold').element.value).toBe('1')
    expect(wrapper.get<HTMLInputElement>('#shared-auto-transfer-time').element.value).toBe('00:00')
    expect(wrapper.get('#shared-auto-transfer-timezone').text()).toContain('Asia/Shanghai')
    expect(wrapper.find('select').exists()).toBe(false)
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
  })

  it('loads independently without exposing editable settings before the response arrives', async () => {
    let resolve!: (value: Settings) => void
    autoTransferSettings.mockImplementationOnce(() => new Promise<Settings>(done => { resolve = done }))
    await render()
    expect(wrapper.find('form').exists()).toBe(false)
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    await openSettings()
    expect(wrapper.get('[role="dialog"]').text()).toContain('common.loading')
    expect(wrapper.find('form').exists()).toBe(false)
    resolve({ ...defaults }); await flushPromises()
    expect(wrapper.get('[data-test="saved-status"]').text()).toBe('sharedPool.autoTransferDisabled')
    expect(wrapper.get<HTMLInputElement>('#shared-auto-transfer-threshold').element.value).toBe('1')
  })

  it('keeps edits local until saved and reflects the returned server state', async () => {
    saveAutoTransferSettings.mockResolvedValueOnce({ ...defaults, enabled: true, threshold: 15, daily_time: '13:40', timezone: 'UTC' })
    await render()
    await openSettings()
    await wrapper.get('[role="switch"]').trigger('click')
    await wrapper.get('#shared-auto-transfer-threshold').setValue('12.12345678')
    await wrapper.get('#shared-auto-transfer-time').setValue('23:59')
    expect(saveAutoTransferSettings).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="saved-status"]').text()).toBe('sharedPool.autoTransferDisabled')
    expect(wrapper.get('#shared-auto-transfer-status').text()).toBe('sharedPool.autoTransferUnsaved')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(saveAutoTransferSettings).toHaveBeenCalledWith({ enabled: true, threshold: 12.12345678, daily_time: '23:59' })
    expect(wrapper.get('[data-test="saved-status"]').text()).toBe('sharedPool.autoTransferEnabled')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="auto-transfer-settings"]').attributes('aria-expanded')).toBe('false')
    await openSettings()
    expect(wrapper.get<HTMLInputElement>('#shared-auto-transfer-threshold').element.value).toBe('15')
    expect(wrapper.get<HTMLInputElement>('#shared-auto-transfer-time').element.value).toBe('13:40')
    expect(wrapper.get('#shared-auto-transfer-timezone').text()).toContain('UTC')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
  })

  it.each(['cancel', 'dialog close'])('discards the draft after %s and reopens the saved values', async action => {
    await render()
    await openSettings()
    await wrapper.get('[role="switch"]').trigger('click')
    await wrapper.get('#shared-auto-transfer-threshold').setValue('9')
    await wrapper.get('#shared-auto-transfer-time').setValue('12:30')
    if (action === 'cancel') await cancelButton().trigger('click')
    else wrapper.getComponent({ name: 'BaseDialog' }).vm.$emit('close')
    await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(saveAutoTransferSettings).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="saved-status"]').text()).toBe('sharedPool.autoTransferDisabled')
    await openSettings()
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.get<HTMLInputElement>('#shared-auto-transfer-threshold').element.value).toBe('1')
    expect(wrapper.get<HTMLInputElement>('#shared-auto-transfer-time').element.value).toBe('00:00')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
  })

  it('saves an explicit disable without losing the configured threshold and time', async () => {
    autoTransferSettings.mockResolvedValue({ ...defaults, enabled: true, threshold: 5, daily_time: '09:30' })
    await render()
    await openSettings()
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.get('[data-test="saved-status"]').text()).toBe('sharedPool.autoTransferEnabled')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(saveAutoTransferSettings).toHaveBeenCalledWith({ enabled: false, threshold: 5, daily_time: '09:30' })
    expect(wrapper.get('[data-test="saved-status"]').text()).toBe('sharedPool.autoTransferDisabled')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })

  it('locks controls during saving and prevents duplicate requests', async () => {
    let resolve!: (value: Settings) => void
    saveAutoTransferSettings.mockImplementationOnce(() => new Promise<Settings>(done => { resolve = done }))
    await render()
    await openSettings()
    await wrapper.get('[role="switch"]').trigger('click')
    await wrapper.get('form').trigger('submit')
    await wrapper.get('form').trigger('submit')
    expect(saveAutoTransferSettings).toHaveBeenCalledTimes(1)
    expect(wrapper.get('button[type="submit"]').text()).toBe('common.saving')
    for (const control of wrapper.findAll('form button, form input')) expect(control.attributes('disabled')).toBeDefined()
    expect(cancelButton().attributes('disabled')).toBeDefined()
    const dialog = wrapper.getComponent({ name: 'BaseDialog' })
    expect(dialog.props('closeOnEscape')).toBe(false)
    expect(dialog.props('showCloseButton')).toBe(false)
    dialog.vm.$emit('close'); await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
    resolve({ ...defaults, enabled: true }); await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    await openSettings()
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeUndefined()
  })

  it('offers a local retry when loading fails', async () => {
    autoTransferSettings.mockRejectedValueOnce(new Error('offline'))
    await render()
    expect(wrapper.get('[role="status"]').text()).toBe('sharedPool.autoTransferStatusUnavailable')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    await openSettings()
    expect(wrapper.get('[role="alert"]').text()).toBe('sharedPool.autoTransferLoadFailed')
    expect(wrapper.find('form').exists()).toBe(false)
    await wrapper.findAll('button').find(button => button.text() === 'sharedPool.autoTransferRetry')!.trigger('click'); await flushPromises()
    expect(autoTransferSettings).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.find('form').exists()).toBe(true)
  })

  it('preserves edits and the last saved state after a save failure, then retries', async () => {
    saveAutoTransferSettings.mockRejectedValueOnce(new Error('offline'))
    await render()
    await openSettings()
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
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })

  it('clears a failed save and its draft when cancelled', async () => {
    saveAutoTransferSettings.mockRejectedValueOnce(new Error('offline'))
    await render()
    await openSettings()
    await wrapper.get('#shared-auto-transfer-threshold').setValue('2')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('sharedPool.autoTransferSaveFailed')
    await cancelButton().trigger('click')
    await openSettings()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.get<HTMLInputElement>('#shared-auto-transfer-threshold').element.value).toBe('1')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
  })

  it.each(['', '0', '-1', 'NaN', 'Infinity', '1e-8', '0.000000001', '1.123456789', '1000000001', '1000000000.00000001'])('rejects invalid threshold %j without sending an update', async value => {
    await render()
    await openSettings()
    await wrapper.get('#shared-auto-transfer-threshold').setValue(value)
    expect(wrapper.get('#shared-auto-transfer-threshold').attributes('aria-invalid')).toBe('true')
    expect(wrapper.get('[role="alert"]').text()).toBe('sharedPool.autoTransferInvalidThreshold')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(saveAutoTransferSettings).not.toHaveBeenCalled()
  })

  it.each(['0.00000001', '1000000000', '1000000000.00000000'])('accepts threshold boundary %s and preserves the saved precision', async value => {
    await render()
    await openSettings()
    await wrapper.get('#shared-auto-transfer-threshold').setValue(value)
    expect(wrapper.get('#shared-auto-transfer-threshold').attributes('aria-invalid')).toBe('false')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(saveAutoTransferSettings).toHaveBeenCalledWith({ enabled: false, threshold: Number(value), daily_time: '00:00' })
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    await openSettings()
    expect(wrapper.get<HTMLInputElement>('#shared-auto-transfer-threshold').element.value).toBe(value === '1000000000.00000000' ? '1000000000' : value)
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
  })

  it('rejects an empty daily time and associates the validation message with its input', async () => {
    await render()
    await openSettings()
    await wrapper.get('#shared-auto-transfer-time').setValue('')
    expect(wrapper.get('#shared-auto-transfer-time').attributes('aria-invalid')).toBe('true')
    expect(wrapper.get('#shared-auto-transfer-time').attributes('aria-describedby')).toContain('shared-auto-transfer-time-error')
    expect(wrapper.get('#shared-auto-transfer-time-error').text()).toBe('sharedPool.autoTransferInvalidTime')
    await wrapper.get('form').trigger('submit')
    expect(saveAutoTransferSettings).not.toHaveBeenCalled()
  })
})
