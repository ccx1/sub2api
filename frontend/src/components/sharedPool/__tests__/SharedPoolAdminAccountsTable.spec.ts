import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import SharedPoolAdminAccountsTable from '../SharedPoolAdminAccountsTable.vue'
import type { SharedAccount } from '@/api/sharedPool'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, te: () => true }) }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
const base = {
  id: 7, name: 'Shared account', platform: 'openai', type: 'oauth', dispatch_consent: true,
  subscription_tier: '', subscription_tier_override: '', settlement_multiplier: 1, enabled: true,
  status: 'active', admin_disabled: false, groups: [], group_ids: [], concurrency: 2,
  platform_rate_bps: 500, proxy_rate_bps: 100, proxy_mode: 'random', owner_user_id: 4,
  total_earnings: 2, today_earnings: 1, estimated_earnings: null, protection_enabled: true
} as SharedAccount

describe('administrator shared account tiers', () => {
  it('shows an unknown subscription tier separately from a 1x shared multiplier', async () => {
    const wrapper = mount(SharedPoolAdminAccountsTable, { props: { accounts: [base] } })
    const tier = wrapper.get('[data-test="admin-account-tier"]')
    expect(tier.text()).toContain('sharedPool.unknownTier')
    expect(tier.text()).toContain('sharedPool.subscriptionTierAutomatic')
    expect(tier.text()).not.toContain('1x')
    expect(wrapper.get('[data-test="admin-account-multiplier"]').text()).toBe('sharedPool.settlementMultiplier 1x')
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('allocate')).toEqual([[base]])
  })

  it('shows the recognized subscription tier and administrator override source', () => {
    const wrapper = mount(SharedPoolAdminAccountsTable, { props: { accounts: [{ ...base, subscription_tier: 'pro', subscription_tier_override: 'pro', settlement_multiplier: 0.5 }] } })
    expect(wrapper.get('[data-test="admin-account-tier"]').text()).toContain('Pro 20x')
    expect(wrapper.get('[data-test="admin-account-tier"]').text()).toContain('sharedPool.subscriptionTierManual')
    expect(wrapper.get('[data-test="admin-account-multiplier"]').text()).toContain('0.5x')
  })

  it('keeps long group lists compact and exposes all names on hover', () => {
    const groups = [1, 2, 3, 4, 5].map(id => ({ id, name: `Shared ${id}` }))
    const wrapper = mount(SharedPoolAdminAccountsTable, { props: { accounts: [{ ...base, group_ids: groups.map(group => group.id), groups }] } })
    const more = wrapper.get('[data-test="admin-account-groups-more"]')
    expect(wrapper.text()).toContain('Shared 1')
    expect(wrapper.text()).toContain('Shared 3')
    expect(wrapper.text()).not.toContain('Shared 4')
    expect(more.text()).toBe('+2')
    expect(more.attributes('title')).toBe('Shared 1\nShared 2\nShared 3\nShared 4\nShared 5')
    expect(more.attributes('aria-label')).toContain('Shared 5')
  })
})
