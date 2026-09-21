import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import SharedRevenueSplit from '../SharedRevenueSplit.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const config = { platforms: [], max_concurrency: 10, platform_rate_bps: 2000, proxy_rate_bps: 100 }

describe('contributor revenue split', () => {
  it('uses one sentence without a card when embedded in a dialog', () => {
    const wrapper = mount(SharedRevenueSplit, { props: { config, useRandomProxy: true, inline: true } })
    expect(wrapper.get('[data-test="revenue-split-inline"]').text()).toBe('sharedPool.revenueSplitInline')
    expect(wrapper.find('[data-test="revenue-split"]').exists()).toBe(false)
    expect(wrapper.find('dl').exists()).toBe(false)
  })

  it('shows the conditional platform fee card with both contributor outcomes', () => {
    const wrapper = mount(SharedRevenueSplit, { props: { config: { ...config, platform_rate_bps: 500 }, useRandomProxy: true, compareProxyModes: true } })
    expect(wrapper.get('[data-test="conditional-platform-share"]').text()).toBe('5% + (1%)')
    expect(wrapper.get('[data-test="owner-share"]').text()).toBe('94%')
    expect(wrapper.get('[data-test="custom-owner-share"]').text()).toBe('95%')
    expect(wrapper.text()).toContain('sharedPool.feeConditionalHint')
  })

  it('adds proxy share as percentage points and recomputes on custom proxy selection', async () => {
    const wrapper = mount(SharedRevenueSplit, { props: { config, useRandomProxy: true } })
    expect(wrapper.get('[data-test="platform-share"]').text()).toBe('20%')
    expect(wrapper.get('[data-test="proxy-share"]').text()).toBe('1%')
    expect(wrapper.get('[data-test="owner-share"]').text()).toBe('79%')
    await wrapper.setProps({ useRandomProxy: false })
    expect(wrapper.get('[data-test="proxy-share"]').text()).toBe('0%')
    expect(wrapper.get('[data-test="owner-share"]').text()).toBe('80%')
  })

  it('respects zero and fractional user overrides', () => {
    const wrapper = mount(SharedRevenueSplit, { props: { config: { ...config, platform_rate_bps: 0, proxy_rate_bps: 125 }, useRandomProxy: true } })
    expect(wrapper.get('[data-test="platform-share"]').text()).toBe('0%')
    expect(wrapper.get('[data-test="proxy-share"]').text()).toBe('1.25%')
    expect(wrapper.get('[data-test="owner-share"]').text()).toBe('98.75%')
  })

  it('does not present a made-up rate when settings are invalid', () => {
    const wrapper = mount(SharedRevenueSplit, { props: { config: { ...config, platform_rate_bps: 10001 }, useRandomProxy: true } })
    expect(wrapper.get('[role="alert"]').text()).toBe('sharedPool.rateUnavailable')
    expect(wrapper.find('[data-test="owner-share"]').exists()).toBe(false)
  })
})
