import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ProxyGroupsDialog from '../ProxyGroupsDialog.vue'
import BatchProxyGroupDialog from '../BatchProxyGroupDialog.vue'

const api = vi.hoisted(() => ({ createGroup: vi.fn(), updateGroup: vi.fn(), deleteGroup: vi.fn(), batchSetGroup: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { proxies: api } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const groups = [{ id: 7, name: 'Hong Kong', proxy_count: 3, active_proxy_count: 2 }]
const global = { stubs: {
  BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
  Select: {
    props: ['modelValue', 'options'],
    emits: ['update:modelValue'],
    template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value === \'\' ? null : Number($event.target.value))"><option v-for="option in options" :value="option.value">{{ option.label }}</option></select>'
  }
} }
const mounted: { unmount: () => void }[] = []
afterEach(() => mounted.splice(0).forEach(wrapper => wrapper.unmount()))
beforeEach(() => vi.clearAllMocks())

describe('proxy group management', () => {
  it('creates a trimmed name and refreshes the parent after success', async () => {
    const wrapper = mount(ProxyGroupsDialog, { props: { show: true, groups }, global })
    mounted.push(wrapper)
    await wrapper.get('input').setValue('  Tokyo  ')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.createGroup).toHaveBeenCalledWith('Tokyo')
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it('renames a group without replacing membership', async () => {
    const wrapper = mount(ProxyGroupsDialog, { props: { show: true, groups }, global })
    mounted.push(wrapper)
    await wrapper.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
    await wrapper.get('input').setValue('HK')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.updateGroup).toHaveBeenCalledWith(7, 'HK')
    expect(api.createGroup).not.toHaveBeenCalled()
  })

  it('requires an explicit delete action and keeps a referenced group visible on failure', async () => {
    api.deleteGroup.mockRejectedValueOnce({ response: { data: { message: 'Group is in use' } } })
    const wrapper = mount(ProxyGroupsDialog, { props: { show: true, groups }, global })
    mounted.push(wrapper)
    await wrapper.findAll('button').find(button => button.text() === 'common.delete')!.trigger('click')
    expect(api.deleteGroup).not.toHaveBeenCalled()
    const deleteButtons = wrapper.findAll('button').filter(button => button.text() === 'common.delete')
    await deleteButtons.at(-1)!.trigger('click')
    await flushPromises()
    expect(api.deleteGroup).toHaveBeenCalledWith(7)
    expect(wrapper.get('[role="alert"]').text()).toBe('Group is in use')
    expect(wrapper.emitted('changed')).toBeUndefined()
    expect(wrapper.text()).toContain('Hong Kong')
  })
})

describe('batch proxy membership', () => {
  it.each([['7', 7], ['', null]])('assigns only selected proxies to %s', async (option, expected) => {
    api.batchSetGroup.mockResolvedValueOnce({ updated_count: 2 })
    const wrapper = mount(BatchProxyGroupDialog, { props: { show: true, groups, ids: [11, 22] }, global })
    mounted.push(wrapper)
    if (option) await wrapper.get('select').setValue(option)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.batchSetGroup).toHaveBeenCalledWith([11, 22], expected)
    expect(wrapper.emitted('changed')).toHaveLength(1)
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('keeps the dialog open and selection intact when the server rejects the group', async () => {
    api.batchSetGroup.mockRejectedValueOnce(new Error('deleted group'))
    const wrapper = mount(BatchProxyGroupDialog, { props: { show: true, groups, ids: [11] }, global })
    mounted.push(wrapper)
    await wrapper.get('select').setValue('7')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.emitted('changed')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.get('select').element.value).toBe('7')
    expect(wrapper.get('[role="alert"]').text()).toBe('deleted group')
  })
})
