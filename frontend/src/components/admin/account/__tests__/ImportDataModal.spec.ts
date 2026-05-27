import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent } from 'vue'

import ImportDataModal from '../ImportDataModal.vue'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

const { tMock } = vi.hoisted(() => ({
  tMock: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      batchCreate: vi.fn(),
      importData: vi.fn()
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn()
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

function getLocaleMessage(locale: 'en' | 'zh', key: string): string {
  return key.split('.').reduce<unknown>((value, segment) => {
    if (!value || typeof value !== 'object') return undefined
    return (value as Record<string, unknown>)[segment]
  }, messages[locale]) as string
}

describe('admin account ImportDataModal', () => {
  beforeEach(() => {
    tMock.mockReset()
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
