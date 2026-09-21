import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import DailyCooldownSettings from '../DailyCooldownSettings.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import { normalizeDailyCooldown } from '@/utils/dailyCooldown'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, values?: { label: string }) => values ? `${values.label}: ${key}` : key }) }))
enableAutoUnmount(afterEach)

const render = (modelValue = normalizeDailyCooldown()) => mount(DailyCooldownSettings, {
  props: { modelValue }, global: { stubs: { transition: true } }
})

describe('DailyCooldownSettings', () => {
  it('starts disabled and enables the default overnight schedule', async () => {
    const wrapper = render()
    expect(wrapper.findComponent(Select).exists()).toBe(false)
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    await wrapper.get('[data-testid="daily-cooldown-enabled"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([{ enabled: true, start: '23:00', end: '08:00', timezone: 'Asia/Shanghai' }])
  })

  it('preserves arbitrary saved minutes while selecting hours and minutes independently', async () => {
    const config = { ...normalizeDailyCooldown(), enabled: true, start: '23:17', end: '08:41' }
    const wrapper = render(config)
    const controls = wrapper.findAllComponents(Select)
    expect(controls.map(control => control.props('modelValue'))).toEqual(['23', '17', '08', '41'])
    expect(controls.map(control => control.props('options').length)).toEqual([24, 60, 24, 60])
    controls[1].vm.$emit('update:modelValue', '03')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([{ ...config, start: '23:03' }])
    await wrapper.setProps({ modelValue: { ...config, start: '23:03' } })
    controls[0].vm.$emit('update:modelValue', '01')
    expect(wrapper.emitted('update:modelValue')?.[1]).toEqual([{ ...config, start: '01:03' }])
    controls[3].vm.$emit('update:modelValue', '59')
    expect(wrapper.emitted('update:modelValue')?.[2]).toEqual([{ ...config, start: '23:03', end: '08:59' }])
    expect(controls[0].get('button').attributes('aria-label')).toBe('admin.accounts.dailyCooldown.start: admin.accounts.dailyCooldown.hourLabel')
    expect(controls[3].get('button').attributes('aria-label')).toBe('admin.accounts.dailyCooldown.end: admin.accounts.dailyCooldown.minuteLabel')
  })

  it('uses the platform menu to select any minute without a native time popup', async () => {
    const wrapper = render({ ...normalizeDailyCooldown(), enabled: true })
    expect(wrapper.find('input[type="time"]').exists()).toBe(false)
    await wrapper.findAllComponents(Select)[1].get('button').trigger('click')
    const options = Array.from(document.querySelectorAll<HTMLElement>('[role="option"]'))
    expect(options).toHaveLength(60)
    options.find(option => option.textContent?.trim() === '17')!.click()
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([{ ...normalizeDailyCooldown(), enabled: true, start: '23:17' }])
  })

  it('shows overnight context, invalid bounds and a disabled fieldset for unapplied bulk edits', async () => {
    const config = { ...normalizeDailyCooldown(), enabled: true }
    const wrapper = render(config)
    expect(wrapper.text()).toContain('admin.accounts.dailyCooldown.overnightHint')
    await wrapper.setProps({ modelValue: { ...config, end: config.start } })
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.accounts.dailyCooldown.equalTimes')
    await wrapper.setProps({ disabled: true })
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
  })

  it('closes open teleported menus and prevents all updates when disabled', async () => {
    const wrapper = render({ ...normalizeDailyCooldown(), enabled: true })
    await wrapper.findAllComponents(Select)[0].get('button').trigger('click')
    expect(document.querySelector('[role="listbox"]')).not.toBeNull()
    await wrapper.setProps({ disabled: true })
    expect(document.querySelector('[role="listbox"]')).toBeNull()
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeDefined()
    for (const control of wrapper.findAllComponents(Select)) {
      expect(control.props('disabled')).toBe(true)
      expect(control.get('button').attributes('disabled')).toBeDefined()
      control.vm.$emit('update:modelValue', '12')
    }
    wrapper.getComponent(Toggle).vm.$emit('update:modelValue', false)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })
})
