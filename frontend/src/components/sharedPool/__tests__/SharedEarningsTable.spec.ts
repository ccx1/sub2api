import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import SharedEarningsTable from '../SharedEarningsTable.vue'

const { userEarnings, adminEarnings, adminUserEarnings } = vi.hoisted(() => ({ userEarnings: vi.fn(), adminEarnings: vi.fn(), adminUserEarnings: vi.fn() }))
vi.mock('@/api/sharedPool', () => ({ sharedPoolAPI: { earnings: userEarnings }, adminSharedPoolAPI: { earnings: adminEarnings, userEarnings: adminUserEarnings } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))

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
})
