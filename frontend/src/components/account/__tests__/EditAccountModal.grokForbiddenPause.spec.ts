import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'

const { updateMock } = vi.hoisted(() => ({ updateMock: vi.fn() }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isSimpleMode: true }) }))
vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      getManagementCapabilities: vi.fn().mockResolvedValue({}),
      update: updateMock,
      checkMixedChannelRisk: vi.fn().mockResolvedValue({ has_risk: false })
    },
    settings: {
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: vi.fn().mockResolvedValue({})
    },
    tlsFingerprintProfiles: { list: vi.fn().mockResolvedValue([]) }
  }
}))
vi.mock('@/api/admin/accounts', () => ({ getAntigravityDefaultModelMapping: vi.fn() }))
vi.mock('vue-i18n', async () => ({
  ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')),
  useI18n: () => ({ t: (key: string) => key })
}))

import EditAccountModal from '../EditAccountModal.vue'

const dialog = defineComponent({
  props: { show: Boolean },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})
function account(extra: Record<string, unknown> = {}, platform = 'grok') {
  return {
    id: 5, name: 'Grok test', platform, type: 'oauth', status: 'active',
    credentials: { expires_at: '2027-01-01T00:00:00Z' },
    credentials_status: { has_access_token: true, has_refresh_token: true },
    extra, proxy_id: null, concurrency: 1, priority: 1, rate_multiplier: 1,
    group_ids: [], expires_at: null, auto_pause_on_expired: false
  } as any
}
function mountModal(value = account()) {
  return mount(EditAccountModal, {
    props: { show: true, account: value, proxies: [], groups: [] },
    global: { stubs: { BaseDialog: dialog, Select: true, Icon: true, ProxySelector: true, GroupSelector: true, ModelWhitelistSelector: true } }
  })
}

describe('Grok unclassified 403 scheduling', () => {
  beforeEach(() => { updateMock.mockReset(); updateMock.mockResolvedValue(account()) })

  it.each([undefined, false, 'true', 1])('defaults disabled for %s', (saved) => {
    const wrapper = mountModal(account({ grok_skip_forbidden_pause: saved }))
    expect(wrapper.get('[data-testid="grok-skip-forbidden-pause-toggle"]').attributes('aria-checked')).toBe('false')
  })

  it('persists a user opt-in while retaining unrelated extra and credentials', async () => {
    const value = account({ custom_setting: 'keep', grok_client_tool_cache_enabled: false })
    const wrapper = mountModal(value)
    await wrapper.get('[data-testid="grok-skip-forbidden-pause-toggle"]').trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await vi.waitFor(() => expect(updateMock).toHaveBeenCalledTimes(1))
    expect(updateMock.mock.calls[0]?.[1].extra).toMatchObject({
      grok_skip_forbidden_pause: true, custom_setting: 'keep', grok_client_tool_cache_enabled: false
    })
  })

  it('resets the setting when switching accounts', async () => {
    const wrapper = mountModal(account({ grok_skip_forbidden_pause: true }))
    expect(wrapper.get('[data-testid="grok-skip-forbidden-pause-toggle"]').attributes('aria-checked')).toBe('true')
    await wrapper.setProps({ account: { ...account(), id: 6 } })
    expect(wrapper.get('[data-testid="grok-skip-forbidden-pause-toggle"]').attributes('aria-checked')).toBe('false')
  })

  it('only appears for Grok accounts', () => {
    const wrapper = mountModal(account({}, 'openai'))
    expect(wrapper.find('[data-testid="grok-skip-forbidden-pause-toggle"]').exists()).toBe(false)
  })
})
