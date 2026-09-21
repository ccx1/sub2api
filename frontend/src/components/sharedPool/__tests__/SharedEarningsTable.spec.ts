import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import SharedEarningsTable from '../SharedEarningsTable.vue'

const { userEarnings, adminEarnings } = vi.hoisted(() => ({ userEarnings: vi.fn(), adminEarnings: vi.fn() }))
vi.mock('@/api/sharedPool', () => ({ sharedPoolAPI: { earnings: userEarnings }, adminSharedPoolAPI: { earnings: adminEarnings } }))
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
})
