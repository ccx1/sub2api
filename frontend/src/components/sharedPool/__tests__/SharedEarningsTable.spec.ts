import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import SharedEarningsTable from '../SharedEarningsTable.vue'

const { userEarnings, adminEarnings, adminUserEarnings, transferUserEarnings, showSuccess, showError } = vi.hoisted(() => ({
  userEarnings: vi.fn(), adminEarnings: vi.fn(), adminUserEarnings: vi.fn(), transferUserEarnings: vi.fn(), showSuccess: vi.fn(), showError: vi.fn(),
}))
vi.mock('@/api/sharedPool', () => ({
  sharedPoolAPI: { earnings: userEarnings },
  adminSharedPoolAPI: { earnings: adminEarnings, userEarnings: adminUserEarnings, transferUserEarnings },
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess, showError }) }))

const base = { account_id: 7, group_id: 10, group_name: 'Internal dispatch group', billing_amount: 5, platform_amount: 1.05, owner_amount: 3.95, created_at: '2026-09-19T12:00:00Z' }
const page = { total: 2, items: [
  { ...base, id: 1, platform_rate_bps: 2000, proxy_rate_bps: 100 },
  { ...base, id: 2, platform_rate_bps: 0, proxy_rate_bps: 0 }
] }

describe('shared earnings rate snapshots', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    userEarnings.mockResolvedValue(page)
    adminEarnings.mockResolvedValue(page)
    adminUserEarnings.mockResolvedValue([])
    transferUserEarnings.mockResolvedValue({ id: 1, amount: 5, balance: 15 })
  })

  it('distinguishes billing from independent settlement and retains negative platform margin', async () => {
    userEarnings.mockResolvedValue({ total: 2, items: [
      { ...page.items[0], billing_amount: 2, base_amount: 10, settlement_multiplier: 0.5, settlement_amount: 5, spread_amount: -3, platform_amount: -1.95 },
      page.items[1]
    ] })
    const wrapper = mount(SharedEarningsTable, { global: { stubs: { Pagination: true } } }); await flushPromises()
    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].get('[data-test="settlement-amount"]').text()).toContain('$5.000000')
    expect(rows[0].text()).toContain('$10.000000 × 0.5')
    expect(rows[0].text()).toContain('$-3.000000')
    expect(rows[0].text()).toContain('$-1.950000')
    expect(rows[1].get('[data-test="settlement-amount"]').text()).toBe('—')
    wrapper.unmount()
  })

  it.each([false, true])('shows each ledger entry actual rates for administrator=%s', async admin => {
    const wrapper = mount(SharedEarningsTable, { props: { admin }, global: { stubs: { Pagination: true } } })
    await flushPromises()
    expect(wrapper.findAll('th').map(header => header.text())).toContain('sharedPool.revenueSplit')
    expect(wrapper.text().includes('sharedPool.groups')).toBe(admin)
    expect(wrapper.text().includes('Internal dispatch group')).toBe(admin)
    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].text()).toContain('sharedPool.platformShare 20%')
    expect(rows[0].text()).toContain('sharedPool.proxyShare 1%')
    expect(rows[1].text()).toContain('sharedPool.platformShare 0%')
    expect(rows[1].text()).toContain('sharedPool.proxyShare 0%')
    expect(admin ? adminEarnings : userEarnings).toHaveBeenCalledWith(1)
    expect(admin ? userEarnings : adminEarnings).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('shows administrator earnings grouped by contributor', async () => {
    adminUserEarnings.mockResolvedValue([{ user_id: 6, email: 'owner@example.com', account_count: 3, account_tiers: [{ tier: 'pro', count: 2 }, { tier: 'team', count: 1 }], earnings_count: 3, total_earned: 12.5, available: 5, pending: 2.5, transferred: 5 }])
    const wrapper = mount(SharedEarningsTable, { props: { admin: true }, global: { stubs: { Pagination: true } } })
    await flushPromises()
    expect(adminUserEarnings).toHaveBeenCalledOnce()
    expect(wrapper.get('[data-test="user-earnings-summary"]').text()).toContain('owner@example.com')
    expect(wrapper.get('[data-test="user-earnings-summary"]').text()).toContain('$12.500000')
    expect(wrapper.get('[data-test="user-earnings-summary"]').text()).toContain('3')
    expect(wrapper.get('[data-test="account-tiers"]').text()).toContain('pro × 2')
    expect(wrapper.get('[data-test="account-tiers"]').text()).toContain('team × 1')
    wrapper.unmount()
  })

  it('keeps the account tier cell usable when older responses omit account_tiers', async () => {
    adminUserEarnings.mockResolvedValue([{ user_id: 7, email: 'legacy@example.com', account_count: 1, earnings_count: 0, total_earned: 0, available: 0, pending: 0, transferred: 0 }])
    const wrapper = mount(SharedEarningsTable, { props: { admin: true }, global: { stubs: { Pagination: true } } })
    await flushPromises()
    expect(wrapper.get('[data-test="account-tiers"]').text()).toBe('—')
    wrapper.unmount()
  })

  it('confirms an administrator transfer and refreshes contributor totals', async () => {
    const contributor = { user_id: 6, email: 'owner@example.com', account_count: 3, earnings_count: 3, total_earned: 12.5, available: 5, pending: 2.5, transferred: 5 }
    adminUserEarnings.mockResolvedValueOnce([contributor]).mockResolvedValueOnce([{ ...contributor, available: 0, transferred: 10 }])
    const wrapper = mount(SharedEarningsTable, {
      props: { admin: true },
      global: {
        stubs: {
          Pagination: true,
          ConfirmDialog: {
            props: ['show', 'message'],
            emits: ['confirm', 'cancel'],
            template: '<div v-if="show" data-test="transfer-confirm"><p>{{ message }}</p><button data-test="transfer-confirm-submit" @click="$emit(\'confirm\')">confirm</button></div>',
          },
        },
      },
    })
    await flushPromises()

    await wrapper.get('[data-test="transfer-user-earnings-6"]').trigger('click')
    expect(wrapper.get('[data-test="transfer-confirm"]').text()).toContain('sharedPool.adminTransferConfirm')
    await wrapper.get('[data-test="transfer-confirm-submit"]').trigger('click')
    await flushPromises()

    expect(transferUserEarnings).toHaveBeenCalledWith(6)
    expect(adminUserEarnings).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-test="transfer-user-earnings-6"]').attributes('disabled')).toBeDefined()
    expect(showSuccess).toHaveBeenCalledWith('sharedPool.adminTransferSuccess')
    wrapper.unmount()
  })

  it('disables empty transfers and reports administrator transfer failures', async () => {
    adminUserEarnings.mockResolvedValue([{ user_id: 7, email: 'empty@example.com', account_count: 1, earnings_count: 1, total_earned: 0, available: 0, pending: 0, transferred: 0 }])
    const wrapper = mount(SharedEarningsTable, {
      props: { admin: true },
      global: {
        stubs: {
          Pagination: true,
          ConfirmDialog: {
            props: ['show'],
            emits: ['confirm', 'cancel'],
            template: '<div v-if="show" data-test="transfer-confirm"><button data-test="transfer-confirm-submit" @click="$emit(\'confirm\')">confirm</button></div>',
          },
        },
      },
    })
    await flushPromises()

    expect(wrapper.get('[data-test="transfer-user-earnings-7"]').attributes('disabled')).toBeDefined()
    expect(transferUserEarnings).not.toHaveBeenCalled()
    wrapper.unmount()

    adminUserEarnings.mockResolvedValue([{ user_id: 8, email: 'failure@example.com', account_count: 1, earnings_count: 1, total_earned: 3, available: 3, pending: 0, transferred: 0 }])
    transferUserEarnings.mockRejectedValueOnce(new Error('transfer failed'))
    const failureWrapper = mount(SharedEarningsTable, {
      props: { admin: true },
      global: {
        stubs: {
          Pagination: true,
          ConfirmDialog: {
            props: ['show'],
            emits: ['confirm', 'cancel'],
            template: '<div v-if="show" data-test="transfer-confirm"><button data-test="transfer-confirm-submit" @click="$emit(\'confirm\')">confirm</button></div>',
          },
        },
      },
    })
    await flushPromises()
    await failureWrapper.get('[data-test="transfer-user-earnings-8"]').trigger('click')
    await failureWrapper.get('[data-test="transfer-confirm-submit"]').trigger('click')
    await flushPromises()
    expect(showError).toHaveBeenCalledWith('transfer failed')
    failureWrapper.unmount()
  })
})
