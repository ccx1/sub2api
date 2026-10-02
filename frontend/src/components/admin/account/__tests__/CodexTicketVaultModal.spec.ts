import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CodexTicketVaultModal from '../CodexTicketVaultModal.vue'
import type { CodexTicketVault } from '@/api/admin/codexTicketVault'

const mocks = vi.hoisted(() => ({
  load: vi.fn(),
  revoke: vi.fn(),
  success: vi.fn(),
  error: vi.fn()
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => (params ? `${key}:${JSON.stringify(params)}` : key) })
  }
})
vi.mock('@/api/admin/codexTicketVault', () => ({
  getCodexTicketVault: mocks.load,
  revokeCodexTicketVault: mocks.revoke
}))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: mocks.success, showError: mocks.error })
}))

const prefix = 'admin.accounts.codexTicketVault'
const fpA = 'a'.repeat(24)
const fpB = 'b'.repeat(24)
const fpC = 'c'.repeat(24)

const vault = (): CodexTicketVault => ({
  account_id: 7,
  account_name: 'acc',
  ticket_enabled: true,
  harvest_enabled: true,
  config_enabled: true,
  proxy_available: true,
  credential_mode: 'cookie',
  pool_capacity: 5,
  ttl_seconds: 600,
  cookie_ttl_seconds: 20,
  policy: { usage_mode: 'aged', min_ticket_age_seconds: 300, consume_after_use: true, fail_closed: true },
  models: [{
    model: 'gpt-6-astra',
    configured: true,
    total: 3,
    available: 1,
    maturing: 1,
    slots: [
      { label: 'primary', fingerprint: fpA, status: 'maturing', business_selected: false, credential_mode: 'cookie', length: 0, cookie_names: ['__oailb'], verified: true, verification_skipped: false, captured_at: '2026-10-01T00:00:00Z', age_seconds: 60, mature_at: '2026-10-01T00:05:00Z', expires_at: '2026-10-01T00:10:00Z', remaining_seconds: 540, attempts: 1, route_node: { host: 'chat.gateway.unified-88.api.openai.com', recognized: true, name: 'unified-88', number: 88, country: 'kr', region: 'seoul' } },
      { label: 'standby', fingerprint: fpB, status: 'available', business_selected: true, credential_mode: 'state', length: 292, cookie_names: [], verified: true, verification_skipped: false, age_seconds: 400, remaining_seconds: 200, attempts: 1 },
      { label: 'reserve-1', fingerprint: fpC, status: 'revoked', business_selected: false, length: 292, cookie_names: null, verified: true, verification_skipped: false, age_seconds: 500, remaining_seconds: 0, attempts: 1, invalidation: { reason: 'admin_revoked', source: 'ticket_vault', invalidated_at: '2026-10-01T00:01:00Z' } }
    ]
  }],
  server_time: '2026-10-01T00:01:00Z'
})

function render() {
  return mount(CodexTicketVaultModal, {
    props: { show: true, account: { id: 7, name: 'acc' } },
    global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' } } }
  })
}

describe('CodexTicketVaultModal', () => {
  beforeEach(() => {
    Object.values(mocks).forEach(mock => mock.mockReset())
    mocks.load.mockResolvedValue(vault())
    mocks.revoke.mockResolvedValue({ account_id: 7, model: 'gpt-6-astra', revoked: 1, remaining: 0 })
  })

  it('renders vault metadata, policy and slot status', async () => {
    const wrapper = render()
    await flushPromises()
    expect(mocks.load).toHaveBeenCalledWith(7)
    const overview = wrapper.get('[data-testid="vault-overview"]').text()
    expect(overview).toContain(`${prefix}.usageModes.aged`)
    expect(overview).toContain(`${prefix}.seconds:{"count":300}`)
    expect(overview).toContain(`${prefix}.fields.cookieTtl`)
    expect(wrapper.get('[data-testid="vault-model-summary"]').text()).toContain('3 / 5')
    const rows = wrapper.findAll('[data-testid="vault-slot"]')
    expect(rows).toHaveLength(3)
    expect(rows[0].text()).toContain(`${prefix}.statuses.maturing`)
    expect(rows[0].text()).toContain('unified-88')
    expect(rows[0].text()).toContain(fpA)
    expect(rows[1].text()).toContain(`${prefix}.businessSelected`)
    expect(rows[1].text()).toContain(`${prefix}.credentialState:{"length":292}`)
    expect(rows[2].text()).toContain('admin.accounts.codexTicketHistory.lifecycle.reasons.admin_revoked')
    expect(rows[2].text()).toContain('admin.accounts.codexTicketHistory.lifecycle.sources.ticket_vault')
    // 已作废的票不再提供作废按钮。
    expect(rows[2].find('[data-testid="vault-revoke"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="vault-revoke"]')).toHaveLength(2)
  })

  it('revokes one ticket by fingerprint and reloads', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.findAll('[data-testid="vault-revoke"]')[1].trigger('click')
    await flushPromises()
    expect(mocks.revoke).toHaveBeenCalledWith(7, { model: 'gpt-6-astra', fingerprint: fpB })
    expect(mocks.success).toHaveBeenCalledWith(`${prefix}.revoked:{"count":1}`)
    expect(mocks.load).toHaveBeenCalledTimes(2)
  })

  it('requires confirmation before revoking all and warns on remaining tickets', async () => {
    mocks.revoke.mockResolvedValue({ account_id: 7, model: 'gpt-6-astra', revoked: 1, remaining: 1 })
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="vault-revoke-all"]').trigger('click')
    expect(mocks.revoke).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="vault-revoke-all-cancel"]').trigger('click')
    expect(wrapper.find('[data-testid="vault-revoke-all-confirm"]').exists()).toBe(false)
    await wrapper.get('[data-testid="vault-revoke-all"]').trigger('click')
    await wrapper.get('[data-testid="vault-revoke-all-confirm"]').trigger('click')
    await flushPromises()
    expect(mocks.revoke).toHaveBeenCalledWith(7, { model: 'gpt-6-astra', all: true })
    expect(wrapper.get('[data-testid="vault-warning"]').text()).toBe(`${prefix}.revokeRemaining:{"count":1}`)
    expect(mocks.load).toHaveBeenCalledTimes(2)
  })

  it('shows load and revoke errors', async () => {
    mocks.load.mockRejectedValueOnce(new Error('boom'))
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get('[data-testid="vault-error"]').text()).not.toBe('')
    expect(wrapper.find('[data-testid="vault-model"]').exists()).toBe(false)

    await wrapper.get('[data-testid="vault-refresh"]').trigger('click')
    await flushPromises()
    mocks.revoke.mockRejectedValueOnce(new Error('nope'))
    await wrapper.findAll('[data-testid="vault-revoke"]')[0].trigger('click')
    await flushPromises()
    expect(mocks.error).toHaveBeenCalled()
    expect(mocks.success).not.toHaveBeenCalled()
  })

  it('does not load while hidden', async () => {
    mount(CodexTicketVaultModal, { props: { show: false, account: { id: 7, name: 'acc' } }, global: { stubs: { BaseDialog: true } } })
    await flushPromises()
    expect(mocks.load).not.toHaveBeenCalled()
  })
})
