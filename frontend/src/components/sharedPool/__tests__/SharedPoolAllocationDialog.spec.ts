import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import SharedPoolAllocationDialog from '../SharedPoolAllocationDialog.vue'
import type { AdminGroup } from '@/types'
import type { SharedAccount } from '@/api/sharedPool'
import Select from '@/components/common/Select.vue'

const { allocate } = vi.hoisted(() => ({ allocate: vi.fn() }))
vi.mock('@/api/sharedPool', () => ({ adminSharedPoolAPI: { allocate } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const group = (id: number, overrides: Partial<AdminGroup> = {}) => ({ id, name: `Group ${id}`, platform: 'openai', status: 'active', subscription_type: 'standard', rate_multiplier: 1, is_exclusive: false, is_shared_pool: false, ...overrides }) as AdminGroup
const groups = [
  group(1), group(2, { is_shared_pool: true }), group(3, { status: 'inactive' }),
  group(4, { rate_multiplier: 0 }), group(5, { is_exclusive: true }), group(6, { subscription_type: 'subscription' }),
  group(7, { platform: 'gemini' }), group(8, { require_oauth_only: true }),
  group(9, { subscription_type: 'subscription', is_exclusive: true }), group(10, { platform: 'gemini', subscription_type: 'subscription' })
]
function render(overrides: Partial<SharedAccount> = {}) {
  return mount(SharedPoolAllocationDialog, { props: { groups, account: { id: 7, name: 'Account', platform: 'openai', type: 'apikey', dispatch_consent: true, group_ids: [], enabled: true, admin_disabled: false, ...overrides } as SharedAccount }, global: { stubs: { BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' } } } })
}
const save = (wrapper: ReturnType<typeof render>) => wrapper.findAll('button').find(button => button.text() === 'common.save')!.trigger('click')
const choices = (wrapper: ReturnType<typeof render>) => wrapper.findAll('input[type="checkbox"]').map(input => Number(input.attributes('value')))

describe('shared account dispatch group allocation', () => {
  beforeEach(() => { vi.clearAllMocks(); allocate.mockResolvedValue({}) })

  it('allows multiple standard, subscription, exclusive and zero-rate groups after consent', async () => {
    const wrapper = render()
    expect(choices(wrapper)).toEqual([1, 2, 4, 5, 6, 9])
    for (const id of [1, 4, 5, 6, 9]) await wrapper.get(`input[value="${id}"]`).setValue(true)
    await save(wrapper); await flushPromises()
    expect(allocate).toHaveBeenCalledWith(7, { group_ids: [1, 4, 5, 6, 9] })
    expect(wrapper.findAll('input[type="number"]')).toHaveLength(1)
    expect(wrapper.get('#pool-account-priority').element).toBeDefined()
    expect(wrapper.emitted('saved')).toHaveLength(1)
  })

  it('keeps legacy accounts limited to shared groups', () => {
    expect(choices(render({ dispatch_consent: false }))).toEqual([2])
    expect(choices(render({ type: 'oauth' }))).toEqual([1, 2, 4, 5, 6, 8, 9])
  })

  it('keeps legacy authorization restricted even when incompatible groups retain a shared flag', async () => {
    const wrapper = render({ dispatch_consent: false, type: 'oauth' })
    await wrapper.setProps({ groups: groups.map(item => ({ ...item, is_shared_pool: [2, 3, 4, 5, 6, 7, 9, 10].includes(item.id) })) })
    expect(choices(wrapper)).toEqual([2])
  })

  it('retains invalid saved assignments until the administrator explicitly removes them', async () => {
    const wrapper = render({ group_ids: [3, 99] })
    expect(choices(wrapper)).toEqual([1, 2, 3, 4, 5, 6, 9, 99])
    await wrapper.get('input[value="1"]').setValue(true)
    await save(wrapper)
    expect(allocate).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toBe('sharedPool.allocationInvalid')
    await wrapper.get('input[value="3"]').setValue(false)
    await wrapper.get('input[value="99"]').setValue(false)
    expect(wrapper.get('input[value="3"]').attributes('disabled')).toBeDefined()
    await save(wrapper); await flushPromises()
    expect(allocate).toHaveBeenCalledWith(7, { group_ids: [1] })
  })

  it.each([false, true])('updates both scheduling gates to %s without granting user consent', async enabled => {
    const wrapper = render({ enabled: !enabled, admin_disabled: enabled, dispatch_consent: false })
    expect(wrapper.get('[data-test="legacy-dispatch-notice"]').text()).toContain('sharedPool.adminLegacyDispatchHint')
    await wrapper.get('[data-test="admin-dispatch"]').trigger('click')
    await save(wrapper); await flushPromises()
    expect(allocate).toHaveBeenCalledWith(7, { enabled, admin_disabled: !enabled })
    expect(allocate.mock.calls[0][1]).not.toHaveProperty('dispatch_consent')
  })

  it('allows supported OAuth tier overrides and explicit automatic detection', async () => {
    const wrapper = render({ type: 'oauth', subscription_tier: 'free' })
    expect(wrapper.get('[data-test="admin-effective-tier"]').text()).toContain('Free')
    expect(wrapper.get('[data-test="admin-effective-tier"]').text()).toContain('sharedPool.subscriptionTierAutomatic')
    const select = wrapper.getComponent(Select)
    expect(select.props('options')).toContainEqual({ value: '', label: 'sharedPool.subscriptionTierAutomatic' })
    select.vm.$emit('update:modelValue', 'pro')
    await wrapper.vm.$nextTick()
    await save(wrapper); await flushPromises()
    expect(allocate).toHaveBeenLastCalledWith(7, { subscription_tier: 'pro' })
    wrapper.unmount()
    const overridden = render({ type: 'oauth', subscription_tier: 'pro', subscription_tier_override: 'pro' })
    expect(overridden.get('[data-test="admin-effective-tier"]').text()).toContain('Pro 20x')
    expect(overridden.get('[data-test="admin-effective-tier"]').text()).toContain('sharedPool.subscriptionTierManual')
    overridden.getComponent(Select).vm.$emit('update:modelValue', '')
    await overridden.vm.$nextTick()
    await save(overridden); await flushPromises()
    expect(allocate).toHaveBeenLastCalledWith(7, { subscription_tier: '' })
  })

  it('does not change scheduling or tier when only assignments are saved', async () => {
    const wrapper = render({ enabled: false, admin_disabled: false, type: 'oauth', subscription_tier: 'pro' })
    await wrapper.get('input[value="1"]').setValue(true)
    await save(wrapper); await flushPromises()
    expect(allocate).toHaveBeenCalledWith(7, { group_ids: [1] })
  })

  it('hides tier editing for API keys and rejects an unsupported tier emitted by a control', async () => {
    expect(render().findComponent(Select).exists()).toBe(false)
    const wrapper = render({ type: 'oauth' })
    wrapper.getComponent(Select).vm.$emit('update:modelValue', 'google_ai_pro')
    await save(wrapper); await flushPromises()
    expect(allocate).not.toHaveBeenCalled()
  })

  it('distinguishes unknown tiers from settlement and explains unsupported OAuth platforms', () => {
    const unknown = render({ type: 'oauth', subscription_tier: '', settlement_multiplier: 1 })
    expect(unknown.get('[data-test="admin-effective-tier"]').text()).toContain('sharedPool.unknownTier')
    expect(unknown.get('[data-test="admin-effective-tier"]').text()).not.toContain('1x')
    const unsupported = render({ platform: 'anthropic', type: 'oauth', subscription_tier: '' })
    expect(unsupported.findComponent(Select).exists()).toBe(false)
    expect(unsupported.text()).toContain('sharedPool.adminTierUnsupported')
  })

  it('enables unassigned accounts without preventing automatic group assignment', async () => {
    const wrapper = render({ enabled: false, group_ids: [], type: 'oauth' })
    await wrapper.get('[data-test="admin-dispatch"]').trigger('click')
    await save(wrapper); await flushPromises()
    expect(allocate).toHaveBeenCalledWith(7, { enabled: true, admin_disabled: false })
    expect(allocate.mock.calls[0][1]).not.toHaveProperty('group_ids')
  })

  it('allows scheduling changes independently of invalid old assignments and sends explicit removals', async () => {
    const wrapper = render({ group_ids: [99], type: 'oauth' })
    await wrapper.get('[data-test="admin-dispatch"]').trigger('click')
    await save(wrapper); await flushPromises()
    expect(allocate).toHaveBeenLastCalledWith(7, { enabled: false, admin_disabled: true })
    wrapper.unmount()
    const clear = render({ group_ids: [1] })
    await clear.get('input[value="1"]').setValue(false)
    await save(clear); await flushPromises()
    expect(allocate).toHaveBeenLastCalledWith(7, { group_ids: [] })
  })

  it.each([0, 100])('updates account priority to %s without changing assignment or authorization', async priority => {
    const wrapper = render({ priority: 50 })
    await wrapper.get('#pool-account-priority').setValue(priority)
    await save(wrapper); await flushPromises()
    expect(allocate).toHaveBeenCalledWith(7, { priority })
  })

  it.each([-1, 101, 1.5, ''])('rejects invalid account priority %s', async priority => {
    const wrapper = render({ priority: 50 })
    await wrapper.get('#pool-account-priority').setValue(priority)
    await save(wrapper); await flushPromises()
    expect(allocate).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toBe('sharedPool.invalidPriority')
  })
})
