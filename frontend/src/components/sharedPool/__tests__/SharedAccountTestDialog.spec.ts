import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import SharedAccountTestDialog from '../SharedAccountTestDialog.vue'
import type { SharedAccount } from '@/api/sharedPool'

const { get, adminModels } = vi.hoisted(() => ({ get: vi.fn(), adminModels: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get }, buildApiUrl: (path: string) => `/api/v1${path}` }))
vi.mock('@/api/admin', () => ({ adminAPI: { accounts: { getAvailableModels: adminModels } } }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const models = [{ id: 'gpt-5.4', display_name: 'GPT-5.4' }, { id: 'gpt-image-1', display_name: 'GPT Image' }]
const account = { id: 12, name: 'Shared OpenAI', platform: 'openai', type: 'oauth', status: 'active' } as SharedAccount
function response(events: object[]) {
  return new Response(new ReadableStream({ start(controller) {
    controller.enqueue(new TextEncoder().encode(events.map(event => `data: ${JSON.stringify(event)}`).join('\n\n')))
    controller.close()
  } }), { status: 200 })
}
function render() {
  return mount(SharedAccountTestDialog, { props: { account }, global: { stubs: {
    BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }, Icon: true,
    Select: {
      props: ['modelValue', 'options', 'valueKey', 'labelKey', 'disabled'], emits: ['update:modelValue'],
      template: '<select :value="modelValue" :disabled="disabled" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="option in options" :key="option[valueKey || \'value\']" :value="option[valueKey || \'value\']">{{ option[labelKey || \'label\'] }}</option></select>'
    },
    TextArea: { props: ['modelValue'], emits: ['update:modelValue'], template: '<textarea :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />' }
  } } })
}
const startButton = (wrapper: ReturnType<typeof render>) => wrapper.findAll('button').find(button => button.text().includes('admin.accounts.startTest'))!

describe('shared account uses the account management connection test', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    get.mockResolvedValue({ data: models })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response([{ type: 'test_complete', success: true }])))
  })
  afterEach(() => vi.unstubAllGlobals())

  it('loads owner-scoped models on open and sends the selected Compact mode', async () => {
    const wrapper = render()
    await flushPromises()
    expect(get).toHaveBeenCalledWith('/shared-pool/accounts/12/models')
    expect(adminModels).not.toHaveBeenCalled()
    await wrapper.findAll('select')[1].setValue('compact')
    await startButton(wrapper).trigger('click'); await flushPromises()
    expect(fetch).toHaveBeenCalledWith('/api/v1/shared-pool/accounts/12/test', expect.objectContaining({
      method: 'POST', body: JSON.stringify({ model_id: 'gpt-5.4', prompt: '', mode: 'compact' })
    }))
    const headers = vi.mocked(fetch).mock.calls[0][1]?.headers as Record<string, string>
    expect(Object.keys(headers).some(key => key.toLowerCase().includes('admin'))).toBe(false)
    expect(wrapper.text()).toContain('admin.accounts.testCompleted')
    expect(wrapper.emitted('completed')).toHaveLength(1)
    wrapper.unmount()
  })

  it('supports the same image prompt and image result as account management', async () => {
    vi.mocked(fetch).mockResolvedValue(response([
      { type: 'image', image_url: 'data:image/png;base64,QUJD', mime_type: 'image/png' },
      { type: 'test_complete', success: true }
    ]))
    const wrapper = render(); await flushPromises()
    await wrapper.findAll('select')[0].setValue('gpt-image-1')
    await wrapper.get('textarea').setValue('Draw a blue sky')
    await startButton(wrapper).trigger('click'); await flushPromises()
    expect(JSON.parse(String(vi.mocked(fetch).mock.calls[0][1]?.body))).toEqual({ model_id: 'gpt-image-1', prompt: 'Draw a blue sky', mode: 'default' })
    expect(wrapper.get('img').attributes('src')).toBe('data:image/png;base64,QUJD')
    wrapper.unmount()
  })

  it('reports incomplete streams as errors and allows retry', async () => {
    vi.mocked(fetch).mockResolvedValue(response([{ type: 'test_start', model: 'gpt-5.4' }]))
    const wrapper = render(); await flushPromises()
    await startButton(wrapper).trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('Connection closed before the test completed')
    expect(wrapper.text()).not.toContain('admin.accounts.testCompleted')
    expect(wrapper.findAll('button').find(button => button.text().includes('admin.accounts.retry'))!.attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('aborts an in-flight request when the dialog is closed', async () => {
    let testSignal!: AbortSignal
    vi.mocked(fetch).mockImplementation((_url, init) => new Promise((_resolve, reject) => {
      testSignal = init!.signal as AbortSignal
      testSignal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
    }))
    const wrapper = render(); await flushPromises()
    await startButton(wrapper).trigger('click'); await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'common.close')!.trigger('click')
    await flushPromises()
    expect(testSignal.aborted).toBe(true)
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(wrapper.emitted('completed')).toBeUndefined()
    wrapper.unmount()
  })
})
