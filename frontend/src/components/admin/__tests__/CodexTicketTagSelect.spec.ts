import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import CodexTicketTagSelect from '../CodexTicketTagSelect.vue'
import Select from '@/components/common/Select.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const options = [
  { value: 'team', label: 'Team / Business' },
  { value: 'pro', label: 'Pro' },
  { value: 'free', label: 'Free', disabled: true }
]
let unmount: (() => void) | undefined

function render(props: Record<string, unknown> = {}) {
  const wrapper = mount(CodexTicketTagSelect, {
    attachTo: document.body,
    props: { modelValue: [], options, label: 'Subscription tiers', placeholder: 'Select tiers', ...props }
  })
  unmount = () => wrapper.unmount()
  return wrapper
}

afterEach(() => {
  unmount?.()
  unmount = undefined
  document.body.innerHTML = ''
})

describe('CodexTicketTagSelect', () => {
  it('selects an existing option from the actual searchable dropdown and removes its tag', async () => {
    const wrapper = render()
    const trigger = wrapper.get('button.select-trigger')
    expect(wrapper.get('label').attributes('for')).toBe(trigger.attributes('id'))
    await trigger.trigger('click')
    const search = document.body.querySelector<HTMLInputElement>('.select-search-input')!
    search.value = 'business'
    search.dispatchEvent(new Event('input'))
    await nextTick()
    const matches = document.body.querySelectorAll<HTMLElement>('[role="option"]')
    expect(matches).toHaveLength(1)
    matches[0].click()
    await nextTick()
    expect(wrapper.emitted('update:modelValue')).toEqual([[['team']]])
    await wrapper.setProps({ modelValue: ['team'] })
    expect(wrapper.get('[data-testid="selected-tag"]').text()).toBe('Team / Business')
    expect(wrapper.getComponent(Select).props('options')).not.toContainEqual(options[0])
    await wrapper.get('[data-testid="tag-remove-team"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[]])
  })

  it('rejects duplicate, unknown, non-string and disabled values even when a select event is injected', () => {
    const wrapper = render({ modelValue: ['team'] })
    const select = wrapper.getComponent(Select)
    for (const value of ['team', 'typo', 'free', null, true, 2]) {
      select.vm.$emit('update:modelValue', value)
    }
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    select.vm.$emit('update:modelValue', 'pro')
    expect(wrapper.emitted('update:modelValue')).toEqual([[['team', 'pro']]])
  })

  it('cannot create a value by entering search text', async () => {
    const wrapper = render()
    await wrapper.get('button.select-trigger').trigger('click')
    const search = document.body.querySelector<HTMLInputElement>('.select-search-input')!
    search.value = 'not-a-subscription'
    search.dispatchEvent(new Event('input'))
    await nextTick()
    search.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await nextTick()
    expect(document.body.querySelectorAll('[role="option"]')).toHaveLength(0)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('supports keyboard selection, Escape and outside-click dismissal', async () => {
    const wrapper = render()
    await wrapper.get('button.select-trigger').trigger('keydown', { key: 'ArrowDown' })
    await nextTick()
    const menu = document.body.querySelector<HTMLElement>('[role="listbox"]')!
    menu.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
    menu.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await nextTick()
    expect(wrapper.emitted('update:modelValue')).toEqual([[['pro']]])
    await wrapper.get('button.select-trigger').trigger('click')
    document.body.querySelector('[role="listbox"]')!
      .dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await nextTick()
    expect(wrapper.get('button.select-trigger').attributes('aria-expanded')).toBe('false')
    await vi.waitFor(() => expect(document.body.querySelector('[role="listbox"]')).toBeNull())
    await wrapper.get('button.select-trigger').trigger('click')
    document.body.click()
    await nextTick()
    expect(wrapper.get('button.select-trigger').attributes('aria-expanded')).toBe('false')
    await vi.waitFor(() => expect(document.body.querySelector('[role="listbox"]')).toBeNull())
  })

  it('keeps unknown old values visible and removable without making them selectable', async () => {
    const wrapper = render({ modelValue: ['legacy-plan'] })
    expect(wrapper.get('[data-testid="selected-tag"]').text()).toBe('legacy-plan')
    await wrapper.get('[data-testid="tag-remove-legacy-plan"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[[]]])
    await wrapper.setProps({ modelValue: [] })
    wrapper.getComponent(Select).vm.$emit('update:modelValue', 'legacy-plan')
    expect(wrapper.emitted('update:modelValue')).toHaveLength(1)
  })

  it('closes an open portal and protects additions and removals when disabled while editing', async () => {
    const wrapper = render({ modelValue: ['team'] })
    await wrapper.get('button.select-trigger').trigger('click')
    expect(document.body.querySelector('[role="listbox"]')).not.toBeNull()
    await wrapper.setProps({ disabled: true })
    await flushPromises()
    expect(document.body.querySelector('[role="listbox"]')).toBeNull()
    expect(wrapper.get<HTMLButtonElement>('button.select-trigger').element.disabled).toBe(true)
    expect(wrapper.get<HTMLButtonElement>('[data-testid="tag-remove-team"]').element.disabled).toBe(true)
    wrapper.getComponent(Select).vm.$emit('update:modelValue', 'pro')
    await wrapper.get('[data-testid="tag-remove-team"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.setProps({ disabled: false })
    wrapper.getComponent(Select).vm.$emit('update:modelValue', 'pro')
    expect(wrapper.emitted('update:modelValue')).toEqual([[['team', 'pro']]])
  })

  it('caps selected values while allowing removal and closes an open portal when the cap is reached', async () => {
    const wrapper = render({ modelValue: [], max: 1 })
    await wrapper.get('button.select-trigger').trigger('click')
    expect(document.body.querySelector('[role="listbox"]')).not.toBeNull()
    await wrapper.setProps({ modelValue: ['team'] })
    await flushPromises()
    expect(document.body.querySelector('[role="listbox"]')).toBeNull()
    expect(wrapper.get<HTMLButtonElement>('button.select-trigger').element.disabled).toBe(true)
    wrapper.getComponent(Select).vm.$emit('update:modelValue', 'pro')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.get('[data-testid="tag-remove-team"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[[]]])
  })

  it('applies updated option restrictions to stale selection events', async () => {
    const wrapper = render()
    await wrapper.setProps({ options: options.map(option => ({ ...option, disabled: option.value === 'team' })) })
    wrapper.getComponent(Select).vm.$emit('update:modelValue', 'team')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })
})
