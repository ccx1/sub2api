import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'

import ImportDataModal from '../ImportDataModal.vue'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

const { tMock, batchCreate, importData, showError, showSuccess } = vi.hoisted(() => ({
  tMock: vi.fn(),
  batchCreate: vi.fn(),
  importData: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      batchCreate,
      importData
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
    showWarning: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: tMock
    })
  }
})

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: {
    show: { type: Boolean, default: false }
  },
  template: '<section v-if="show"><slot /><slot name="footer" /></section>'
})

const messages = { en, zh }
const successfulImport = {
  proxy_created: 0, proxy_reused: 0, proxy_failed: 0,
  account_created: 1, account_failed: 0, account_ids: [71]
}
const accountPayload = { name: 'imported', platform: 'openai', type: 'oauth', credentials: {} }
const dataPayload = { type: 'sub2api-data', version: 1, proxies: [], accounts: [accountPayload] }

function mountModal() {
  return mount(ImportDataModal, {
    props: { show: true },
    global: { stubs: { BaseDialog: BaseDialogStub } }
  })
}

function importAndEditButton(wrapper: ReturnType<typeof mountModal>) {
  return wrapper.findAll('button').find(button => button.text() === 'admin.accounts.dataImportAndEdit')!
}

function getLocaleMessage(locale: 'en' | 'zh', key: string): string {
  return key.split('.').reduce<unknown>((value, segment) => {
    if (!value || typeof value !== 'object') return undefined
    return (value as Record<string, unknown>)[segment]
  }, messages[locale]) as string
}

