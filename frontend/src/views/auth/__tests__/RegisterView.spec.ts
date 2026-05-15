import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import RegisterView from '@/views/auth/RegisterView.vue'

const routeState = vi.hoisted(() => ({
  query: {
    aff_code: 'GK2GH3XZ634Z',
  } as Record<string, string>,
}))

const {
  pushMock,
  showErrorMock,
  getPublicSettingsMock,
  validateAffiliateCodeMock,
} = vi.hoisted(() => ({
  pushMock: vi.fn(),
  showErrorMock: vi.fn(),
  getPublicSettingsMock: vi.fn(),
  validateAffiliateCodeMock: vi.fn(),
}))

vi.mock('vue-router', () => ({
  useRoute: () => routeState,
  useRouter: () => ({
    push: pushMock,
  }),
}))

vi.mock('vue-i18n', () => ({
  createI18n: () => ({
    global: {
      t: (key: string) => key,
    },
  }),
  useI18n: () => ({
    t: (key: string) => key,
    locale: { value: 'zh-CN' },
  }),
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => ({
    register: vi.fn(),
  }),
  useAppStore: () => ({
    showError: (...args: any[]) => showErrorMock(...args),
    showWarning: vi.fn(),
    showSuccess: vi.fn(),
  }),
}))

vi.mock('@/api/auth', async () => {
  const actual = await vi.importActual<typeof import('@/api/auth')>('@/api/auth')
  return {
    ...actual,
    getPublicSettings: (...args: any[]) => getPublicSettingsMock(...args),
    validateAffiliateCode: (...args: any[]) => validateAffiliateCodeMock(...args),
  }
})

describe('RegisterView', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    routeState.query = { aff_code: 'GK2GH3XZ634Z' }
    pushMock.mockReset()
    showErrorMock.mockReset()
    getPublicSettingsMock.mockReset()
    validateAffiliateCodeMock.mockReset()
    localStorage.clear()
    sessionStorage.clear()

    getPublicSettingsMock.mockResolvedValue({
      registration_enabled: true,
      email_verify_enabled: false,
      promo_code_enabled: false,
      turnstile_enabled: false,
      turnstile_site_key: '',
      site_name: 'Sub2API',
      linuxdo_oauth_enabled: false,
      wechat_oauth_enabled: false,
      wechat_oauth_mode: '',
      oidc_oauth_enabled: false,
      oidc_oauth_provider_name: 'OIDC',
      github_oauth_enabled: false,
      google_oauth_enabled: false,
      registration_email_suffix_whitelist: [],
      login_agreement_enabled: false,
      login_agreement_documents: [],
    })
    validateAffiliateCodeMock.mockResolvedValue({
      valid: true,
    })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows affiliate code input and hydrates it from route query', async () => {
    const wrapper = mount(RegisterView, {
      global: {
        stubs: {
          AuthLayout: { template: '<div><slot /><slot name="footer" /></div>' },
          Icon: true,
          TurnstileWidget: true,
          LoginAgreementPrompt: true,
          EmailOAuthButtons: true,
          LinuxDoOAuthSection: true,
          WechatOAuthSection: true,
          OidcOAuthSection: true,
          RouterLink: { template: '<a><slot /></a>' },
          transition: false,
        },
      },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(600)
    await flushPromises()

    const affiliateInput = wrapper.get('#aff_code')
    expect((affiliateInput.element as HTMLInputElement).value).toBe('GK2GH3XZ634Z')
    expect(validateAffiliateCodeMock).toHaveBeenCalledWith('GK2GH3XZ634Z')
  })
})
