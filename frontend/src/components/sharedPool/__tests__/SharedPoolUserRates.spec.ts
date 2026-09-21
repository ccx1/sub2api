import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import SharedPoolUserRates from '../SharedPoolUserRates.vue'

const { userRates, saveUserRate } = vi.hoisted(() => ({ userRates: vi.fn(), saveUserRate: vi.fn() }))
vi.mock('@/api/sharedPool', () => ({ adminSharedPoolAPI: { userRates, saveUserRate } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const rate = { user_id: 9, platform_rate_bps: 500, proxy_rate_bps: null, settlement_multiplier: 0.3 }

describe('shared settlement user override', () => {
  beforeEach(() => { vi.clearAllMocks(); userRates.mockResolvedValue([rate]); saveUserRate.mockResolvedValue({}) })

  it('preserves the existing multiplier when editing shares', async () => {
    const wrapper = mount(SharedPoolUserRates); await flushPromises()
    expect(wrapper.get('[data-test="user-multiplier"]').text()).toBe('0.3x')
    await wrapper.get('tbody button').trigger('click')
    expect((wrapper.get('#user-settlement-multiplier').element as HTMLInputElement).value).toBe('0.3')
    await wrapper.get('#user-platform-rate').setValue(6)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(saveUserRate).toHaveBeenCalledWith(9, { platform_rate_bps: 600, proxy_rate_bps: null, settlement_multiplier: 0.3 })
  })

  it.each([{ input: '', expected: null }, { input: '0', expected: 0 }, { input: '0.000001', expected: 0.000001 }])('supports override $input without losing zero or inheritance', async ({ input, expected }) => {
    const wrapper = mount(SharedPoolUserRates); await flushPromises()
    await wrapper.get('#rate-user-id').setValue(9)
    await wrapper.get('#user-settlement-multiplier').setValue(input)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(saveUserRate).toHaveBeenCalledWith(9, { platform_rate_bps: null, proxy_rate_bps: null, settlement_multiplier: expected })
  })

  it.each([-1, 101])('rejects an invalid multiplier %s before saving', async value => {
    const wrapper = mount(SharedPoolUserRates); await flushPromises()
    await wrapper.get('#rate-user-id').setValue(9)
    await wrapper.get('#user-settlement-multiplier').setValue(value)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(saveUserRate).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toBe('sharedPool.invalidSettlementMultiplier')
  })
})
