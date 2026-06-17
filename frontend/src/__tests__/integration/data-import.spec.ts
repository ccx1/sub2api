import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ImportDataModal from '@/components/admin/account/ImportDataModal.vue'

const showError = vi.fn()
const showSuccess = vi.fn()

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess
  })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      importData: vi.fn(),
      batchCreate: vi.fn()
    }
  }
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

describe('ImportDataModal', () => {
  beforeEach(() => {
    showError.mockReset()
    showSuccess.mockReset()
    vi.clearAllMocks()
  })

  it('未选择文件时提示错误', async () => {
    const wrapper = mount(ImportDataModal, {
      props: { show: true },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }
        }
      }
    })

    await wrapper.find('form').trigger('submit')
    expect(showError).toHaveBeenCalledWith('admin.accounts.dataImportSelectFile')
  })

  it('无效 JSON 时提示解析失败', async () => {
    const wrapper = mount(ImportDataModal, {
      props: { show: true },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }
        }
      }
    })

    const input = wrapper.find('input[type="file"]')
    const file = new File(['invalid json'], 'data.json', { type: 'application/json' })
    Object.defineProperty(file, 'text', {
      value: () => Promise.resolve('invalid json')
    })
    Object.defineProperty(input.element, 'files', {
      value: [file]
    })

    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await Promise.resolve()

    expect(showError).toHaveBeenCalledWith('admin.accounts.dataImportParseFailed')
  })

  it('支持直接粘贴账号 JSON 数组并批量创建', async () => {
    const { adminAPI } = await import('@/api/admin')
    vi.mocked(adminAPI.accounts.batchCreate).mockResolvedValue({
      success: 2,
      failed: 0,
      results: [
        { success: true },
        { success: true }
      ]
    })

    const wrapper = mount(ImportDataModal, {
      props: { show: true },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }
        }
      }
    })

    const accounts = [
      {
        name: 'acc-1',
        platform: 'openai',
        type: 'apikey',
        credentials: { api_key: 'sk-1' }
      },
      {
        name: 'acc-2',
        platform: 'openai',
        type: 'apikey',
        credentials: { api_key: 'sk-2' }
      }
    ]

    await wrapper.find('textarea').setValue(JSON.stringify(accounts))
    await wrapper.find('form').trigger('submit')
    await Promise.resolve()

    expect(adminAPI.accounts.batchCreate).toHaveBeenCalledWith(accounts)
    expect(adminAPI.accounts.importData).not.toHaveBeenCalled()
    expect(showSuccess).toHaveBeenCalledWith('admin.accounts.dataImportSuccess')
  })

  it('完整导出 JSON 仍走数据导入接口', async () => {
    const { adminAPI } = await import('@/api/admin')
    vi.mocked(adminAPI.accounts.importData).mockResolvedValue({
      proxy_created: 0,
      proxy_reused: 0,
      proxy_failed: 0,
      account_created: 1,
      account_failed: 0
    })

    const wrapper = mount(ImportDataModal, {
      props: { show: true },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }
        }
      }
    })

    const payload = {
      exported_at: '2026-05-25T00:00:00Z',
      proxies: [],
      accounts: [
        {
          name: 'acc-1',
          platform: 'openai',
          type: 'apikey',
          credentials: { api_key: 'sk-1' },
          concurrency: 1,
          priority: 50
        }
      ]
    }

    await wrapper.find('textarea').setValue(JSON.stringify(payload))
    await wrapper.find('form').trigger('submit')
    await Promise.resolve()

    expect(adminAPI.accounts.importData).toHaveBeenCalledWith({
      data: payload,
      skip_default_group_bind: true
    })
    expect(adminAPI.accounts.batchCreate).not.toHaveBeenCalled()
  })
})
