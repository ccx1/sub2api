import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({ update: vi.fn(), create: vi.fn(), session: vi.fn(), pat: vi.fn() }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn(), showInfo: vi.fn(), showWarning: vi.fn() }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isSimpleMode: true }) }))
vi.mock('@/api/admin', () => ({ adminAPI: {
  proxies: { listGroups: vi.fn().mockResolvedValue([]) },
  accounts: { update: mocks.update, create: mocks.create, importCodexSession: mocks.session, createOpenAICodexPAT: mocks.pat, checkMixedChannelRisk: vi.fn().mockResolvedValue({ has_risk: false }) },
  settings: { getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }), getSettings: vi.fn().mockResolvedValue({}) },
  tlsFingerprintProfiles: { list: vi.fn().mockResolvedValue([]) }
} }))
vi.mock('@/api/admin/accounts', () => ({ getAntigravityDefaultModelMapping: vi.fn().mockResolvedValue([]) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))

import CreateAccountModal from '../CreateAccountModal.vue'
import EditAccountModal from '../EditAccountModal.vue'

const BaseDialog = defineComponent({ props: { show: Boolean }, template: '<div v-if="show"><slot /><slot name="footer" /></div>' })
const OAuthAuthorizationFlow = defineComponent({
  emits: ['import-codex-session', 'import-codex-pat'],
  template: '<div><button type="button" data-testid="import-session" @click="$emit(\'import-codex-session\', \'session-json\')">session</button><button type="button" data-testid="import-pat" @click="$emit(\'import-codex-pat\', \'pat-token\')">pat</button></div>'
})
const stubs = { BaseDialog, OAuthAuthorizationFlow, ConfirmDialog: true, Select: true, Icon: true, PlatformIcon: true, ProxySelector: true, ProxyAdBanner: true, GroupSelector: true, ModelWhitelistSelector: true, QuotaLimitCard: true }
const account = (extra: Record<string, unknown> = {}) => ({
  id: 7, name: 'Cookie experiment', notes: '', platform: 'openai', type: 'oauth', parent_account_id: null,
  credentials: { access_token: 'test-access' }, extra, proxy_id: null, concurrency: 1, priority: 1,
  rate_multiplier: 1, status: 'active', group_ids: [], expires_at: null, auto_pause_on_expired: false
}) as any
const renderEdit = (value = account()) => mount(EditAccountModal, { props: { show: true, account: value, proxies: [], groups: [] }, global: { stubs } })
const renderCreate = () => mount(CreateAccountModal, { props: { show: true, proxies: [], groups: [] }, global: { stubs } })

beforeEach(() => {
  vi.clearAllMocks()
  mocks.update.mockResolvedValue(account())
  mocks.create.mockResolvedValue(account())
  mocks.session.mockResolvedValue({ created: 1, updated: 0, skipped: 0, failed: 0, errors: [], warnings: [] })
  mocks.pat.mockResolvedValue({})
})

describe('ticket credential account integration', () => {
  it('loads and saves an override, then clears it without losing other extra', async () => {
    const policy = { mode: 'cookie_state', ttl_seconds: 21, refresh_before_seconds: 0 }
    const wrapper = renderEdit(account({ custom_flag: 'keep', codex_ticket_credential_policy: policy }))
    expect(wrapper.get<HTMLSelectElement>('[data-testid="codex-ticket-credential-mode"]').element.value).toBe('cookie_state')
    expect(wrapper.get<HTMLInputElement>('[data-testid="codex-ticket-credential-refresh_before_seconds"]').element.value).toBe('0')
    await wrapper.get('#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(mocks.update.mock.calls.at(-1)?.[1].extra).toMatchObject({ custom_flag: 'keep', codex_ticket_credential_policy: policy })
    await wrapper.get('[data-testid="codex-ticket-credential-mode"]').setValue('inherit')
    await wrapper.get('#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(mocks.update.mock.calls.at(-1)?.[1].extra).toMatchObject({ custom_flag: 'keep', codex_ticket_credential_policy: { mode: 'inherit' } })
    expect(mocks.update.mock.calls.at(-1)?.[1].extra.codex_ticket_credential_policy).not.toHaveProperty('ttl_seconds')
    wrapper.unmount()
  })

  it('blocks invalid account timing and duplicate pending submissions', async () => {
    const wrapper = renderEdit()
    await wrapper.get('[data-testid="codex-ticket-credential-mode"]').setValue('cookie')
    await wrapper.get('[data-testid="codex-ticket-credential-ttl_seconds"]').setValue('0')
    await wrapper.get('#edit-account-form').trigger('submit.prevent')
    expect(mocks.update).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="codex-ticket-credential-ttl_seconds"]').setValue('20')
    let finish!: (value: unknown) => void
    mocks.update.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    await wrapper.get('#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    await wrapper.get('#edit-account-form').trigger('submit.prevent')
    expect(mocks.update).toHaveBeenCalledTimes(1)
    expect(wrapper.get<HTMLInputElement>('[data-testid="codex-ticket-credential-ttl_seconds"]').element.disabled).toBe(true)
    finish(account())
    await flushPromises()
    wrapper.unmount()
  })

  it.each([{ type: 'apikey' }, { parent_account_id: 1 }])('hides overrides for unsupported accounts %s', changes => {
    const wrapper = renderEdit({ ...account(), ...changes })
    expect(wrapper.find('[data-testid="codex-ticket-credential-settings"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it.each(['session', 'pat'])('retains the Cookie policy through Codex %s creation', async method => {
    const wrapper = renderCreate()
    await wrapper.findAll('button').find(button => button.text().includes('OpenAI'))!.trigger('click')
    expect(wrapper.get<HTMLSelectElement>('[data-testid="codex-ticket-credential-mode"]').element.value).toBe('inherit')
    await wrapper.get('[data-testid="codex-ticket-credential-mode"]').setValue('cookie')
    await wrapper.get('[data-testid="codex-ticket-credential-ttl_seconds"]').setValue('21')
    await wrapper.get('[data-testid="codex-ticket-credential-refresh_before_seconds"]').setValue('0')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('Cookie trial')
    await wrapper.get('#create-account-form').trigger('submit.prevent')
    await wrapper.get(`[data-testid="import-${method}"]`).trigger('click')
    await flushPromises()
    expect((method === 'session' ? mocks.session : mocks.pat)).toHaveBeenCalledWith(expect.objectContaining({ extra: expect.objectContaining({
      codex_ticket_proxy_mode: 'account', codex_ticket_credential_policy: { mode: 'cookie', ttl_seconds: 21, refresh_before_seconds: 0 }
    }) }))
    wrapper.unmount()
  })

  it('rejects invalid Cookie timing before opening OAuth import', async () => {
    const wrapper = renderCreate()
    await wrapper.findAll('button').find(button => button.text().includes('OpenAI'))!.trigger('click')
    await wrapper.get('[data-testid="codex-ticket-credential-mode"]').setValue('cookie_state')
    await wrapper.get('[data-testid="codex-ticket-credential-ttl_seconds"]').setValue('3601')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('Invalid trial')
    await wrapper.get('#create-account-form').trigger('submit.prevent')
    expect(wrapper.find('[data-testid="import-session"]').exists()).toBe(false)
    expect(mocks.session).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
