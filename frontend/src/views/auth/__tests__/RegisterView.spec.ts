import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import RegisterView from '@/views/auth/RegisterView.vue'

const routeState = vi.hoisted(() => ({
  query: {} as Record<string, string>,
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

const publicSettings = {
  registration_enabled: true,
  email_verify_enabled: false,
  promo_code_enabled: false,
  invitation_code_enabled: false,
  affiliate_enabled: true,
  turnstile_enabled: true,
  turnstile_site_key: 'site-key',
  site_name: 'Sub2API',
  registration_email_suffix_whitelist: [],
  linuxdo_oauth_enabled: false,
  wechat_oauth_enabled: false,
  wechat_oauth_mode: '',
  oidc_oauth_enabled: false,
  oidc_oauth_provider_name: 'OIDC',
  github_oauth_enabled: false,
  google_oauth_enabled: false,
  login_agreement_enabled: false,
  login_agreement_documents: [],
}

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

function mountRegister() {
  return mount(RegisterView, {
    global: {
      stubs: {
        AuthLayout: { template: '<div><slot /><slot name="footer" /></div>' },
        Icon: true,
        TurnstileWidget: { template: '<div data-testid="turnstile-widget" />' },
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
}

describe('RegisterView', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    routeState.query = {}
    pushMock.mockReset()
    showErrorMock.mockReset()
    getPublicSettingsMock.mockReset()
    validateAffiliateCodeMock.mockReset()
    localStorage.clear()
    sessionStorage.clear()

    getPublicSettingsMock.mockResolvedValue(publicSettings)
    validateAffiliateCodeMock.mockResolvedValue({
      valid: true,
    })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows affiliate code input and hydrates it from route query', async () => {
    routeState.query = { aff_code: 'GK2GH3XZ634Z' }

    const wrapper = mountRegister()

    await flushPromises()
    await vi.advanceTimersByTimeAsync(600)
    await flushPromises()

    const affiliateInput = wrapper.get('#affiliate_code')
    expect((affiliateInput.element as HTMLInputElement).value).toBe('GK2GH3XZ634Z')
    expect(validateAffiliateCodeMock).toHaveBeenCalledWith('GK2GH3XZ634Z')
  })

  it('keeps the optional affiliate invitation field before Turnstile', async () => {
    const wrapper = mountRegister()
    await flushPromises()

    const invitationField = wrapper.get('[data-testid="affiliate-invitation-field"]')
    const turnstile = wrapper.get('[data-testid="registration-turnstile"]')

    expect(invitationField.get('input').attributes('id')).toBe('affiliate_code')
    expect(invitationField.text()).toContain('common.optional')
    expect(
      invitationField.element.compareDocumentPosition(turnstile.element) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
  })

  it('uses the mandatory invitation field without duplicating the affiliate field', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings,
      invitation_code_enabled: true,
    })

    const wrapper = mountRegister()
    await flushPromises()

    expect(wrapper.find('[data-testid="affiliate-invitation-field"]').exists()).toBe(false)
    expect(wrapper.get('#invitation_code').exists()).toBe(true)
  })
})
