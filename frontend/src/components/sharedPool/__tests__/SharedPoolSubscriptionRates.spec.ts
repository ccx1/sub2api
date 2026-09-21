import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import SharedPoolSubscriptionRates from '../SharedPoolSubscriptionRates.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

function render(rates?: Record<string, Record<string, number>>) {
  return mount(SharedPoolSubscriptionRates, {
    props: { rates },
    global: { stubs: { PlatformIcon: true, Icon: true } }
  })
}

describe('shared pool subscription rates', () => {
  it('serializes configured platform and tier multipliers', async () => {
    const wrapper = render({ openai: { free: 0, pro: 1.5 }, gemini: { google_ai_pro: 2 } })
    const values = wrapper.vm.$.exposed?.serialize()
    expect(values).toEqual({ openai: { free: 0, pro: 1.5 }, gemini: { google_ai_pro: 2 } })
  })

  it('omits blank tiers so they inherit user or global settings', () => {
    const wrapper = render({ openai: { free: 1 } })
    const values = wrapper.vm.$.exposed?.serialize()
    expect(values).toEqual({ openai: { free: 1 } })
    expect(wrapper.find('[data-rate-platform="openai"]').exists()).toBe(true)
    expect(wrapper.find('[data-rate-tier="pro"]').exists()).toBe(true)
  })

  it('rejects multipliers outside the supported range', async () => {
    const wrapper = render()
    await wrapper.get('#subscription-rate-openai-free').setValue(101)
    expect(() => wrapper.vm.$.exposed?.serialize()).toThrow('sharedPool.invalidSettlementMultiplier')
  })
})