describe('admin account ImportDataModal', () => {
  beforeEach(() => {
    tMock.mockReset().mockImplementation((key: string) => key)
    batchCreate.mockReset()
    importData.mockReset().mockResolvedValue(successfulImport)
    showError.mockReset()
    showSuccess.mockReset()
  })

  it.each([
    JSON.stringify([accountPayload]),
    JSON.stringify({ accounts: [accountPayload] })
  ])('imports pasted accounts and passes only successful IDs to edit: %s', async text => {
    batchCreate.mockResolvedValue({
      success: 2, failed: 1,
      results: [
        { success: true, id: 71, name: 'first' },
        { success: false, id: 99, name: 'failed', error: 'invalid' },
        { success: true, id: 72, name: 'second' }
      ]
    })
    const wrapper = mountModal()
    await wrapper.get('textarea').setValue(text)
    await importAndEditButton(wrapper).trigger('click')
    await flushPromises()

    expect(batchCreate).toHaveBeenCalledWith([accountPayload], { protection_enabled: true, codex_ticket_enabled: true })
    expect(wrapper.emitted('imported-and-edit')).toEqual([[[71, 72]]])
    expect(wrapper.emitted('imported')).toBeUndefined()
    expect(showError).toHaveBeenCalledWith('admin.accounts.dataImportCompletedWithErrors')
    wrapper.unmount()
  })

  it('keeps ordinary pasted import on the existing close-and-refresh flow', async () => {
    const wrapper = mountModal()
    await wrapper.get('textarea').setValue(JSON.stringify(dataPayload))
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(importData).toHaveBeenCalledWith({
      data: dataPayload, skip_default_group_bind: true, protection_enabled: true, codex_ticket_enabled: true
    })
    expect(wrapper.emitted('imported')).toEqual([[]])
    expect(wrapper.emitted('imported-and-edit')).toBeUndefined()
    wrapper.unmount()
  })

  it('merges multiple files and edits the exact IDs returned by data import', async () => {
    const wrapper = mountModal()
    const input = wrapper.get('input[type="file"]')
    await wrapper.get('[aria-labelledby="import-ticket-label"]').trigger('click')
    const files = ['first.json', 'second.json'].map(name => new File([JSON.stringify(dataPayload)], name, { type: 'application/json' }))
    Object.defineProperty(input.element, 'files', { value: files })
    await input.trigger('change')
    importData.mockResolvedValue({ ...successfulImport, account_created: 2, account_ids: [71, 72] })
    await importAndEditButton(wrapper).trigger('click')
    await vi.waitFor(() => expect(importData).toHaveBeenCalledOnce())
    await flushPromises()

    expect(importData.mock.calls[0]?.[0].data.accounts).toEqual([accountPayload, accountPayload])
    expect(importData.mock.calls[0]?.[0]).toMatchObject({ protection_enabled: true, codex_ticket_enabled: false })
    expect(wrapper.emitted('imported-and-edit')).toEqual([[[71, 72]]])
    wrapper.unmount()
  })

  it('does not open an editor when no accounts were imported', async () => {
    importData.mockResolvedValue({ ...successfulImport, account_created: 0, account_failed: 1, account_ids: [] })
    const wrapper = mountModal()
    await wrapper.get('textarea').setValue(JSON.stringify(dataPayload))
    await importAndEditButton(wrapper).trigger('click')
    await flushPromises()

    expect(wrapper.emitted('imported-and-edit')).toBeUndefined()
    expect(wrapper.emitted('imported')).toBeUndefined()
    expect(wrapper.text()).toContain('admin.accounts.dataImportResult')
    wrapper.unmount()
  })

  it.each([undefined, [], [71, 71], [0]])('reports imported data without guessing missing account IDs: %j', async accountIds => {
    importData.mockResolvedValue({ ...successfulImport, account_created: 2, account_ids: accountIds })
    const wrapper = mountModal()
    await wrapper.get('textarea').setValue(JSON.stringify(dataPayload))
    await importAndEditButton(wrapper).trigger('click')
    await flushPromises()

    expect(wrapper.emitted('imported-and-edit')).toBeUndefined()
    expect(showError).toHaveBeenCalledWith('admin.accounts.dataImportEditUnavailable')
    await wrapper.findAll('button').find(button => button.text() === 'common.cancel')!.trigger('click')
    expect(wrapper.emitted('imported')).toEqual([[]])
    wrapper.unmount()
  })

  it('blocks duplicate submits while an import-and-edit request is pending', async () => {
    let resolveImport!: (value: typeof successfulImport) => void
    importData.mockReturnValue(new Promise(resolve => { resolveImport = resolve }))
    const wrapper = mountModal()
    await wrapper.get('textarea').setValue(JSON.stringify(dataPayload))
    await importAndEditButton(wrapper).trigger('click')
    await wrapper.get('form').trigger('submit')

    expect(importData).toHaveBeenCalledOnce()
    expect(wrapper.findAll('[role="switch"]').every(button => (button.element as HTMLButtonElement).disabled)).toBe(true)
    expect(wrapper.get('button[form="import-data-form"]').attributes('disabled')).toBeDefined()
    resolveImport(successfulImport)
    await flushPromises()
    expect(wrapper.emitted('imported-and-edit')).toEqual([[[71]]])
    wrapper.unmount()
  })

  it.each([
    [true, true], [true, false], [false, true], [false, false]
  ])('passes independent protection=%s and ticket=%s options for array and bundle imports', async (protection, ticket) => {
    batchCreate.mockResolvedValue({ success: 0, failed: 0, results: [] })
    const wrapper = mountModal()
    if (!protection) await wrapper.get('[aria-labelledby="import-protection-label"]').trigger('click')
    if (!ticket) await wrapper.get('[aria-labelledby="import-ticket-label"]').trigger('click')
    expect(wrapper.get('[aria-labelledby="import-protection-label"]').attributes('aria-checked')).toBe(String(protection))
    expect(wrapper.get('[aria-labelledby="import-ticket-label"]').attributes('aria-checked')).toBe(String(ticket))
    const options = { protection_enabled: protection, codex_ticket_enabled: ticket }

    await wrapper.get('textarea').setValue(JSON.stringify([accountPayload]))
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(batchCreate).toHaveBeenCalledWith([accountPayload], options)

    await wrapper.get('textarea').setValue(JSON.stringify(dataPayload))
    await importAndEditButton(wrapper).trigger('click')
    await flushPromises()
    expect(importData).toHaveBeenCalledWith({ data: dataPayload, skip_default_group_bind: true, ...options })
    wrapper.unmount()
  })

  it('defaults both independent switches to enabled on each modal opening', async () => {
    const wrapper = mountModal()
    await wrapper.get('[aria-labelledby="import-protection-label"]').trigger('click')
    await wrapper.get('[aria-labelledby="import-ticket-label"]').trigger('click')
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })

    expect(wrapper.findAll('[role="switch"]').map(button => button.attributes('aria-checked'))).toEqual(['true', 'true'])
    wrapper.unmount()
  })

  it.each(['zh', 'en'] as const)(
    'renders the JSON text placeholder without vue-i18n placeholder parsing errors for %s',
    (locale) => {
      const i18nMessage = getLocaleMessage(locale, 'admin.accounts.dataImportJsonPlaceholder')
      expect(i18nMessage).not.toMatch(/\{\s*"/)

      tMock.mockImplementation((key: string) => getLocaleMessage(locale, key) || key)

      const wrapper = mount(ImportDataModal, {
        props: { show: true },
        global: {
          stubs: {
            BaseDialog: BaseDialogStub
          }
        }
      })

      const placeholder = wrapper.get('textarea').attributes('placeholder')
      expect(placeholder).toContain('[{ "name": "..."')
      expect(placeholder).toContain('"credentials": {...}')
    }
  )
})
